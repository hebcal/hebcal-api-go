package main

// shabbatHandler implements the /shabbat JSON API, a Go port of shabbatApp in
// hebcal-web src/shabbat.js. It returns this week's (or a given week's)
// candle-lighting, Torah portion, havdalah, and related events for a location.
//
// Scope: only cfg=json with leyning={off,0} is supported. Any other cfg, or
// leyning left on (the default), returns 501 Not Implemented.

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/molad"
	"github.com/hebcal/hebcal-go/sedra"
	"github.com/hebcal/locales"
)

// parseFloat parses a query float, returning an error for empty/invalid input.
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// shabbatQueryDate resolves the requested date: dt=YYYY-MM-DD, or gy/gm/gd, or
// (when none given) "today" in the location timezone (isToday=true).
func shabbatQueryDate(q url.Values) (gregDate, bool, error) {
	if dt := strings.TrimSpace(q.Get("dt")); dt != "" {
		d, err := isoDateStringToDate(dt)
		return d, false, err
	}
	if q.Get("gy") != "" || q.Get("gm") != "" || q.Get("gd") != "" {
		d, err := makeGregDate(q.Get("gy"), q.Get("gm"), q.Get("gd"))
		return d, false, err
	}
	return gregDate{}, true, nil
}

// shabbatWeekRange returns the [start, endOfWeek] Gregorian window for the
// Shabbat listing, ported from shabbatWeekRange + getStartAndEnd in
// hebcal-web src/dateUtil.js. If isToday, "now" in the location tz is used.
func shabbatWeekRange(dt gregDate, isToday bool, tzid string) (gregDate, gregDate, error) {
	loc, err := time.LoadLocation(tzid)
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
	loc, err := time.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	offset := 7 - int(now.Weekday()) // Sunday -> +7 (next week), matches dayjs day(7)
	sun := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, offset)
	w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
	w.Header().Set("Expires", sun.UTC().Format(http.TimeFormat))
}

// shabbatHandler implements GET /shabbat (cfg=json, leyning=off only).
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

	// Scope gate: only cfg=json with leyning disabled is implemented.
	cfg := q.Get("cfg")
	leyning := q.Get("leyning")
	leyningOff := leyning == "off" || leyning == "0"
	if cfg != "json" || !leyningOff {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write(jsonMarshal(map[string]string{
			"error": "Only cfg=json with leyning=off is supported by this endpoint",
		}))
		return
	}
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

	il := loc.isIsrael()
	lg := q.Get("lg")
	locale := strings.ToLower(aliasLocale(lg))
	opts := shabbatCalOptions(loc, il, start, end, q)
	events, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		app.writeZmanimError(w, badRequest("%s", err.Error()))
		return
	}
	// m=0 suppresses havdalah (there is no CalOption for this, so filter).
	if q.Get("m") == "0" {
		events = filterOutHavdalah(events)
	}
	if len(events) == 0 {
		app.writeZmanimError(w, badRequest("Bad request: no events"))
		return
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

	hdp := q.Get("hdp") == "1"
	body := shabbatResponse(events, loc, il, locale, lg, hdp)
	w.Write(jsonMarshal(body))
}

// shabbatCalOptions builds the hebcal.CalOptions for the Shabbat week.
func shabbatCalOptions(loc *geoLocation, il bool, start, end gregDate, q url.Values) hebcal.CalOptions {
	zloc := loc.zmanimLocation()
	opts := hebcal.CalOptions{
		Location:         &zloc,
		IL:               il,
		CandleLighting:   true,
		Sedrot:           true,
		ShabbatMevarchim: true,
		Start:            hdate.FromProlepticGregorian(start.Year, start.Month, start.Day),
		End:              hdate.FromProlepticGregorian(end.Year, end.Month, end.Day),
	}
	// candle-lighting minutes before sunset
	if b, ok := parseInt(q.Get("b")); ok {
		opts.CandleLightingMins = b
	} else {
		opts.CandleLightingMins = candleLightingDefaultMins(loc)
	}
	// havdalah: td=<deg> or M=on => degrees; m=<min> => fixed minutes
	if td, err := parseFloat(q.Get("td")); err == nil && td > 0 {
		opts.HavdalahDeg = td
	} else if m, ok := parseInt(q.Get("m")); ok && m > 0 {
		opts.HavdalahMins = m
	} else {
		opts.HavdalahDeg = 8.5 // M=on default (3 small stars)
	}
	return opts
}

// candleLightingDefaultMins mirrors hebcal-web's location-specific defaults.
func candleLightingDefaultMins(loc *geoLocation) int {
	switch loc.GeonameID {
	case 281184: // Jerusalem
		return 40
	case 294801, 293067: // Haifa, Zikhron Ya'akov
		return 30
	}
	return 18
}

