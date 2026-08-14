// Package shabbat implements the /shabbat JSON API, a Go port of shabbatApp in
// hebcal-web src/shabbat.js. It builds this week's (or a given week's)
// candle-lighting, Torah portion, havdalah, and related events for a location.
//
// Scope: only cfg=json is supported by the handler; any other cfg returns 501
// Not Implemented. Torah readings (the default, suppressed with
// leyning={off,0}) come from the readings-svc sidecar's /leyning endpoint;
// see internal/repository/readings.
package shabbat

import (
	"encoding/json"
	"fmt"
	"net/url"
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

	"github.com/hebcal/hebcal-api-go/internal/config"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/service/location"
	zmanimsvc "github.com/hebcal/hebcal-api-go/internal/service/zmanim"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// QueryDate resolves the requested date and reports whether it came
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
func QueryDate(q url.Values) (model.GregDate, bool, error) {
	for _, param := range []string{"dt", "date", "start"} {
		if s := strings.TrimSpace(q.Get(param)); s != "" {
			d, err := model.IsoDateStringToDate(s)
			return d, false, err
		}
	}
	if q.Get("gy") != "" || q.Get("gm") != "" || q.Get("gd") != "" {
		d, err := model.MakeGregDate(q.Get("gy"), q.Get("gm"), q.Get("gd"))
		return d, false, err
	}
	return model.GregDate{}, true, nil
}

// QueryLang returns the requested `lg`, falling back to the much older
// a=on spelling of Ashkenazi transliteration. Ported from makeHebcalOptions()
// in hebcal-web src/calendar.js, which rewrites a=on to lg=a whenever lg is
// absent; the /converter route has no such fallback in hebcal-web, so this
// stays local to /shabbat. (Resolving the short codes themselves is
// model.AliasLocale's job, and both routes share it.)
func QueryLang(q url.Values) string {
	if lg := q.Get("lg"); lg != "" {
		return lg
	}
	if q.Get("a") == "on" {
		return "a"
	}
	return ""
}

// WeekRange returns the [start, endOfWeek] Gregorian window for the
// Shabbat listing, ported from shabbatWeekRange + getStartAndEnd in
// hebcal-web src/dateUtil.js. If isToday, "now" in the location tz is used.
func WeekRange(dt model.GregDate, isToday bool, tzid string) (model.GregDate, model.GregDate, error) {
	loc, err := zmanim.LoadLocation(tzid)
	if err != nil {
		return model.GregDate{}, model.GregDate{}, model.BadRequest("Invalid time zone specified: %s", tzid)
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
	start := model.GregDate{Year: base.Year(), Month: base.Month(), Day: base.Day()}
	endD := model.GregDate{Year: end.Year(), Month: end.Month(), Day: end.Day()}
	return start, endD, nil
}

// CalOptions builds the hebcal.CalOptions for the Shabbat week.
func CalOptions(loc *geodb.Location, il bool, start, end model.GregDate, q url.Values,
	c Candles) hebcal.CalOptions {
	zloc := loc.ZmanimLocation()
	opts := hebcal.CalOptions{
		Location:         &zloc,
		IL:               il,
		CandleLighting:   true,
		Sedrot:           true,
		ShabbatMevarchim: true,
		UseElevation:     jsutil.IsOn(q.Get("ue")),
		Molad:            jsutil.IsOn(q.Get("molad")),
		Start:            hdate.FromProlepticGregorian(start.Year, start.Month, start.Day),
		End:              hdate.FromProlepticGregorian(end.Year, end.Month, end.Day),
	}
	opts.CandleLightingMins = c.CandleMins
	opts.HavdalahMins = c.HavdalahMins
	opts.HavdalahDeg = c.HavdalahDeg
	opts.SuppressHavdalah = c.NoHavdalah
	return opts
}

// Candles holds the resolved candle-lighting and havdalah settings.
// HavdalahMins and HavdalahDeg are mutually exclusive; both zero means no
// havdalah at all.
type Candles struct {
	CandleMins   int
	HavdalahMins int
	HavdalahDeg  float64
	// NoHavdalah marks m=0, which asks for no havdalah at all. It becomes
	// CalOptions.SuppressHavdalah: a zero HavdalahMins means "use the default
	// tzeit" to hebcal-go, so the intent needs its own flag.
	NoHavdalah bool
	// AtSunset marks b=0, which asks for candle-lighting exactly at sunset.
	// hebcal-go cannot express it (CheckCandleOptions rewrites a zero
	// CandleLightingMins to the 18/20-minute default), so the caller fixes
	// the times up afterwards.
	AtSunset bool
}

// CandleOptions resolves b, m, M and td into candle-lighting and havdalah
// settings, porting the precedence rules in makeHebcalOptions() (hebcal-web
// src/calendar.js) together with the default in shabbatApp().
func CandleOptions(q url.Values, loc *geodb.Location) Candles {
	var c Candles
	mStr, tdStr := q.Get("m"), q.Get("td")
	mIsOn := mStr == "on" // the lowercase spelling of M=on
	if mIsOn {
		mStr = ""
	}
	// shabbatApp() defaults to tzeit when the request names no preference
	havdalahTzeit := mIsOn || jsutil.IsOn(q.Get("M")) ||
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
		if deg, err := jsutil.ParseFloat(tdStr); err == nil && deg != 0 {
			c.HavdalahDeg = deg
			havdalahTzeit = false
			mStr = ""
		}
	}
	if havdalahTzeit {
		c.HavdalahDeg = 8.5 // 3 small stars
		mStr = ""
	}
	if mStr != "" {
		if m, ok := jsutil.ParseInt(mStr); ok {
			if m == 0 {
				// @hebcal/core drops the havdalah outright at zero minutes
				c.NoHavdalah = true
			} else {
				c.HavdalahMins = m
			}
		}
	}
	if c.HavdalahMins == 0 && c.HavdalahDeg == 0 {
		// nothing survived, so @hebcal/core falls back on Zmanim.tzeit(),
		// whose own default is 8.5 degrees
		c.HavdalahDeg = 8.5
	}

	// candle-lighting minutes before sunset. In Israel an absent b -- or the
	// b=18 that the web form submits by default -- yields the local custom.
	c.CandleMins = locationDefaultCandleMins(loc)
	if b, ok := jsutil.ParseInt(q.Get("b")); ok {
		if !(loc.IsIsrael() && b == DefaultCandleMins && c.CandleMins != DefaultCandleMins) {
			c.CandleMins = b
		}
	}
	if c.CandleMins == 0 {
		c.AtSunset = true
		c.CandleMins = DefaultCandleMins // placeholder; times are fixed up later
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
func locationDefaultCandleMins(loc *geodb.Location) int {
	if !loc.IsIsrael() {
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

// FilterYomTovOnly keeps only the Yom Tov days, for yto=on. Ported from
// makeHebrewCalendar() in hebcal-web src/calendar.js, which applies the
// filter after the calendar is built.
func FilterYomTovOnly(events []event.CalEvent) []event.CalEvent {
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

// MoveCandleLightingToSunset re-times candle-lighting to sunset itself, for
// b=0. @hebcal/core lights at sunsetOffset(0) in that case, but hebcal-go
// only reaches for a zero offset on the havdalah side and rewrites a zero
// CandleLightingMins to the default before the calendar is built, so the
// times have to be recomputed here. Drop this once hebcal-go can express it.
func MoveCandleLightingToSunset(events []event.CalEvent, opts *hebcal.CalOptions) {
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

// Response builds the ordered top-level JSON object. readings is nil
// when leyning is suppressed.
func Response(events []event.CalEvent, loc *geodb.Location, il bool, locale, lg string, hdp bool,
	hour12 *bool, leyning map[string][]readings.Item) jsutil.OrderedObj {
	body := jsutil.OrderedObj{
		{Key: "title", Val: title(events, loc)},
		{Key: "date", Val: time.Now().UTC().Format("2006-01-02T15:04:05.000Z")},
		{Key: "version", Val: config.APIVersion},
		{Key: "location", Val: location.ToPlainObj(loc, true)}, // /shabbat always includes elevation
	}
	if len(events) > 0 {
		first := events[0].GetDate()
		last := events[len(events)-1].GetDate()
		body = append(body, jsutil.KV{Key: "range", Val: jsutil.OrderedObj{
			{Key: "start", Val: model.IsoGreg(first)},
			{Key: "end", Val: model.IsoGreg(last)},
		}})
	}
	items := make([]interface{}, 0, len(events))
	for _, ev := range events {
		items = append(items, item(ev, loc, il, locale, lg, hdp, hour12, leyning))
	}
	body = append(body, jsutil.KV{Key: "items", Val: items})
	return body
}

// title ports getCalendarTitle for this endpoint: "Hebcal <city> <Month
// Year>" (or year range when the events span multiple years).
func title(events []event.CalEvent, loc *geodb.Location) string {
	title := "Hebcal " + loc.ShortName()
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

// item serializes one event to the classic-API item object. Ordered to
// match @hebcal/rest-api eventToClassicApiObject.
func item(ev event.CalEvent, loc *geodb.Location, il bool, locale, lg string, hdp bool,
	hour12 *bool, leyning map[string][]readings.Item) jsutil.OrderedObj {
	flags := ev.GetFlags()
	hd := ev.GetDate()
	desc := descOf(ev)
	cat, subcat := categoriesOf(ev, desc, flags)

	timed, isTimed := ev.(hebcal.TimedEvent)
	item := jsutil.OrderedObj{}

	// title (+ ": time" for candles/havdalah only); date
	title := renderBriefLike(ev, locale)
	if flags&event.MOLAD != 0 {
		// @hebcal/core renders the announcement and the Shabbat Mevarchim
		// memo from the same Molad.render(), so keep them one string
		title = mevarchimMoladMemo(hd, locale, loc.CC, il, hour12)
	}
	if isTimed {
		// @hebcal/core rounds candle-lighting and havdalah to the whole minute.
		t := zmanimsvc.RoundTime(timed.EventTime)
		if isCandleOrHavdalah(desc) {
			title = title + ": " + reformatTimeStr(t.Format("15:04"), "pm", loc.CC, il, hour12)
		}
		item = append(item, jsutil.KV{Key: "title", Val: title})
		item = append(item, jsutil.KV{Key: "date", Val: t.Format("2006-01-02T15:04:05-07:00")})
	} else {
		item = append(item, jsutil.KV{Key: "title", Val: title})
		item = append(item, jsutil.KV{Key: "date", Val: model.IsoGreg(hd)})
		item = append(item, jsutil.KV{Key: "hdate", Val: model.HDateString(hd)})
	}

	item = append(item, jsutil.KV{Key: "category", Val: cat})
	if subcat != "" {
		item = append(item, jsutil.KV{Key: "subcat", Val: subcat})
	}
	if cat == "holiday" && flags&event.CHAG != 0 {
		item = append(item, jsutil.KV{Key: "yomtov", Val: true})
	}
	if title != desc {
		item = append(item, jsutil.KV{Key: "title_orig", Val: desc})
	}

	// eventToClassicApiObject deletes `hebrew` again on a molad announcement
	if flags&event.MOLAD == 0 {
		if hebrew := renderBriefLike(ev, "he-x-NoNikud"); hebrew != "" {
			item = append(item, jsutil.KV{Key: "hebrew", Val: hebrew})
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
			if ley := itemLeyning(hd, flags, desc, leyning); len(ley) != 0 {
				item = append(item, jsutil.KV{Key: "leyning", Val: ley})
			}
		}
		if link := itemLink(holiday, hd, il); link != "" {
			item = append(item, jsutil.KV{Key: "link", Val: link})
		}
	}

	if flags&event.MOLAD != 0 {
		item = append(item, jsutil.KV{Key: "molad", Val: moladObj(hd)})
	}

	if hdp && !isTimed {
		item = append(item, jsutil.KV{Key: "heDateParts", Val: model.MakeHeDateParts(hd)})
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
		memo = jsutil.SmartApostrophe(timed.LinkedEvent.Render(locale))
	}
	if memo != "" {
		item = append(item, jsutil.KV{Key: "memo", Val: memo})
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
	return molad.New(hyear, monNext), model.MonthNameEn(monNext, hyear)
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
func moladObj(hd hdate.HDate) jsutil.OrderedObj {
	m, monthEn := announcedMolad(hd)
	return jsutil.OrderedObj{
		{Key: "hy", Val: hd.Year()},
		{Key: "hm", Val: monthEn},
		{Key: "dow", Val: int(m.Date.Weekday())},
		{Key: "hour", Val: m.Hours},
		{Key: "minutes", Val: m.Minutes},
		{Key: "chalakim", Val: m.Chalakim},
		{Key: "instant", Val: moladInstant(m)},
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
	month := jsutil.SmartApostrophe(model.Gettext(monthEn, locale))
	dow := moladDayName(m.Date.Weekday(), locale)
	fmtTime := reformatTimeStr(fmt.Sprintf("%d:%02d", m.Hours, m.Minutes), "pm", cc, il, hour12)
	result := model.Gettext("Molad", locale) + " " + month + ": " + dow + ", " + fmtTime
	if m.Chalakim != 0 {
		result += " " + model.Gettext("and", locale) + " " + strconv.Itoa(m.Chalakim) + " " + model.Gettext("chalakim", locale)
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
//
// model.FixMonthSpelling does the same job for the PDF calendars, restoring the
// fast after a blanket replace rather than skipping the whole string. The two
// agree on every string either service renders; this one stays as it is because
// its result feeds title_orig and the /leyning lookup.
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
		return model.Gettext("Rosh Hashana", locale) + " " + strconv.Itoa(he.Date.Year())
	}
	r := ev.Render(locale)
	if ev.GetFlags()&event.SHABBAT_MEVARCHIM != 0 {
		r = stripMevarchimPrefix(r)
	}
	return jsutil.SmartApostrophe(normMonth(r))
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

// itemLeyning returns the Torah reading for an event, or nil when it has
// none (or when readings were not requested).
//
// readings-svc builds each item with eventToClassicApiObject(), so the reading
// is already the object this response wants -- aliyot, torah, haftarah, the
// "| Shabbat Shekalim" reasons, and (for a parsha from Hebrew year 5745 on)
// the triennial cycle -- in @hebcal/rest-api's own key order. It is passed
// through as raw JSON rather than decoded and rebuilt.
//
// The lookup is the JS one turned inside out: there, each event asks for its
// reading; here, hebcal-go's events are matched to the sidecar's by the
// untranslated event description, except for the parsha, which is the one item
// on the day whose category is "parashat". Timed events are never passed here,
// matching the JS side, where getLeyningForHoliday() rejects anything with an
// eventTime.
func itemLeyning(hd hdate.HDate, flags event.HolidayFlags, desc string,
	leyning map[string][]readings.Item) json.RawMessage {
	if leyning == nil {
		return nil
	}
	onDate := leyning[model.IsoGreg(hd)]
	if len(onDate) == 0 {
		return nil
	}
	// @hebcal/rest-api tests the mask for equality, so an event merely
	// carrying PARSHA_HASHAVUA alongside other flags is not a parsha.
	if flags == event.PARSHA_HASHAVUA {
		return readings.FindParsha(onDate)
	}
	return readings.FindHoliday(onDate, desc)
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
// related events share one description. It is the fallback key for the memo
// catalog, looked up when the full description has no memo of its own.
//
// Rosh Chodesh keeps its whole description: the generic rule strips a
// trailing Roman numeral, which would turn "Rosh Chodesh Adar I" into "Rosh
// Chodesh Adar" — a different month in a leap year, with a memo of its own.
func eventBasename(ev event.CalEvent) string {
	if ev.GetFlags()&event.ROSH_CHODESH != 0 {
		return descOf(ev)
	}
	return normMonth(ev.Basename())
}

// itemLink builds the shortened, tracked hebcal.com URL for an event.
func itemLink(ev event.CalEvent, hd hdate.HDate, il bool) string {
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

// canonicalHolidayPrefix is what hebcal-go's event.URL() puts in front of a
// holiday's "<slug>-<suffix>". The short links here point at those same pages.
const canonicalHolidayPrefix = "https://www.hebcal.com/holidays/"

// holidayShortURL builds /h/<slug>-<year>?us=js&um=api.
//
// The slug and suffix are taken from the canonical URL rather than derived a
// second time, so the rules that do not fall out of the description -- Asara
// B'Tevet needs a full date because it can fall twice in one Gregorian year,
// Chanukah is filed under the year it began, Rosh Chodesh Tammuz is spelled
// "tamuz", each Adar of a leap year has its own page -- are stated once,
// upstream. An event with no page yields no link.
//
// The ?i=on on a canonical URL marks an Israel-only observance. The feed's
// own i=on means something else -- the schedule the reader asked for, which
// every link in the response carries -- so it is dropped here and re-added
// from il.
func holidayShortURL(he event.HolidayEvent, il bool) string {
	slug, ok := strings.CutPrefix(strings.TrimSuffix(event.URL(he), "?i=on"),
		canonicalHolidayPrefix)
	if !ok {
		return ""
	}
	q := "?us=js&um=api"
	if il {
		q = "?i=on&us=js&um=api"
	}
	return "https://hebcal.com/h/" + slug + q
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
	hour, _ := jsutil.ParseInt(hm[0])
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
