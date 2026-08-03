package main

// shabbatHandler implements the /shabbat JSON API, a Go port of shabbatApp in
// hebcal-web src/shabbat.js. It returns this week's (or a given week's)
// candle-lighting, Torah portion, havdalah, and related events for a location.
//
// Scope: only cfg=json is supported; any other cfg returns 501 Not
// Implemented. Torah readings (the default, suppressed with leyning={off,0})
// come from the hebcal-web /leyning endpoint; see leyning.go.

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/molad"
	"github.com/hebcal/hebcal-go/sedra"
	"github.com/hebcal/hebcal-go/zmanim"
	"github.com/hebcal/locales"
)

// parseFloat parses a query float, returning an error for empty/invalid input.
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// shabbatQueryDate resolves the requested date and reports whether it came
// from the query (isToday=false) or defaults to "now" in the location
// timezone. Callers in the wild pin the week four different ways, so all are
// accepted, in this order of precedence:
//
//	dt=YYYY-MM-DD
//	date=YYYY-MM-DD
//	start=YYYY-MM-DD (end is accepted but ignored: /shabbat always renders
//	                  the one Shabbat week containing the start date)
//	gy=YYYY&gm=MM&gd=DD
//
// hebcal-web's getTodayDate() reads only dt and gy/gm/gd; date and start
// silently fall back to today there. This endpoint honors them instead.
func shabbatQueryDate(q url.Values) (gregDate, bool, error) {
	for _, param := range []string{"dt", "date", "start"} {
		if s := strings.TrimSpace(q.Get(param)); s != "" {
			d, err := isoDateStringToDate(s)
			return d, false, err
		}
	}
	if q.Get("gy") != "" || q.Get("gm") != "" || q.Get("gd") != "" {
		d, err := makeGregDate(q.Get("gy"), q.Get("gm"), q.Get("gd"))
		return d, false, err
	}
	return gregDate{}, true, nil
}

// shabbatQueryLang returns the requested `lg`, falling back to the much older
// a=on spelling of Ashkenazi transliteration. Ported from makeHebcalOptions()
// in hebcal-web src/calendar.js, which rewrites a=on to lg=a whenever lg is
// absent; the /converter route has no such fallback in hebcal-web, so this
// stays local to /shabbat. (Resolving the short codes themselves is
// aliasLocale's job, and both routes share it.)
func shabbatQueryLang(q url.Values) string {
	if lg := q.Get("lg"); lg != "" {
		return lg
	}
	if q.Get("a") == "on" {
		return "a"
	}
	return ""
}

// shabbatWeekRange returns the [start, endOfWeek] Gregorian window for the
// Shabbat listing, ported from shabbatWeekRange + getStartAndEnd in
// hebcal-web src/dateUtil.js. If isToday, "now" in the location tz is used.
func shabbatWeekRange(dt gregDate, isToday bool, tzid string) (gregDate, gregDate, error) {
	loc, err := zmanim.LoadLocation(tzid)
	if err != nil {
		return gregDate{}, gregDate{}, badRequest("Invalid time zone specified: %s", tzid)
	}
	var day time.Time
	if isToday {
		day = time.Now().In(loc)
	} else {
		day = time.Date(dt.Year, dt.Month, dt.Day, 12, 0, 0, 0, loc)
	}
	y, m, d := day.Date()
	base := time.Date(y, m, d, 0, 0, 0, 0, loc)
	// if the day is Saturday, back up to Friday so last night's candles show
	if base.Weekday() == time.Saturday {
		base = base.AddDate(0, 0, -1)
	}
	saturday := base.AddDate(0, 0, (6-int(base.Weekday())+7)%7)
	fiveDaysAhead := base.AddDate(0, 0, 5)
	end := saturday
	if fiveDaysAhead.After(saturday) {
		end = fiveDaysAhead
	}
	start := gregDate{Year: base.Year(), Month: base.Month(), Day: base.Day()}
	endD := gregDate{Year: end.Year(), Month: end.Month(), Day: end.Day()}
	return start, endD, nil
}

// setExpiresSaturdayNight sets Expires to the next Sunday 00:00 in the
// location's timezone, matching expiresSaturdayNight in hebcal-web.
func setExpiresSaturdayNight(w http.ResponseWriter, tzid string) {
	loc, err := zmanim.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	offset := 7 - int(now.Weekday()) // Sunday -> +7 (next week), matches dayjs day(7)
	sun := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, offset)
	w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
	w.Header().Set("Expires", sun.UTC().Format(http.TimeFormat))
}

