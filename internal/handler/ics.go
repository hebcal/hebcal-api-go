package handler

import (
	"errors"
	"net/http"
	"path"
	"strconv"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/service/ical"
)

// icsDownload serves a holiday calendar as an iCalendar feed, from any of the
// download URL shapes: /v4/, /v2/h/ and the classic /hebcal/index.cgi/ and
// /export/ query strings.
//
// Its headers are the PDF's -- a 14-day Cache-Control and a weak ETag answered
// before any events are generated, nosniff -- plus a Content-Disposition that
// saves the file unless the request is a subscription. A subscription too long
// for calendar applications is cut, which makes it depend on the week, so the
// ETag is checked a second time with the week in it. A calendar with no events
// is still a 200 holding one placeholder event, as several clients drop a
// subscription that answers with an error or an empty calendar; that response
// is cacheable for a day and has no ETag.
func (s *Server) icsDownload(w http.ResponseWriter, r *http.Request) {
	if !downloadMethodAllowed(w, r) {
		return
	}
	req, err := s.ICS.Parse(r.Context(), r.URL)
	if err != nil {
		writeDownloadError(w, err)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", httpx.CacheControl14Days)
	h.Set("X-Content-Type-Options", "nosniff")
	variant := req.Variant()
	if s.etagFresh(w, r, variant) {
		return
	}
	feed, err := s.ICS.Generate(r.Context(), req)
	if err != nil {
		// Only the 304 above and a feed are cached for two weeks.
		h.Del("ETag")
		h.Del("Cache-Control")
		writeDownloadError(w, err)
		return
	}
	if s.ICS.Limit(feed) && s.etagFresh(w, r, variant+"/"+strconv.FormatInt(s.ICS.Week(), 10)) {
		return
	}
	res, err := s.ICS.Marshal(feed, s.Zones, r.RequestURI)
	if err != nil {
		h.Del("ETag")
		httpx.WritePlainError(w, err)
		return
	}
	if res.Empty {
		h.Del("ETag")
		h.Set("Cache-Control", "max-age=86400")
	}
	if req.Attachment() {
		h.Set("Content-Disposition", attachment(path.Base(r.URL.Path)))
	}
	h.Set("Content-Type", "text/calendar; charset=utf-8")
	w.Write(res.Body)
}

// etagFresh stamps the response's ETag and reports whether the request was
// answered 304 from it.
func (s *Server) etagFresh(w http.ResponseWriter, r *http.Request, variant string) bool {
	etag := httpx.MakeETag(r, variant)
	w.Header().Set("ETag", etag)
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// writeReadingsError answers a feed whose Torah readings could not be had: 501
// when no readings-svc is configured, because retrying cannot help, and 503
// when one did not answer, so the proxy in front keeps serving what it holds.
func writeReadingsError(w http.ResponseWriter, err error) bool {
	var re *ical.ReadingsError
	if !errors.As(err, &re) {
		return false
	}
	httpx.RecordError(w, err)
	if re.Retryable {
		w.Header().Set("Retry-After", "5")
		http.Error(w, re.Error(), http.StatusServiceUnavailable)
	} else {
		http.Error(w, re.Error(), http.StatusNotImplemented)
	}
	return true
}
