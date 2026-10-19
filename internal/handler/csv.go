package handler

import (
	"mime"
	"net/http"
	"path"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	csvsvc "github.com/hebcal/hebcal-api-go/internal/service/csv"
)

// csvDownload renders one download calendar as an Outlook-compatible CSV file.
//
// Its headers are the PDF's -- a 14-day Cache-Control, CORS, nosniff and a weak
// ETag answered before the calendar is generated -- plus a Content-Disposition
// that saves the file under the name in the URL. Its errors are the PDF's too,
// except that a calendar with no events is a header row rather than a 400.
func (s *Server) csvDownload(w http.ResponseWriter, r *http.Request) {
	if !downloadMethodAllowed(w, r) {
		return
	}
	cal, err := s.CSV.Prepare(r.Context(), r.URL)
	if err != nil {
		writeDownloadError(w, err)
		return
	}

	w.Header().Set("Cache-Control", httpx.CacheControl14Days)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s.writeETag(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/x-csv; charset=utf-8")
	w.Header().Set("Content-Disposition", attachment(path.Base(r.URL.Path)))
	w.Write(csvsvc.Render(cal))
}

// attachment builds a Content-Disposition that saves the response as
// filename. The download names are ASCII slugs, which go out quoted; anything
// else falls back to the RFC 2231 encoding.
func attachment(filename string) string {
	for _, c := range filename {
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return mime.FormatMediaType("attachment", map[string]string{"filename": filename})
		}
	}
	return `attachment; filename="` + filename + `"`
}