// shabbatHandler implements GET /shabbat (cfg=json only).
func (app *appServer) shabbatHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	setCORS(w)
	switch r.Method {
	case http.MethodOptions:
		corsPreflight(w, "GET")
		return
	case http.MethodGet, http.MethodHead:
		// handled below
	default:
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		http.Error(w, fmt.Sprintf("Method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)

	// Scope gate: only cfg=json is implemented.
	if q.Get("cfg") != "json" {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write(jsonMarshal(map[string]string{
			"error": "Only cfg=json is supported by this endpoint",
		}))
		return
	}
	leyning := q.Get("leyning")
	leyningOff := leyning == "off" || leyning == "0"
	if app.db == nil {
		app.writeZmanimError(w, &httpError{status: http.StatusServiceUnavailable,
			message: "Location database is not available"})
		return
	}

	loc, err := getLocationFromQuery(app.db, q)
	if err != nil {
		app.writeZmanimError(w, err)
		return
	}
	if loc == nil {
		// hebcal-web defaults to New York when no location is given.
		loc = app.db.lookupLegacyCity("New York")
		if loc == nil {
			loc = app.db.lookupGeoname(5128581)
		}
		if loc == nil {
			app.writeZmanimError(w, badRequest("Location is required"))
			return
		}
	}

	dt, isToday, err := shabbatQueryDate(q)
	if err != nil {
		app.writeZmanimError(w, err)
		return
	}
	start, end, err := shabbatWeekRange(dt, isToday, loc.TimeZoneID)
	if err != nil {
		app.writeZmanimError(w, err)
		return
	}

	// i=on puts a Diaspora location on the Israel schedule. The candle-lighting
	// custom still follows the location itself, as it does in hebcal-web.
	il := loc.isIsrael() || isOn(q.Get("i"))
	lg := shabbatQueryLang(q)
	// hebcal-web validates the locale here (makeHebcalOptions calls
	// Locale.useLocale, which throws for an unknown name); its other JSON
	// routes accept any lg and quietly fall back to English.
	if !localeSupported(lg) {
		app.writeZmanimError(w, badRequest("Locale '%s' not found", lg))
		return
	}
	locale := strings.ToLower(aliasLocale(lg))
	candleOpts := shabbatCandleOptions(q, loc)
	opts := shabbatCalOptions(loc, il, start, end, q, candleOpts)
	events, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		app.writeZmanimError(w, badRequest("%s", err.Error()))
		return
	}
	if candleOpts.noHavdalah {
		events = filterOutHavdalah(events)
	}
	if candleOpts.atSunset {
		moveCandleLightingToSunset(events, &opts)
	}
	if len(events) == 0 {
		app.writeZmanimError(w, badRequest("Bad request: no events"))
		return
	}
	// Deliberately after the empty check: asking for Yom Tov only in a week
	// that has none is a fair question with an empty answer, so it returns
	// 200 and no items. hebcal-web filters before its own check and answers
	// 400 there.
	if isOn(q.Get("yto")) {
		events = filterYomTovOnly(events)
	}

	// Caching: an explicit date is cacheable for 7 days; the rolling "today"
	// window expires at the end of Saturday in the location's timezone.
	if !isToday {
		w.Header().Set("Cache-Control", cacheControl7Days)
	} else {
		setExpiresSaturdayNight(w, loc.TimeZoneID)
	}
	etag := makeETag(r, "")
	w.Header().Set("ETag", etag)
	if checkFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Torah readings come from the hebcal-web /leyning service, fetched only
	// once a body is actually going out. They are part of that body, so a
	// failure there is a failure of this request rather than something to
	// paper over with a partial answer.
	var readings map[string][]*leyningReading
	if !leyningOff {
		readings, err = app.leyning.readings(r.Context(), start, end, il)
		if err != nil {
			app.logger.Warn("leyning lookup failed: " + err.Error())
			// the validators and freshness above describe the body we are
			// no longer sending
			for _, h := range []string{"Cache-Control", "Expires", "Last-Modified", "ETag"} {
				w.Header().Del(h)
			}
			app.writeZmanimError(w, &httpError{status: http.StatusServiceUnavailable,
				message: "Torah reading service is not available"})
			return
		}
	}

	hdp := q.Get("hdp") == "1"
	body := shabbatResponse(events, loc, il, locale, lg, hdp, queryHour12(q), readings)
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

// writeShabbatBody writes the response, wrapping it in a JSONP callback when
// one is requested. Ported from jsonpBody() in hebcal-web src/common.js: a
// callback that is too long or not a plain dotted identifier is ignored
// rather than sanitized, so a bad one still yields ordinary JSON.
func writeShabbatBody(w http.ResponseWriter, q url.Values, body interface{}) {
	callback := q.Get("callback")
	if len(callback) == 0 || len(callback) > jsonpCallbackMaxLen || !jsonpCallbackRe.MatchString(callback) {
		w.Write(jsonMarshal(body))
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Write([]byte(callback + "("))
	w.Write(jsonMarshal(body))
	w.Write([]byte(")\n"))
}

const jsonpCallbackMaxLen = 128

var jsonpCallbackRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*$`)

// shabbatCalOptions builds the hebcal.CalOptions for the Shabbat week.
func shabbatCalOptions(loc *geoLocation, il bool, start, end gregDate, q url.Values,
	c candleOptions) hebcal.CalOptions {
	zloc := loc.zmanimLocation()
	opts := hebcal.CalOptions{
		Location:         &zloc,
		IL:               il,
		CandleLighting:   true,
		Sedrot:           true,
		ShabbatMevarchim: true,
		UseElevation:     isOn(q.Get("ue")),
		Molad:            isOn(q.Get("molad")),
		Start:            hdate.FromProlepticGregorian(start.Year, start.Month, start.Day),
		End:              hdate.FromProlepticGregorian(end.Year, end.Month, end.Day),
	}
	opts.CandleLightingMins = c.candleMins
	opts.HavdalahMins = c.havdalahMins
	opts.HavdalahDeg = c.havdalahDeg
	return opts
}

// candleOptions holds the resolved candle-lighting and havdalah settings.
// havdalahMins and havdalahDeg are mutually exclusive; both zero means no
// havdalah at all.
type candleOptions struct {
	candleMins   int
	havdalahMins int
	havdalahDeg  float64
	// noHavdalah marks m=0, which asks for no havdalah at all.
	noHavdalah bool
	// atSunset marks b=0, which asks for candle-lighting exactly at sunset.
	// hebcal-go cannot express it (CheckCandleOptions rewrites a zero
	// CandleLightingMins to the 18/20-minute default), so the caller fixes
	// the times up afterwards.
	atSunset bool
}

// shabbatCandleOptions resolves b, m, M and td into candle-lighting and
// havdalah settings, porting the precedence rules in makeHebcalOptions()
// (hebcal-web src/calendar.js) together with the default in shabbatApp().
func shabbatCandleOptions(q url.Values, loc *geoLocation) candleOptions {
	var c candleOptions
	mStr, tdStr := q.Get("m"), q.Get("td")
	mIsOn := mStr == "on" // the lowercase spelling of M=on
	if mIsOn {
		mStr = ""
	}
	// shabbatApp() defaults to tzeit when the request names no preference
	havdalahTzeit := mIsOn || isOn(q.Get("M")) ||
		(q.Get("M") == "" && mStr == "" && tdStr == "")
	// with both degrees and fixed minutes, M disambiguates
	if tdStr != "" && mStr != "" {
		if havdalahTzeit || q.Get("M") == "" {
			mStr = ""
		} else {
			tdStr = ""
		}
	}
	// degrees override M=on (legacy 8.5) and m=<minutes>; a zero or
	// unparsable td is ignored
	if tdStr != "" {
		if deg, err := parseFloat(tdStr); err == nil && deg != 0 {
			c.havdalahDeg = deg
			havdalahTzeit = false
			mStr = ""
		}
	}
	if havdalahTzeit {
		c.havdalahDeg = 8.5 // 3 small stars
		mStr = ""
	}
	if mStr != "" {
		if m, ok := parseInt(mStr); ok {
			if m == 0 {
				// @hebcal/core drops the havdalah outright at zero minutes
				c.noHavdalah = true
			} else {
				c.havdalahMins = m
			}
		}
	}
	if c.havdalahMins == 0 && c.havdalahDeg == 0 {
		// nothing survived, so @hebcal/core falls back on Zmanim.tzeit(),
		// whose own default is 8.5 degrees
		c.havdalahDeg = 8.5
	}

	// candle-lighting minutes before sunset. In Israel an absent b -- or the
	// b=18 that the web form submits by default -- yields the local custom.
	c.candleMins = locationDefaultCandleMins(loc)
	if b, ok := parseInt(q.Get("b")); ok {
		if !(loc.isIsrael() && b == DefaultCandleMins && c.candleMins != DefaultCandleMins) {
			c.candleMins = b
		}
	}
	if c.candleMins == 0 {
		c.atSunset = true
		c.candleMins = DefaultCandleMins // placeholder; times are fixed up later
	}
	return c
}

// DefaultCandleMins is the customary number of minutes before sunset outside
// Israel, and the value hebcal.com's form submits when the reader has
// expressed no preference.
const DefaultCandleMins = 18

// locationDefaultCandleMins ports locationDefaultCandleMins() in hebcal-web
// src/urlArgs.js. hebcal-go applies the same custom, but keys the Israeli
// cities by name, and this service passes the full "Jerusalem, Israel" form
// as the location name, so those lookups would miss.
func locationDefaultCandleMins(loc *geoLocation) int {
	if !loc.isIsrael() {
		return DefaultCandleMins
	}
	switch loc.GeonameID {
	case 281184: // Jerusalem
		return 40
	case 294801, 293067: // Haifa, Zikhron Ya'akov
		return 30
	}
	return 20
}

// isOn reports whether a boolean query parameter is set, matching the
// booleanOpts loop in hebcal-web src/calendar.js.
func isOn(v string) bool {
	return v == "on" || v == "1"
}

// filterOutHavdalah drops the havdalah times, for m=0. It matches on the
// event description rather than a flag: an ordinary Saturday-night havdalah
// carries LIGHT_CANDLES_TZEIS, and only the one ending a Yom Tov carries
// YOM_TOV_ENDS.
func filterOutHavdalah(events []event.CalEvent) []event.CalEvent {
	out := events[:0]
	for _, ev := range events {
		if timed, ok := ev.(hebcal.TimedEvent); ok && timed.Desc == "Havdalah" {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// filterYomTovOnly keeps only the Yom Tov days, for yto=on. Ported from
// makeHebrewCalendar() in hebcal-web src/calendar.js, which applies the
// filter after the calendar is built.
func filterYomTovOnly(events []event.CalEvent) []event.CalEvent {
	out := events[:0]
	for _, ev := range events {
		flags := ev.GetFlags()
		cat, _ := categoriesOf(ev, descOf(ev), flags)
		if cat == "holiday" && flags&event.CHAG != 0 {
			out = append(out, ev)
		}
	}
	return out
}

// moveCandleLightingToSunset re-times candle-lighting to sunset itself, for
// b=0. @hebcal/core lights at sunsetOffset(0) in that case, but hebcal-go
// only reaches for a zero offset on the havdalah side and rewrites a zero
// CandleLightingMins to the default before the calendar is built, so the
// times have to be recomputed here. Drop this once hebcal-go can express it.
func moveCandleLightingToSunset(events []event.CalEvent, opts *hebcal.CalOptions) {
	for i, ev := range events {
		timed, ok := ev.(hebcal.TimedEvent)
		if !ok || timed.Desc != "Candle lighting" {
			continue
		}
		gy, gm, gd := timed.Date.Greg()
		z := zmanim.New(opts.Location, time.Date(gy, gm, gd, 12, 0, 0, 0, time.UTC))
		z.UseElevation = opts.UseElevation
		sunset := z.SunsetOffset(0, true)
		if sunset.IsZero() {
			continue
		}
		timed.EventTime = sunset
		events[i] = timed
	}
}

// shabbatResponse builds the ordered top-level JSON object. readings is nil
// when leyning is suppressed.
func shabbatResponse(events []event.CalEvent, loc *geoLocation, il bool, locale, lg string, hdp bool,
	hour12 *bool, readings map[string][]*leyningReading) orderedObj {
	body := orderedObj{
		{"title", shabbatTitle(events, loc)},
		{"date", time.Now().UTC().Format("2006-01-02T15:04:05.000Z")},
		{"version", apiVersion},
		{"location", locationToPlainObj(loc, true)}, // /shabbat always includes elevation
	}
	if len(events) > 0 {
		first := events[0].GetDate()
		last := events[len(events)-1].GetDate()
		body = append(body, jsonKV{"range", orderedObj{
			{"start", isoGreg(first)},
			{"end", isoGreg(last)},
		}})
	}
	items := make([]interface{}, 0, len(events))
	for _, ev := range events {
		items = append(items, shabbatItem(ev, loc, il, locale, lg, hdp, hour12, readings))
	}
	body = append(body, jsonKV{"items", items})
	return body
}

// shabbatTitle ports getCalendarTitle for this endpoint: "Hebcal <city> <Month
// Year>" (or year range when the events span multiple years).
func shabbatTitle(events []event.CalEvent, loc *geoLocation) string {
	title := "Hebcal " + loc.shortName()
	if len(events) == 0 {
		return title
	}
	sy, sm, sd := events[0].GetDate().Greg()
	ey, _, _ := events[len(events)-1].GetDate().Greg()
	_ = sd
	if sy != ey {
		return fmt.Sprintf("%s %d-%d", title, sy, ey)
	}
	return fmt.Sprintf("%s %s %d", title, sm.String(), sy)
}

// isoGreg formats a Hebrew date's Gregorian date as YYYY-MM-DD.
func isoGreg(hd hdate.HDate) string {
	y, m, d := hd.Greg()
	return isoDateString(y, m, d)
}

// shabbatItem serializes one event to the classic-API item object. Ordered to
// match @hebcal/rest-api eventToClassicApiObject.
func shabbatItem(ev event.CalEvent, loc *geoLocation, il bool, locale, lg string, hdp bool,
	hour12 *bool, readings map[string][]*leyningReading) orderedObj {
	flags := ev.GetFlags()
	hd := ev.GetDate()
	desc := descOf(ev)
	cat, subcat := categoriesOf(ev, desc, flags)

	timed, isTimed := ev.(hebcal.TimedEvent)
	item := orderedObj{}

	// title (+ ": time" for candles/havdalah only); date
	title := renderBriefLike(ev, locale)
	if flags&event.MOLAD != 0 {
		// @hebcal/core renders the announcement and the Shabbat Mevarchim
		// memo from the same Molad.render(), so keep them one string
		title = mevarchimMoladMemo(hd, locale, loc.CC, il, hour12)
	}
	if isTimed {
		// @hebcal/core rounds candle-lighting and havdalah to the whole minute.
		t := roundTime(timed.EventTime)
		if isCandleOrHavdalah(desc) {
			title = title + ": " + reformatTimeStr(t.Format("15:04"), "pm", loc.CC, il, hour12)
		}
		item = append(item, jsonKV{"title", title})
		item = append(item, jsonKV{"date", t.Format("2006-01-02T15:04:05-07:00")})
	} else {
		item = append(item, jsonKV{"title", title})
		item = append(item, jsonKV{"date", isoGreg(hd)})
		item = append(item, jsonKV{"hdate", hdateString(hd)})
	}

	item = append(item, jsonKV{"category", cat})
	if subcat != "" {
		item = append(item, jsonKV{"subcat", subcat})
	}
	if cat == "holiday" && flags&event.CHAG != 0 {
		item = append(item, jsonKV{"yomtov", true})
	}
	if title != desc {
		item = append(item, jsonKV{"title_orig", desc})
	}

	// eventToClassicApiObject deletes `hebrew` again on a molad announcement
	if flags&event.MOLAD == 0 {
		if hebrew := renderBriefLike(ev, "he-x-NoNikud"); hebrew != "" {
			item = append(item, jsonKV{"hebrew", hebrew})
		}
	}

	// The holiday an event stands for, which is the event itself except for
	// the Chanukah candle-lighting: @hebcal/core models that as a
	// ChanukahEvent subclass carrying a time, so it keeps the holiday's own
	// basename and URL, while hebcal-go models it as a TimedEvent that
	// repeats the holiday's description and links back to it.
	holiday := ev
	if isTimed {
		if he, ok := linkedHoliday(timed); ok {
			holiday = he
		}
	}

	// leyning and link: neither for candle-lighting or havdalah. Leyning is
	// also skipped for every other timed event, matching
	// getLeyningForHoliday(), which rejects anything with an eventTime.
	if !isCandleOrHavdalah(desc) {
		if !isTimed {
			if ley := shabbatLeyning(hd, flags, desc, readings); ley != nil {
				item = append(item, jsonKV{"leyning", ley})
			}
		}
		if link := shabbatLink(holiday, hd, il); link != "" {
			item = append(item, jsonKV{"link", link})
		}
	}

	if flags&event.MOLAD != 0 {
		item = append(item, jsonKV{"molad", moladObj(hd)})
	}

	if hdp && !isTimed {
		item = append(item, jsonKV{"heDateParts", makeHeDateParts(hd)})
	}

	// memo priority (per eventToClassicApiObject):
	//   ev.memo (molad for Shabbat Mevarchim) || getHolidayDescription()
	//   || (for timed events) linkedEvent.render()
	memo := ""
	if flags&event.SHABBAT_MEVARCHIM != 0 {
		memo = mevarchimMoladMemo(hd, locale, loc.CC, il, hour12)
	}
	if memo == "" {
		memo = holidayMemo(desc, eventBasename(holiday), memoLocaleName(locale))
	}
	// As of hebcal-go v0.17.0, erev-Shabbat candle-lighting carries the
	// upcoming parsha as its LinkedEvent, matching @hebcal/core.
	if memo == "" && isTimed && timed.LinkedEvent != nil {
		memo = smartApostrophe(timed.LinkedEvent.Render(locale))
	}
	if memo != "" {
		item = append(item, jsonKV{"memo", memo})
	}
	return item
}

// memoLocaleName collapses a locale to the two catalogs that carry MEMO/molad
// strings: Hebrew locales use "he", everything else "en".
func memoLocaleName(locale string) string {
	switch strings.ToLower(locale) {
	case "he", "he-x-nonikud", "h":
		return "he"
	}
	return "en"
}

// holidayMemo ports getHolidayDescription: MEMO:<desc>, then MEMO:<basename>.
func holidayMemo(desc, basename, localeName string) string {
	if s := lookupMemo("MEMO:"+desc, localeName); s != "" {
		return s
	}
	if basename != desc {
		if s := lookupMemo("MEMO:"+basename, localeName); s != "" {
			return s
		}
	}
	return ""
}

// lookupMemo looks up a MEMO catalog key, treating a result equal to the key
// as "not found" (the Go locales package echoes the key with ok=true for
// unknown English keys).
func lookupMemo(key, localeName string) string {
	if s, ok := locales.LookupTranslation(key, localeName); ok && s != key {
		return s
	}
	return ""
}

// announcedMolad returns the molad of the month announced on the given
// Shabbat, and that month's untranslated English name. Both the Shabbat
// Mevarchim memo and the separate molad=on announcement are about the
// following month.
func announcedMolad(hd hdate.HDate) (molad.Molad, string) {
	hyear := hd.Year()
	monNext := hd.Month() + 1
	if int(hd.Month()) == hdate.MonthsInYear(hyear) {
		monNext = hdate.Nisan
	}
	return molad.New(hyear, monNext), monthNameEn(monNext, hyear)
}

// moladDesc is the untranslated description of a molad announcement,
// e.g. "Molad Elul 5786". hebcal-go's molad event renders the whole sentence
// instead, and keeps its month and year unexported.
func moladDesc(hd hdate.HDate) string {
	_, monthEn := announcedMolad(hd)
	return fmt.Sprintf("Molad %s %d", monthEn, hd.Year())
}

// moladObj builds the `molad` member of a molad announcement item, matching
// eventToClassicApiObject.
func moladObj(hd hdate.HDate) orderedObj {
	m, monthEn := announcedMolad(hd)
	return orderedObj{
		{"hy", hd.Year()},
		{"hm", monthEn},
		{"dow", int(m.Date.Weekday())},
		{"hour", m.Hours},
		{"minutes", m.Minutes},
		{"chalakim", m.Chalakim},
		{"instant", moladInstant(m)},
	}
}

// moladInstant renders the exact moment of the molad as a UTC timestamp,
// porting getMoladAsDate() in @hebcal/core. The molad's wall clock is
// Jerusalem *mean solar* time, so the reading is taken in fixed UTC+2 (never
// Israel daylight time) and then shifted back by the local mean time offset
// of Har Habayis: longitude 35.2354° is 5.2354° east of the meridian its
// zone is named for, which at 4 minutes per degree is 20 minutes 56.496
// seconds.
func moladInstant(m molad.Molad) string {
	const lmtOffset = 1256496 * time.Millisecond
	// chalakim are 10/3 of a second each
	nanos := int64(m.Chalakim) * 10 * int64(time.Second) / 3
	gy, gm, gd := m.Date.Greg()
	t := time.Date(gy, gm, gd, m.Hours, m.Minutes, 0, 0, time.FixedZone("", 2*60*60)).
		Add(time.Duration(nanos) - lmtOffset).UTC()
	// JavaScript's Temporal.Instant.toJSON() prints milliseconds and trims
	// trailing zeros, so ".170" comes out as ".17"
	frac := strings.TrimRight(fmt.Sprintf("%03d", t.Nanosecond()/int(time.Millisecond)), "0")
	if frac != "" {
		frac = "." + frac
	}
	return t.Format("2006-01-02T15:04:05") + frac + "Z"
}

// mevarchimMoladMemo reproduces @hebcal/core MevarchimChodeshEvent.memo, i.e.
// Molad.render(locale, options) for the announced (next) month. locale is the
// aliased request locale (e.g. "he", "ru", "en").
func mevarchimMoladMemo(hd hdate.HDate, locale, cc string, il bool, hour12 *bool) string {
	m, monthEn := announcedMolad(hd)
	// Hebrew uses a distinct sentence structure; hebcal-go's moladEvent renders
	// it identically to @hebcal/core, so reuse it.
	if locale == "he" || locale == "he-x-nonikud" {
		return event.NewMoladEvent(m.Date, m, monthEn, cc).Render(locale)
	}
	// Other locales: "Molad <month>: <weekday>, <time> and <n> chalakim", with
	// the month localized and the time formatted per the location's country.
	// Molad.render() curls the apostrophe in the month name ("Sh'vat" =>
	// "Sh’vat"), and only there — the Hebrew sentence above does not.
	month := smartApostrophe(gettext(monthEn, locale))
	dow := moladDayName(m.Date.Weekday(), locale)
	fmtTime := reformatTimeStr(fmt.Sprintf("%d:%02d", m.Hours, m.Minutes), "pm", cc, il, hour12)
	result := gettext("Molad", locale) + " " + month + ": " + dow + ", " + fmtTime
	if m.Chalakim != 0 {
		result += " " + gettext("and", locale) + " " + strconv.Itoa(m.Chalakim) + " " + gettext("chalakim", locale)
	}
	return result
}

// moladDayName returns the weekday of the molad. @hebcal/core's
// getDayNames() carries its own French names for this one sentence and falls
// back to English everywhere else; the Hebrew names live in hebcal-go's
// molad renderer, which handles the Hebrew sentence.
func moladDayName(dow time.Weekday, locale string) string {
	if locale == "fr" {
		return [...]string{"Dimanche", "Lundi", "Mardi", "Mercredi",
			"Jeudi", "Vendredi", "Samedi"}[dow]
	}
	return dow.String()
}

// normMonth normalizes hebcal-go's "Tammuz" to the "Tamuz" spelling used by
// @hebcal/core (and this API), in English strings only (a no-op elsewhere).
// The 17th of Tammuz fast is the one place @hebcal/core keeps the double
// "m", so "Tzom Tammuz" is left alone: it is the event description the
// classic API reports as title_orig, the key the MEMO catalog and the event
// URL are built from, and the name /leyning uses for the fast's reading.
func normMonth(s string) string {
	if strings.Contains(s, "Tzom Tammuz") {
		return s
	}
	return strings.ReplaceAll(s, "Tammuz", "Tamuz")
}

// descOf returns the canonical (untranslated) description used for category
// lookup and title_orig.
func descOf(ev event.CalEvent) string {
	// hebcal-go's molad event has no description of its own: Render() returns
	// the whole announcement sentence and the month and year it was built
	// from are unexported, so rebuild @hebcal/core's "Molad <month> <year>".
	if ev.GetFlags()&event.MOLAD != 0 {
		return moladDesc(ev.GetDate())
	}
	switch e := ev.(type) {
	case hebcal.TimedEvent:
		return e.Desc
	case event.HolidayEvent:
		return normMonth(e.Desc)
	default:
		return normMonth(ev.Render("en"))
	}
}

// renderBriefLike renders an event's brief title in the given locale. For
// timed events it returns the base label (no time); the caller appends time.
func renderBriefLike(ev event.CalEvent, locale string) string {
	if e, ok := ev.(hebcal.TimedEvent); ok {
		return timedEventLabel(e, locale)
	}
	// Rosh Hashana renders the year as a number in every locale (matching the
	// JS API), rather than hebcal-go's gematriya.
	if he, ok := ev.(event.HolidayEvent); ok &&
		he.Date.Month() == hdate.Tishrei && he.Date.Day() == 1 &&
		strings.HasPrefix(he.Desc, "Rosh Hashana") {
		return gettext("Rosh Hashana", locale) + " " + strconv.Itoa(he.Date.Year())
	}
	r := ev.Render(locale)
	if ev.GetFlags()&event.SHABBAT_MEVARCHIM != 0 {
		r = stripMevarchimPrefix(r)
	}
	return smartApostrophe(normMonth(r))
}

// timedEventLabel renders a timed event's label without the ": <time>"
// suffix hebcal-go appends, since the caller formats the time itself for the
// location's country. Going through Render rather than translating Desc
// directly is what keeps the "(50 min)" on a havdalah pinned to a fixed
// number of minutes after sunset (m=<min>): hebcal-go adds it from the
// event's sunsetOffset, which is unexported, so the label cannot be rebuilt
// from the outside. @hebcal/core spells it the same way, in the title and in
// the Hebrew rendering both.
func timedEventLabel(ev hebcal.TimedEvent, locale string) string {
	s := ev.Render(locale)
	// the time is last and never contains ": ", so a Chanukah candle event
	// ("Chanukah: 8 Candles: 4:37pm") splits correctly too
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[:i]
	}
	return s
}

// stripMevarchimPrefix drops the first (space-delimited) word from a Shabbat
// Mevarchim title, matching MevarchimChodeshEvent.renderBrief across locales
// (e.g. "Shabbat "/"שַׁבַּת "/"Шаббат "/"Shabbos ").
func stripMevarchimPrefix(s string) string {
	if i := strings.Index(s, " "); i >= 0 {
		return s[i+1:]
	}
	return s
}

// isCandleOrHavdalah reports whether a timed event's title should carry a
// ": time" suffix (only candle-lighting and havdalah do).
func isCandleOrHavdalah(desc string) bool {
	return desc == "Candle lighting" || desc == "Havdalah" || strings.HasPrefix(desc, "Havdalah (")
}

// shabbatLeyning returns the Torah reading for an event, or nil when it has
// none (or when readings were not requested). It reproduces the lookup in
// eventToClassicApiObject: a parsha ha-shavua event gets
// getLeyningForParshaHaShavua(), every other event gets
// getLeyningForHoliday(). Timed events are never passed here, matching the
// JS side, where getLeyningForHoliday() rejects anything with an eventTime.
//
// The triennial cycle is added for parsha events only, and only from Hebrew
// year 5745 on, per hebcal-web src/myEventsToClassicApi.js.
func shabbatLeyning(hd hdate.HDate, flags event.HolidayFlags, desc string,
	readings map[string][]*leyningReading) orderedObj {
	if readings == nil {
		return nil
	}
	onDate := readings[isoGreg(hd)]
	if len(onDate) == 0 {
		return nil
	}
	// @hebcal/rest-api tests the mask for equality, so an event merely
	// carrying PARSHA_HASHAVUA alongside other flags is not a parsha.
	if flags == event.PARSHA_HASHAVUA {
		r := findParshaReading(onDate)
		if r == nil {
			return nil
		}
		return formatLeyning(r, hd.Year() >= 5745)
	}
	r := findHolidayReading(onDate, desc)
	if r == nil {
		return nil
	}
	return formatLeyning(r, false)
}

// linkedHoliday returns the holiday a timed event stands for, rather than one
// it is merely attached to. The Chanukah candle-lighting *is* the holiday, so
// hebcal-go gives it the holiday's own description and links back to it;
// erev-Yom-Tov candle-lighting and the fast start/end times link to a holiday
// but describe something else, and are not holidays themselves.
func linkedHoliday(timed hebcal.TimedEvent) (event.HolidayEvent, bool) {
	if timed.LinkedEvent == nil {
		return event.HolidayEvent{}, false
	}
	he, ok := timed.LinkedEvent.(event.HolidayEvent)
	if !ok || he.Desc != timed.Desc {
		return event.HolidayEvent{}, false
	}
	return he, true
}

// eventBasename ports @hebcal/core's basename(), which strips the qualifiers
// that distinguish days of one holiday ("Sukkot III (CH”M)" => "Sukkot") so
// related events share a description and a URL.
//
// Rosh Chodesh overrides it to keep the whole description, and must: the
// generic rule strips a trailing Roman numeral, which would turn "Rosh
// Chodesh Adar I" into "Rosh Chodesh Adar" — a different month in a leap
// year, and a URL that 404s.
func eventBasename(ev event.CalEvent) string {
	if ev.GetFlags()&event.ROSH_CHODESH != 0 {
		return descOf(ev)
	}
	return normMonth(ev.Basename())
}

// shabbatLink builds the shortened, tracked hebcal.com URL for an event.
func shabbatLink(ev event.CalEvent, hd hdate.HDate, il bool) string {
	flags := ev.GetFlags()
	if flags&event.SHABBAT_MEVARCHIM != 0 {
		return "" // Shabbat Mevarchim events have no URL
	}
	switch {
	case flags&event.PARSHA_HASHAVUA != 0:
		return sedrotShortURL(hd, il)
	default:
		if he, ok := ev.(event.HolidayEvent); ok {
			return holidayShortURL(he, il)
		}
	}
	return ""
}

// sedrotShortURL builds /s/<hebYear>[i]/<parshaId>[d]?us=js&um=api.
func sedrotShortURL(sat hdate.HDate, il bool) string {
	s := sedra.New(sat.Year(), il)
	parsha := s.Lookup(sat)
	if parsha.Chag || len(parsha.Num) == 0 {
		return ""
	}
	path := fmt.Sprintf("/s/%d", sat.Year())
	suffix := ""
	if il {
		suffix = "i"
	}
	path += suffix + "/" + fmt.Sprintf("%d", parsha.Num[0])
	if len(parsha.Num) == 2 {
		path += "d"
	}
	return "https://hebcal.com" + path + "?us=js&um=api"
}

// holidayShortURL builds /h/<slug>-<year>?us=js&um=api.
func holidayShortURL(he event.HolidayEvent, il bool) string {
	gy, gm, gd := he.Date.ProlepticGreg()
	if gy < 100 || gy > 2999 {
		return ""
	}
	var suffix string
	switch {
	case he.Desc == "Asara B'Tevet":
		suffix = fmt.Sprintf("%04d%02d%02d", gy, int(gm), gd)
	case strings.HasPrefix(he.Desc, "Chanukah"):
		year := gy
		if gm == time.January {
			year--
		}
		suffix = fmt.Sprintf("%d", year)
	default:
		suffix = fmt.Sprintf("%d", gy)
	}
	u := "https://hebcal.com/h/" + urlFriendly(eventBasename(he)) + "-" + suffix
	q := "?us=js&um=api"
	if il {
		q = "?i=on&us=js&um=api"
	}
	return u + q
}

// categoriesOf ports @hebcal/rest-api getEventCategories + @hebcal/core
// getCategories (including the HolidayEvent override): [category, subcat].
func categoriesOf(ev event.CalEvent, desc string, flags event.HolidayFlags) (string, string) {
	// TimedEvents are keyed by their description (@hebcal/core
	// TimedEvent.getCategories)
	switch {
	case desc == "Candle lighting":
		return "candles", ""
	case desc == "Havdalah" || strings.HasPrefix(desc, "Havdalah ("):
		return "havdalah", ""
	case desc == "Fast begins" || desc == "Fast ends":
		return "zmanim", "fast"
	case desc == "Sof Zman Achilat Chametz":
		return "zmanim", "achilasChametz"
	case desc == "Biur Chametz":
		return "zmanim", "biurChametz"
	}
	// getEventCategories special cases
	if desc == "Purim" || desc == "Erev Purim" || strings.HasPrefix(desc, "Chanukah: ") {
		return "holiday", "major"
	}
	// base Event.getCategories via the flagToCategory table (first match wins)
	if cat, sub, ok := baseCategory(flags); ok {
		return cat, sub
	}
	// HolidayEvent.getCategories override, reached when the base is "unknown"
	if he, ok := ev.(event.HolidayEvent); ok {
		if he.CholHaMoedDay != 0 {
			return "holiday", "major" // (+ "cholhamoed", unused by the classic API)
		}
	}
	switch desc {
	case "Lag BaOmer", "Leil Selichot", "Pesach Sheni", "Erev Purim", "Purim Katan",
		"Shushan Purim", "Tu B'Av", "Tu BiShvat", "Rosh Hashana LaBehemot":
		return "holiday", "minor"
	}
	return "holiday", "major"
}

// baseCategory ports the flagToCategory table of @hebcal/core Event.getCategories.
// ok is false when no flag matches (the caller then applies the HolidayEvent
// fallback).
func baseCategory(flags event.HolidayFlags) (string, string, bool) {
	type entry struct {
		flag     event.HolidayFlags
		cat, sub string
	}
	table := []entry{
		{event.MAJOR_FAST, "holiday", "major"}, // + "fast", unused
		{event.CHANUKAH_CANDLES, "holiday", "minor"},
		{event.HEBREW_DATE, "hebdate", ""},
		{event.MINOR_FAST, "holiday", "fast"},
		{event.MINOR_HOLIDAY, "holiday", "minor"},
		{event.MODERN_HOLIDAY, "holiday", "modern"},
		{event.MOLAD, "molad", ""},
		{event.OMER_COUNT, "omer", ""},
		{event.PARSHA_HASHAVUA, "parashat", ""},
		{event.ROSH_CHODESH, "roshchodesh", ""},
		{event.SHABBAT_MEVARCHIM, "mevarchim", ""},
		{event.SPECIAL_SHABBAT, "holiday", "shabbat"},
		{event.USER_EVENT, "user", ""},
	}
	for _, e := range table {
		if flags&e.flag != 0 {
			return e.cat, e.sub, true
		}
	}
	return "", "", false
}

// reformatTimeStr ports @hebcal/core reformatTimeStr: converts 24h "HH:MM" to
// 12h "h:MMpm" for countries that use 12-hour clocks, else returns unchanged.
// hour12 is the h12 query override and wins over the country when set: false
// forces the 24-hour form, true forces the 12-hour one.
func reformatTimeStr(timeStr, suffix, cc string, il bool, hour12 *bool) string {
	if hour12 != nil && !*hour12 {
		return timeStr
	}
	if cc == "" {
		if il {
			cc = "IL"
		} else {
			cc = "US"
		}
	}
	if (hour12 == nil || !*hour12) && !hour12Countries[cc] {
		return timeStr
	}
	hm := strings.SplitN(timeStr, ":", 2)
	if len(hm) != 2 {
		return timeStr
	}
	hour, _ := parseInt(hm[0])
	if hour < 12 {
		suffix = strings.NewReplacer("p", "a", "P", "A").Replace(suffix)
		if hour == 0 {
			hour = 12
		}
		return fmt.Sprintf("%d:%s%s", hour, hm[1], suffix)
	}
	if hour > 12 {
		hour = hour % 12
	}
	return fmt.Sprintf("%d:%s%s", hour, hm[1], suffix)
}

var hour12Countries = map[string]bool{
	"US": true, "CA": true, "BR": true, "AU": true, "NZ": true, "DO": true,
	"PR": true, "GR": true, "IN": true, "KR": true, "NP": true, "ZA": true,
}
