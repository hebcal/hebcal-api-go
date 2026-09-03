package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/model"
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
// the same request: /v2/h/ (see internal/service/pdf/v2.go) and the classic
// /hebcal/index.cgi/<name>.pdf?<query> (see internal/service/pdf/cgi.go).
//
// The response headers follow hebcal-web: its download dispatcher
// (src/app-download.js) sets a 14-day Cache-Control before rendering and the
// .pdf branch of src/hebcal-download.js leaves it in place -- removing it only
// on the empty-events 400 -- then adds CORS and nosniff. Without these Varnish
// and browsers do not cache the PDF.
func (s *Server) pdfDownload(w http.ResponseWriter, r *http.Request) {
	if !pdfMethodAllowed(w, r) || !s.pdfAvailable(w) {
		return
	}
	cal, err := s.PDF.Prepare(r.Context(), r.URL)
	if err != nil {
		writeDownloadError(w, err, r)
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
// The response headers follow holidayPdf.js rather than the /v4/ handler: a
// 60-day Cache-Control, no CORS header (www.hebcal.com sets that only for the
// cfg= API responses), and nosniff, which www sets on every response. As in
// holidayPdf.js the Cache-Control goes on after the URL has been accepted, so
// the 404, 400 and 410 are not cached.
func (s *Server) pdfHoliday(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !pdfMethodAllowed(w, r) {
		return
	}
	// Varnish routes only the PDFs here; the HTML pages under /holidays/ are
	// still served by hebcal-web, so anything else is a 404 rather than a
	// half-rendered calendar.
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

// bingUA is the substring app-download.js's fixup2 checks the User-Agent
// header for (`compatible; bingbot/2.`), ported verbatim rather than matching
// the whole "bingbot" token so it can't also catch some future unrelated
// crawler that merely mentions bing.
const bingUA = "compatible; bingbot/2."

// isBingBot reports whether the request's User-Agent identifies bingbot,
// mirroring hebcal-web's isBingBot.
func isBingBot(r *http.Request) bool {
	return strings.Contains(r.Header.Get("User-Agent"), bingUA)
}

// pdfMethodAllowed rejects the methods a calendar has no answer for.
func pdfMethodAllowed(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
		return false
	}
	return true
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

// writeETag stamps the response's ETag and reports whether the request was
// answered 304 from it.
func (s *Server) writeETag(w http.ResponseWriter, r *http.Request) bool {
	etag := httpx.MakeETag(r, "")
	w.Header().Set("ETag", etag)
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
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

// writeDownloadError maps a /v4/ service error onto the status hebcal-web
// answers with.
//
// Which responses keep the Cache-Control is the interesting part. The
// dispatcher in src/app-download.js sets it before the handler runs, so it
// survives onto everything the handler does not remove: the 410 keeps it (that
// year never comes into range) and the empty-calendar 400 drops it. The
// unknown-location 404 is a deliberate divergence -- hebcal-web keeps it there
// too, but a location missing today may be added later, so pinning the 404 in
// Varnish for two weeks is worse than a cache miss. pdfDownload sets the header
// only after this function has returned without writing, so a 410 sets its own
// and nothing else carries one.
func writeDownloadError(w http.ResponseWriter, err error, r *http.Request) {
	// Record the underlying error (not the possibly-terser body some cases
	// write) for the access log's "msg" field.
	httpx.RecordError(w, err)
	var oor *pdf.OutOfRangeError
	var unsup *pdf.UnsupportedSeriesError
	var decErr *pdf.DecodeError
	switch {
	case errors.As(err, &oor):
		w.Header().Set("Cache-Control", httpx.CacheControl14Days)
		http.Error(w, oor.Error(), http.StatusGone)
	case errors.As(err, &decErr) && isBingBot(r):
		// bingbot fetches /v4/ URLs with the path lowercased, which fails
		// base64-decode or protobuf-unmarshal every time; app-download.js's
		// fixup2 answered exactly this case 404 rather than 400 (see
		// pdf.DecodeError). Every other malformed /v4/ request -- from any
		// other user agent -- keeps falling through to writeCommonPDFError's
		// 400 below.
		http.Error(w, "Not Found", http.StatusNotFound)
	case errors.As(err, &unsup):
		// Six daily-learning series have no Go schedule, and their rows come
		// from hebcal-web rather than the calendar being served without them.
		// Either way the user never gets a calendar missing rows they asked
		// for, but the two failure modes are different and the codes say so:
		//
		//   501  this build cannot render it at all, because no hebcal-web URL
		//        is configured. Retrying will not help.
		//   503  hebcal-web is configured but did not answer. Transient, and
		//        worth retrying or falling back to the Node service.
		w.Header().Set("X-Unsupported-Series", unsup.Header())
		if unsup.Retryable {
			w.Header().Set("Retry-After", "5")
			http.Error(w, unsup.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, unsup.Error(), http.StatusNotImplemented)
	default:
		writeCommonPDFError(w, err)
	}
}

// writeHolidayError maps a /holidays/ parse error onto the status
// holidayPdf.js answers with. Its three throws all happen before it sets
// Cache-Control, so none of these responses is cacheable -- and its 410 says
// only "Gone", where the download path names the year.
func writeHolidayError(w http.ResponseWriter, err error) {
	// The holiday 410 body is only "Gone"; log the richer error naming the year.
	httpx.RecordError(w, err)
	var oor *pdf.OutOfRangeError
	if errors.As(err, &oor) {
		http.Error(w, "Gone", http.StatusGone)
		return
	}
	writeCommonPDFError(w, err)
}

// writeCommonPDFError handles the errors both PDF routes answer identically:
// an unresolvable location or URL is 404 (getLocationFromQuery reports these
// with 404, reserving 400 for malformed input), a year outside 1..32000 is 400,
// an error that carries its own status keeps it, and anything else is a
// malformed request.
func writeCommonPDFError(w http.ResponseWriter, err error) {
	var nf *pdf.NotFoundError
	var bad *holidaypdf.BadRequestError
	var herr *model.HTTPError
	switch {
	case errors.As(err, &nf):
		http.Error(w, nf.Error(), http.StatusNotFound)
	case errors.As(err, &bad):
		http.Error(w, bad.Error(), http.StatusBadRequest)
	case errors.As(err, &herr):
		http.Error(w, herr.Message, herr.Status)
	default:
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
	}
}
