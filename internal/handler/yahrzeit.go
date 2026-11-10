package handler

import (
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/yahrzeitdb"
	"github.com/hebcal/hebcal-api-go/internal/reqlog"
	"github.com/hebcal/hebcal-api-go/internal/service/yahrzeit"
)

// yahrzeitDownload serves a personal Yahrzeit + Anniversary calendar as an
// iCalendar feed, or as a .csv file (yahrzeitCSV).
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
	ext := req.Ext()
	if ext != ".ics" && ext != ".csv" {
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
	if ext == ".csv" {
		s.yahrzeitCSV(w, r, cal)
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

// yahrzeitCSV serves the .csv export of a personal calendar. Like the feed it
// carries no Cache-Control and saves as an attachment only on dl=1; unlike it,
// nothing in it depends on the client or the current week, so it does not
// vary by User-Agent and a calendar with no events is a header row with an
// ETag like any other.
func (s *Server) yahrzeitCSV(w http.ResponseWriter, r *http.Request, cal *yahrzeit.Calendar) {
	h := w.Header()
	etag := httpx.MakeETag(r, cal.Variant(""))
	h.Set("ETag", etag)
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, err := cal.CSV()
	if err != nil {
		httpx.WritePlainError(w, err)
		return
	}
	if cal.Attachment() {
		h.Set("Content-Disposition", attachment(path.Base(r.URL.Path)))
	}
	h.Set("Content-Type", "text/x-csv; charset=utf-8")
	w.Write(body)
}

// yahrzeitSaved serves a saved calendar, /v3/<id>/<name>.ics or .csv: its
// parameters are read from the database by id, the URL's own query string
// overrides them, and the result is served exactly as a calendar carried in
// its URL would be.
//
// An unknown id is 404 with no caching headers. A database that is not
// configured or does not answer is 503 with Retry-After, so the proxy in front
// keeps serving its stale copy -- what a subscriber should see during an
// outage, rather than an error their client may unsubscribe over.
//
// Every request that finds its calendar records the access, 304s included:
// the first marks the calendar downloaded, which keeps it from being pruned
// as abandoned.
func (s *Server) yahrzeitSaved(w http.ResponseWriter, r *http.Request) {
	if !downloadMethodAllowed(w, r) {
		return
	}
	path, rawQuery, _ := strings.Cut(r.RequestURI, "?")
	id, ok := yahrzeit.SavedID(path)
	if !ok {
		httpx.WritePlainError(w, model.NotFound("Yahrzeit/Anniversary id not found: %s", savedIDForMessage(path)))
		return
	}
	if s.Yahrzeit == nil {
		writeYahrzeitUnavailable(w, errors.New("Yahrzeit database is not configured"))
		return
	}
	row, err := s.Yahrzeit.Get(r.Context(), id)
	if errors.Is(err, yahrzeitdb.ErrNotFound) {
		httpx.WritePlainError(w, model.NotFound("Yahrzeit/Anniversary id not found: %s", id))
		return
	}
	if err != nil {
		writeYahrzeitUnavailable(w, err)
		return
	}
	s.Yahrzeit.Touch(id, row.Downloaded)
	req, err := yahrzeit.NewSavedRequest(path, rawQuery, id, row.Contents, row.Updated)
	if err != nil {
		httpx.WritePlainError(w, model.Internal("Yahrzeit/Anniversary calendar %s is unreadable: %v", id, err))
		return
	}
	if !req.IsYahrzeit() {
		// The URL overrode v= with something that is not a personal calendar.
		httpx.WriteNotFoundText(w)
		return
	}
	s.yahrzeitDownload(w, r, req)
}

// savedIDForMessage is what a malformed /v3/ path named in place of an id,
// cut to the id's length the way the path is read.
func savedIDForMessage(path string) string {
	rest := strings.TrimPrefix(path, yahrzeit.SavedPrefix)
	if len(rest) > 26 {
		rest = rest[:26]
	}
	return rest
}

// writeYahrzeitUnavailable answers 503 for a database that is down or not
// configured. The cause goes to the access log, not to the client.
func writeYahrzeitUnavailable(w http.ResponseWriter, err error) {
	httpx.RecordError(w, err)
	h := w.Header()
	h.Del("Cache-Control")
	h.Del("ETag")
	h.Set("Retry-After", "30")
	h.Set("Content-Type", httpx.ContentTypeText)
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("Yahrzeit database is not available\n"))
}