// filterOutHavdalah drops YOM_TOV_ENDS (havdalah) timed events, for m=0.
func filterOutHavdalah(events []event.CalEvent) []event.CalEvent {
	out := events[:0]
	for _, ev := range events {
		if ev.GetFlags()&event.YOM_TOV_ENDS != 0 {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// shabbatResponse builds the ordered top-level JSON object.
func shabbatResponse(events []event.CalEvent, loc *geoLocation, il bool, locale, lg string, hdp bool) orderedObj {
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
		items = append(items, shabbatItem(ev, loc, il, locale, lg, hdp))
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
func shabbatItem(ev event.CalEvent, loc *geoLocation, il bool, locale, lg string, hdp bool) orderedObj {
	flags := ev.GetFlags()
	hd := ev.GetDate()
	desc := descOf(ev)
	cat, subcat := categoriesOf(ev, desc, flags)

	timed, isTimed := ev.(hebcal.TimedEvent)
	item := orderedObj{}

	// title (+ ": time" for candles/havdalah only); date
	title := renderBriefLike(ev, locale)
	if isTimed {
		// @hebcal/core rounds candle-lighting and havdalah to the whole minute.
		t := roundTime(timed.EventTime)
		if isCandleOrHavdalah(desc) {
			title = title + ": " + reformatTimeStr(t.Format("15:04"), "pm", loc.CC, il)
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

	if hebrew := renderBriefLike(ev, "he-x-NoNikud"); hebrew != "" {
		item = append(item, jsonKV{"hebrew", hebrew})
	}

	// link (not for candles/havdalah)
	if !isTimed {
		if link := shabbatLink(ev, hd, il); link != "" {
			item = append(item, jsonKV{"link", link})
		}
	}

	if hdp && !isTimed {
		item = append(item, jsonKV{"heDateParts", makeHeDateParts(hd)})
	}

	// memo priority (per eventToClassicApiObject):
	//   ev.memo (molad for Shabbat Mevarchim) || getHolidayDescription()
	//   || (for timed events) linkedEvent.render()
	memo := ""
	if flags&event.SHABBAT_MEVARCHIM != 0 {
		memo = mevarchimMoladMemo(hd, locale, loc.CC, il)
	}
	if memo == "" {
		memo = holidayMemo(desc, normMonth(ev.Basename()), memoLocaleName(locale))
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

// mevarchimMoladMemo reproduces @hebcal/core MevarchimChodeshEvent.memo, i.e.
// Molad.render(locale, options) for the announced (next) month. locale is the
// aliased request locale (e.g. "he", "ru", "en").
func mevarchimMoladMemo(hd hdate.HDate, locale, cc string, il bool) string {
	hyear := hd.Year()
	monNext := hd.Month() + 1
	if int(hd.Month()) == hdate.MonthsInYear(hyear) {
		monNext = hdate.Nisan
	}
	m := molad.New(hyear, monNext)
	monthEn := monthNameEn(monNext, hyear)
	// Hebrew uses a distinct sentence structure; hebcal-go's moladEvent renders
	// it identically to @hebcal/core, so reuse it.
	if locale == "he" || locale == "he-x-nonikud" {
		return event.NewMoladEvent(m.Date, m, monthEn, cc).Render(locale)
	}
	// Other locales: "Molad <month>: <weekday>, <time> and <n> chalakim", with
	// the month localized and the time formatted per the location's country.
	month := gettext(monthEn, locale)
	dow := m.Date.Weekday().String()
	fmtTime := reformatTimeStr(fmt.Sprintf("%d:%02d", m.Hours, m.Minutes), "pm", cc, il)
	result := gettext("Molad", locale) + " " + month + ": " + dow + ", " + fmtTime
	if m.Chalakim != 0 {
		result += " " + gettext("and", locale) + " " + strconv.Itoa(m.Chalakim) + " " + gettext("chalakim", locale)
	}
	return result
}

// normMonth normalizes hebcal-go's "Tammuz" to the "Tamuz" spelling used by
// @hebcal/core (and this API), in English strings only (a no-op elsewhere).
func normMonth(s string) string {
	return strings.ReplaceAll(s, "Tammuz", "Tamuz")
}

// descOf returns the canonical (untranslated) description used for category
// lookup and title_orig.
func descOf(ev event.CalEvent) string {
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
		if s, ok := locales.LookupTranslation(e.Desc, locale); ok {
			return s
		}
		return e.Desc
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
	u := "https://hebcal.com/h/" + urlFriendly(normMonth(he.Basename())) + "-" + suffix
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
func reformatTimeStr(timeStr, suffix, cc string, il bool) string {
	if cc == "" {
		if il {
			cc = "IL"
		} else {
			cc = "US"
		}
	}
	if !hour12Countries[cc] {
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
