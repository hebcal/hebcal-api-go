package handler

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/service/location"
	"github.com/hebcal/hebcal-api-go/internal/service/shabbat"
)

// shabbat implements GET /shabbat (cfg=json only).
func (s *Server) shabbat(w http.ResponseWriter, r *http.Request) {
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

	// Scope gate: only cfg=json is implemented.
	if q.Get("cfg") != "json" {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write(jsutil.Marshal(map[string]string{
			"error": "Only cfg=json is supported by this endpoint",
		}))
		return
	}
	leyningParam := q.Get("leyning")
	leyningOff := leyningParam == "off" || leyningParam == "0"
	if s.DB == nil {
		httpx.WriteJSONError(w, dbUnavailable())
		return
	}

	loc, err := location.FromQuery(s.DB, q)
	if err != nil {
		httpx.WriteJSONError(w, err)
		return
	}
	if loc == nil {
		// hebcal-web defaults to New York when no location is given.
		loc = s.DB.LookupLegacyCity("New York")
		if loc == nil {
			loc = s.DB.LookupGeoname(5128581)
		}
		if loc == nil {
			httpx.WriteJSONError(w, model.BadRequest("Location is required"))
			return
		}
	}

	dt, isToday, err := shabbat.QueryDate(q)
	if err != nil {
		httpx.WriteJSONError(w, err)
		return
	}
	start, end, err := shabbat.WeekRange(dt, isToday, loc.TimeZoneID)
	if err != nil {
		httpx.WriteJSONError(w, err)
		return
	}

	// i=on puts a Diaspora location on the Israel schedule. The candle-lighting
	// custom still follows the location itself, as it does in hebcal-web.
	il := loc.IsIsrael() || jsutil.IsOn(q.Get("i"))
	lg := shabbat.QueryLang(q)
	// hebcal-web validates the locale here (makeHebcalOptions calls
	// Locale.useLocale, which throws for an unknown name); its other JSON
	// routes accept any lg and quietly fall back to English.
	if !model.LocaleSupported(lg) {
		httpx.WriteJSONError(w, model.BadRequest("Locale '%s' not found", lg))
		return
	}
	locale := strings.ToLower(model.AliasLocale(lg))
	candleOpts := shabbat.CandleOptions(q, loc)
	opts := shabbat.CalOptions(loc, il, start, end, q, candleOpts)
	events, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		httpx.WriteJSONError(w, model.BadRequest("%s", err.Error()))
		return
	}
	if candleOpts.AtSunset {
		shabbat.MoveCandleLightingToSunset(events, &opts)
	}
	if len(events) == 0 {
		httpx.WriteJSONError(w, model.BadRequest("Bad request: no events"))
		return
	}
	// Deliberately after the empty check: asking for Yom Tov only in a week
	// that has none is a fair question with an empty answer, so it returns
	// 200 and no items. hebcal-web filters before its own check and answers
	// 400 there.
	if jsutil.IsOn(q.Get("yto")) {
		events = shabbat.FilterYomTovOnly(events)
	}

	// Caching: an explicit date is cacheable for 7 days; the rolling "today"
	// window expires at the end of Saturday in the location's timezone.
	if !isToday {
		w.Header().Set("Cache-Control", httpx.CacheControl7Days)
	} else {
		now, expires := shabbat.ExpiresSaturdayNight(loc.TimeZoneID)
		w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
		w.Header().Set("Expires", expires.UTC().Format(http.TimeFormat))
	}
	etag := httpx.MakeETag(r, "")
	w.Header().Set("ETag", etag)
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Torah readings come from the readings-svc sidecar, fetched only once a
	// body is actually going out. They are part of that body, so a failure
	// there is a failure of this request rather than something to paper over
	// with a partial answer.
	var leyningByDate map[string][]readings.Item
	if !leyningOff {
		leyningByDate, err = s.Readings.Leyning(r.Context(), start, end, il)
		if err != nil {
			s.Logger.Warn("leyning lookup failed: " + err.Error())
			// the validators and freshness above describe the body we are
			// no longer sending
			for _, h := range []string{"Cache-Control", "Expires", "Last-Modified", "ETag"} {
				w.Header().Del(h)
			}
			httpx.WriteJSONError(w, model.Unavailable("Torah reading service is not available"))
			return
		}
	}

	hdp := q.Get("hdp") == "1"
	body := shabbat.Response(events, loc, il, locale, lg, hdp, queryHour12(q), leyningByDate)
	writeShabbatBody(w, q, body)
}

// queryHour12 reads the h12 override, or nil when the request does not ask.
// hebcal-web sets options.hour12 = !off(query.h12), so h12=0 and h12=off both
// mean 24-hour and anything else present means 12-hour.
func queryHour12(q url.Values) *bool {
	v := q.Get("h12")
	if v == "" {
		return nil
	}
	hour12 := !(v == "off" || v == "0")
	return &hour12
}

const jsonpCallbackMaxLen = 128

var jsonpCallbackRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*$`)

// writeShabbatBody writes the response, wrapping it in a JSONP callback when
// one is requested. Ported from jsonpBody() in hebcal-web src/common.js: a
// callback that is too long or not a plain dotted identifier is ignored
// rather than sanitized, so a bad one still yields ordinary JSON.
func writeShabbatBody(w http.ResponseWriter, q url.Values, body interface{}) {
	callback := q.Get("callback")
	if len(callback) == 0 || len(callback) > jsonpCallbackMaxLen || !jsonpCallbackRe.MatchString(callback) {
		w.Write(jsutil.Marshal(body))
		return
	}
	w.Header().Set("Content-Type", httpx.ContentTypeJSONP)
	w.Write([]byte(callback + "("))
	w.Write(jsutil.Marshal(body))
	w.Write([]byte(")\n"))
}
