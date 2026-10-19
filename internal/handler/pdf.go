package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
	"github.com/hebcal/hebcal-api-go/internal/service/holidaypdf"
	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
)

// This file is the transport half of the two PDF calendar families Varnish
// routes here: download.hebcal.com's /v4/<data>/<name>.pdf downloads (and
// their legacy /v2/h/<data>/<name>.pdf spelling) and www.hebcal.com's
// /holidays/hebcal-<year>.pdf holiday calendars. They share a generator and a
// renderer (internal/service/pdf) and differ in their headers, which is most
// of what is below.

// pdfDownload renders one /v4/ calendar, or one of its two legacy spellings of
// the same request: /v2/h/ (see internal/service/download/v2.go) and the classic
// /hebcal/index.cgi/<name>.pdf?<query> (see internal/service/download/cgi.go).
//
// The response carries a 14-day Cache-Control (dropped only on the empty-events
// 400), CORS and nosniff. Without these Varnish and browsers do not cache the
// PDF.
func (s *Server) pdfDownload(w http.ResponseWriter, r *http.Request) {
	if !downloadMethodAllowed(w, r) || !s.pdfAvailable(w) {
		return
	}
	cal, err := s.PDF.Prepare(r.Context(), r.URL)
	if err != nil {
		writeDownloadError(w, err)
		return
	}

	w.Header().Set("Cache-Control", httpx.CacheControl14Days)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// The ETag is a pure function of the request and the library versions, so a
	// conditional request can be answered 304 before the (expensive) render. A
	// client only holds this ETag if it received a 200 for the same URL, and the
	// calendar is a deterministic function of that URL, so its cached copy is
	// still valid.
	if s.writeETag(w, r) {
		return
	}
	s.writePDF(w, cal)
}

// pdfHoliday renders /holidays/hebcal-<year>.pdf.
//
// The response headers differ from the /v4/ handler: a 60-day Cache-Control, no
// CORS header (www.hebcal.com sets that only for the cfg= API responses), and
// nosniff, which www sets on every response. The Cache-Control goes on after
// the URL has been accepted, so the 404, 400 and 410 are not cached.
func (s *Server) pdfHoliday(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !downloadMethodAllowed(w, r) {
		return
	}
	// Only the PDFs are routed here; the HTML pages under /holidays/ are served
	// elsewhere, so anything else is a 404 rather than a half-rendered
	// calendar.
	if !strings.HasSuffix(r.URL.Path, ".pdf") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !s.pdfAvailable(w) {
		return
	}
	params, err := holidaypdf.Parse(r.URL.Path, r.URL.Query())
	if err != nil {
		writeHolidayError(w, err)
		return
	}

	w.Header().Set("Cache-Control", httpx.CacheControl60Days)

	// As on the /v4/ path, the ETag depends only on the request and the build,
	// so a conditional request is answered before the calendar is generated.
	if s.writeETag(w, r) {
		return
	}

	events, err := pdf.Generate(params)
	if err != nil {
		httpx.WritePlainError(w, model.Internal("render: %s", err.Error()))
		return
	}
	s.writePDF(w, &pdf.Calendar{
		Params: params,
		Events: events,
		Title:  pdf.CalendarTitle(params, events),
	})
}

// pdfAvailable reports the PDF routes as unavailable when the fonts could not
// be loaded at startup. The rest of the API keeps working in that case, which
// is why a missing font directory is not fatal at startup.
func (s *Server) pdfAvailable(w http.ResponseWriter) bool {
	if !s.PDF.Available() {
		httpx.WritePlainError(w, model.Unavailable("PDF rendering is not available"))
		return false
	}
	return true
}

// writePDF renders the calendar and writes it. The middleware sets
// Content-Length and drops the body on a HEAD request.
func (s *Server) writePDF(w http.ResponseWriter, cal *pdf.Calendar) {
	body, err := s.PDF.Render(cal)
	if err != nil {
		httpx.WritePlainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Write(body)
}

// writeHolidayError maps a /holidays/ parse error onto its HTTP status. The
// parse errors all happen before Cache-Control is set, so none of these
// responses is cacheable -- and the 410 says only "Gone", where the download
// path names the year.
func writeHolidayError(w http.ResponseWriter, err error) {
	// The holiday 410 body is only "Gone"; log the richer error naming the year.
	httpx.RecordError(w, err)
	var oor *download.OutOfRangeError
	if errors.As(err, &oor) {
		http.Error(w, "Gone", http.StatusGone)
		return
	}
	writeCalendarError(w, err)
}
