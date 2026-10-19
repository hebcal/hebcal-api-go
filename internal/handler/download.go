package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
	"github.com/hebcal/hebcal-api-go/internal/service/holidaypdf"
)

// This file is the transport shared by every calendar the download service
// renders, whatever its format: the /v4/ dispatch by file extension, the
// accepted methods, the conditional request, and the status each service error
// maps to. pdf.go and csv.go hold what differs.

// byExtension sends a download request whose path ends in .csv to csvDownload
// and everything else to pdf, the PDF handler. All three download URL shapes
// -- /v4/, /v2/h/ and /hebcal/index.cgi/ -- serve both formats.
func (s *Server) byExtension(pdf http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".csv") {
			s.csvDownload(w, r)
			return
		}
		pdf(w, r)
	}
}

// downloadMethodAllowed rejects the methods a calendar has no answer for.
func downloadMethodAllowed(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
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

// writeDownloadError maps a download service error (PDF or CSV) onto its HTTP
// status.
//
// Which responses keep the Cache-Control is the interesting part: the 410 keeps
// it (that year never comes into range) and the empty-calendar 400 drops it.
// The unknown-location 404 also drops it -- a deliberate divergence from
// production, which keeps it there -- because a location missing today may be
// added later, so pinning the 404 in Varnish for two weeks is worse than a
// cache miss. The download handlers set the header only after this function has
// returned without writing, so a 410 sets its own and nothing else carries one.
func writeDownloadError(w http.ResponseWriter, err error) {
	// Record the underlying error (not the possibly-terser body some cases
	// write) for the access log's "msg" field.
	httpx.RecordError(w, err)
	var oor *download.OutOfRangeError
	var unsup *download.UnsupportedSeriesError
	switch {
	case errors.As(err, &oor):
		w.Header().Set("Cache-Control", httpx.CacheControl14Days)
		http.Error(w, oor.Error(), http.StatusGone)
	case errors.As(err, &unsup):
		// Several daily-learning series have no Go schedule, and their rows
		// come from the readings-svc sidecar rather than the calendar being
		// served without them. Either way the user never gets a calendar
		// missing rows they asked for, but the two failure modes are different
		// and the codes say so:
		//
		//   501  this build cannot render it at all, because no readings-svc is
		//        configured. Retrying will not help.
		//   503  readings-svc is configured but did not answer. Transient, and
		//        worth retrying or letting Varnish fall back.
		w.Header().Set("X-Unsupported-Series", unsup.Header())
		if unsup.Retryable {
			w.Header().Set("Retry-After", "5")
			http.Error(w, unsup.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, unsup.Error(), http.StatusNotImplemented)
	default:
		writeCalendarError(w, err)
	}
}

// writeCalendarError handles the errors every calendar route answers alike:
// an unresolvable location or URL is 404 (reserving 400 for malformed input),
// a year outside 1..32000 is 400, an error that carries its own status keeps
// it, and anything else is a malformed request.
func writeCalendarError(w http.ResponseWriter, err error) {
	var nf *download.NotFoundError
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
