package handler

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	hebzmanim "github.com/hebcal/hebcal-go/zmanim"

	"github.com/hebcal/hebcal-api-go/internal/config"
	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/service/location"
	"github.com/hebcal/hebcal-api-go/internal/service/zmanim"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// zmanim implements GET /zmanim (cfg=json only).
func (s *Server) zmanim(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	httpx.SetCORS(w)
	switch r.Method {
	case http.MethodOptions:
		httpx.CORSPreflight(w, "GET")
		return
	case http.MethodGet, http.MethodHead:
		// handled below
	default:
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD, OPTIONS")
		return
	}
	w.Header().Set("Content-Type", httpx.ContentTypeJSON)
	if s.DB == nil {
		httpx.WriteJSONError(w, dbUnavailable())
		return
	}
	if q.Get("cfg") != "json" {
		httpx.WriteJSONError(w, model.BadRequest("Parameter cfg=json is required"))
		return
	}
	loc, err := location.FromQuery(s.DB, q)
	if err != nil {
		httpx.WriteJSONError(w, err)
		return
	}
	if loc == nil {
		httpx.WriteJSONError(w, model.BadRequest("Location is required"))
		return
	}
	useElevation := jsutil.IsOn(q.Get("ue"))
	locObj := location.ToPlainObj(loc, useElevation)

	if jsutil.IsOn(q.Get("im")) {
		s.checkMelacha(w, q, loc, locObj, useElevation)
		return
	}

	isRange, startD, endD, err := zmanim.StartAndEnd(q, loc.TimeZoneID)
	if err != nil {
		httpx.WriteJSONError(w, err)
		return
	}
	if isRange || !jsutil.QueryEmpty(q, "date") {
		w.Header().Set("Cache-Control", httpx.CacheControl30Days)
		etag := httpx.MakeETag(r, "")
		w.Header().Set("ETag", etag)
		if httpx.CheckFresh(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	} else {
		// A request without an explicit date describes today in the location's
		// own timezone, so it stops being true when the day rolls over there.
		now, expires := zmanim.ExpiresTomorrow(loc.TimeZoneID)
		w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
		w.Header().Set("Expires", expires.UTC().Format(http.TimeFormat))
	}
	roundMinute := q.Get("sec") != "1"

	var body jsutil.OrderedObj
	if isRange {
		times := zmanim.TimesForRange(startD, endD, loc, roundMinute, useElevation)
		body = jsutil.OrderedObj{
			{Key: "date", Val: jsutil.OrderedObj{
				{Key: "start", Val: startD.String()},
				{Key: "end", Val: endD.String()},
			}},
			{Key: "version", Val: config.APIVersion},
			{Key: "location", Val: locObj},
			{Key: "times", Val: times},
		}
	} else {
		times := zmanim.Times(startD, loc, roundMinute, useElevation)
		body = jsutil.OrderedObj{
			{Key: "date", Val: startD.String()},
			{Key: "version", Val: config.APIVersion},
			{Key: "location", Val: locObj},
			{Key: "times", Val: times},
		}
	}
	w.Write(jsutil.Marshal(body))
}

// checkMelacha implements the im=1 branch: reports whether melacha (work) is
// prohibited at a given moment. Ported from checkMelacha() in zmanim.js.
func (s *Server) checkMelacha(w http.ResponseWriter, q url.Values,
	loc *geodb.Location, locObj jsutil.OrderedObj, useElevation bool) {
	now := time.Now()
	w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
	tz, err := hebzmanim.LoadLocation(loc.TimeZoneID)
	if err != nil {
		httpx.WriteJSONError(w, model.BadRequest("Invalid time zone specified: %s", loc.TimeZoneID))
		return
	}
	var dt time.Time
	dateStr := strings.TrimSpace(q.Get("dt"))
	if dateStr != "" && model.ReIsoDate.MatchString(dateStr) {
		parsed, ok := zmanim.ParseMelachaDate(dateStr, tz)
		if !ok {
			httpx.WriteJSONError(w, model.BadRequest("Invalid Date: %s", dateStr))
			return
		}
		dt = parsed
	} else {
		dt = now
		w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=60")
	}
	dt = dt.In(tz)
	assur, err := zmanim.IsAssurBemlacha(dt, loc, useElevation)
	if err != nil {
		httpx.WriteJSONError(w, model.BadRequest("%s", err.Error()))
		return
	}
	status := jsutil.OrderedObj{
		{Key: "localTime", Val: dt.Format("2006-01-02T15:04:05-07:00")},
		{Key: "isAssurBemlacha", Val: assur},
	}
	body := jsutil.OrderedObj{
		{Key: "date", Val: now.UTC().Format("2006-01-02T15:04:05.000Z")},
		{Key: "version", Val: config.APIVersion},
		{Key: "location", Val: locObj},
		{Key: "status", Val: status},
	}
	w.Write(jsutil.Marshal(body))
}
