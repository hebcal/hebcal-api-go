package handler

import (
	"net/http"
	"path"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/reqlog"
	"github.com/hebcal/hebcal-api-go/internal/service/yahrzeit"
)

// yahrzeitDownload serves a personal Yahrzeit + Anniversary calendar as an
// iCalendar feed.
//
// The response carries no Cache-Control: these are personal calendars, and
// the proxy in front marks them private. It does vary by User-Agent, because
// Google Calendar's importer is sent a different rendering of the reminders
// (see yahrzeit.GoogleImporter), and its weak ETag covers everything a
// rendering depends on -- the URL, the years it spans, the current week and
// that client distinction -- so a refresh that would produce the same events
// is answered 304 before any are generated.
//
// A calendar with no events is still a 200, holding one placeholder event,
// since several clients drop a subscription that answers with an error or an
// empty calendar. That response is cacheable for a day and has no ETag.
func (s *Server) yahrzeitDownload(w http.ResponseWriter, r *http.Request, req *yahrzeit.Request) {
	if !downloadMethodAllowed(w, r) {
		return
	}
	if req.Opaque {
		reqlog.FromContext(r.Context()).SetQuery(req.RawQuery)
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	if ext := req.Ext(); ext != ".ics" {
		// The .csv export of these calendars is not served here.
		h.Set("Content-Type", httpx.ContentTypeText)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Invalid download format: " + ext + "\n"))
		return
	}
	cal, err := yahrzeit.New(req, s.Now())
	if err != nil {
		httpx.WritePlainError(w, err)
		return
	}
	userAgent := r.UserAgent()
	etag := httpx.MakeETag(r, cal.Variant(userAgent))
	h.Set("ETag", etag)
	h.Add("Vary", "User-Agent")
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	res, err := cal.ICS(userAgent, s.Zones, time.Now())
	if err != nil {
		h.Del("Vary")
		httpx.WritePlainError(w, err)
		return
	}
	if res.Empty {
		h.Del("ETag")
		h.Set("Cache-Control", "max-age=86400")
	}
	if cal.Attachment() {
		h.Set("Content-Disposition", attachment(path.Base(r.URL.Path)))
	}
	h.Set("Content-Type", "text/calendar; charset=utf-8")
	w.Write(res.Body)
}
