// Package fridge builds the printable year of candle-lighting times served at
// /shabbat/fridge.cgi: one row for every Shabbat and Yom Tov of a Hebrew or
// Gregorian year, with the Torah portion or the holiday it ushers in.
package fridge

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/service/shabbat"
	zmanimsvc "github.com/hebcal/hebcal-api-go/internal/service/zmanim"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// The years served run from YearsBefore before the current year to YearsAfter
// after it; anything else is 410 Gone. The page is for printing next year's
// times, not an archive, and crawlers would otherwise walk every year.
const (
	YearsBefore = 4
	YearsAfter  = 20
)

// Year is the year a page covers: a Hebrew year from Erev Rosh Hashana to
// Erev Rosh Hashana, or a Gregorian year from January to December.
type Year struct {
	// Hebrew is whether Number is a Hebrew year.
	Hebrew bool
	Number int
	// Start and End are the R.D. days of the first and last day listed.
	Start, End int64
	// Default is set when the request named no year and got the coming one.
	// Such a response describes a different year once that default moves.
	Default bool
}

// GregYears returns the Gregorian years of the first and last day.
func (y Year) GregYears() (int, int) {
	return model.GregFromRD(y.Start).Year, model.GregFromRD(y.End).Year
}

// ParseYear reads `year` and `yt` relative to today. With no year it is the
// Hebrew year in progress, or the next one once 15 Av has passed, whatever
// `yt` says. A year is Hebrew unless yt=G; "now", "x" and 0 mean the
// current one. A year that is not a number is a 400, one outside the window around
// today a 410.
func ParseYear(q url.Values, today model.GregDate) (Year, error) {
	todayHD := model.HDateFromRD(today.RD())
	thisHebYear := todayHD.Year()
	yearStr := q.Get("year")
	if yearStr == "" {
		hy := thisHebYear
		if todayHD.Abs() > hdate.New(hy, hdate.Av, 15).Abs() {
			hy++
		}
		y := hebrewYear(hy)
		y.Default = true
		return y, nil
	}
	isGreg := q.Get("yt") == "G"
	thisYear, calName := thisHebYear, "Hebrew"
	if isGreg {
		thisYear, calName = today.Year, "Gregorian"
	}
	year := thisYear
	if yearStr != "now" && yearStr != "x" {
		n, ok := jsutil.ParseInt(yearStr)
		if !ok {
			return Year{}, model.BadRequest("Sorry, invalid year %s", yearStr)
		}
		if n != 0 {
			year = n
		}
	}
	if year < thisYear-YearsBefore {
		return Year{}, gone("%s year %d too early", calName, year)
	} else if year > thisYear+YearsAfter {
		return Year{}, gone("%s year %d too late", calName, year)
	}
	if isGreg {
		return Year{
			Number: year,
			Start:  model.GregDate{Year: year, Month: time.January, Day: 1}.RD(),
			End:    model.GregDate{Year: year, Month: time.December, Day: 31}.RD(),
		}, nil
	}
	return hebrewYear(year), nil
}

func gone(format string, args ...interface{}) error {
	e := model.BadRequest(format, args...)
	e.Status = 410
	return e
}

// hebrewYear spans Erev Rosh Hashana of hy through Erev Rosh Hashana of the
// next year, so the page both opens and closes with the new year's candles.
func hebrewYear(hy int) Year {
	return Year{
		Hebrew: true,
		Number: hy,
		Start:  hdate.New(hy, hdate.Tishrei, 1).Abs() - 1,
		End:    hdate.New(hy+1, hdate.Tishrei, 1).Abs() - 1,
	}
}

// InWindow reports whether a year of the given calendar would be served
// today, for deciding which of the previous/next links to offer.
func InWindow(year int, hebrew bool, today model.GregDate) bool {
	thisYear := today.Year
	if hebrew {
		thisYear = model.HDateFromRD(today.RD()).Year()
	}
	return year >= thisYear-YearsBefore && year <= thisYear+YearsAfter
}

// Item is one candle-lighting: the evening of a Shabbat or Yom Tov.
type Item struct {
	Date model.GregDate
	// Reason is what the candles are lit for, in the page's language: the
	// Torah portion of a regular Shabbat, otherwise the holiday.
	Reason string
	// YomTov marks the eve of a day of Yom Tov, drawn bold.
	YomTov bool
	// Time is the candle-lighting time and Havdalah the time the Shabbat or
	// holiday ends, "" when it runs into another candle-lighting. Both are
	// clock times without an am/pm suffix.
	Time     string
	Havdalah string
}

// Calendar is everything a page or CSV export shows.
type Calendar struct {
	Year     Year
	Location *geodb.Location
	// IL is whether the Israel holiday schedule applies.
	IL bool
	// Lg is the request's language code and Locale the catalog it resolves to.
	Lg     string
	Locale string
	// Options are the resolved candle-lighting and Havdalah settings.
	Options hebcal.Options
	Items   []Item
}

// Lang returns the language the page's dates are written in, a BCP 47 code:
// that of the locale where there are month names for it, English otherwise
// (the transliterated locales among them).
func (c *Calendar) Lang() string {
	return dateLang(c.Locale)
}

func dateLang(locale string) string {
	switch locale {
	case "he", "he-x-nonikud":
		return "he"
	case "ashkenazi_romanian":
		return "ro"
	case "de", "es", "fi", "fr", "hu", "nl", "pl", "pt", "ru", "ro", "uk":
		return locale
	}
	return "en"
}

// New generates the year's candle-lighting times for a location. lg must be
// a supported language code. The query supplies the candle-lighting and
// Havdalah preferences (b, m, M, td), ue and i.
func New(loc *geodb.Location, year Year, lg string, q url.Values) (*Calendar, error) {
	il := loc.IsIsrael() || jsutil.IsOn(q.Get("i"))
	// One day past the end, so the last evening's Shabbat or holiday is there
	// to be named.
	opts := shabbat.Options(loc, il, model.GregFromRD(year.Start), model.GregFromRD(year.End+1), q)
	opts.ShabbatMevarchim = false
	opts.Molad = false
	events, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		return nil, model.BadRequest("%s", err.Error())
	}
	c := &Calendar{
		Year:     year,
		Location: loc,
		IL:       il,
		Lg:       lg,
		Locale:   strings.ToLower(model.AliasLocale(lg)),
		Options:  opts,
	}
	c.Items = makeItems(events, year.End, c.Locale, loc.CC, il, event.ClockAuto)
	if len(c.Items) == 0 {
		return nil, model.BadRequest("No candle-lighting times for this location")
	}
	return c, nil
}

// makeItems picks the candle-lightings through the day `end` out of a
// calendar and names each after the day that follows it. A Havdalah belongs
// to the candle-lighting before it. Chanukah candles are not listed.
func makeItems(events []event.CalEvent, end int64, locale, cc string, il bool, clock event.ClockFormat) []Item {
	byDay := make(map[int64][]event.CalEvent)
	for _, ev := range events {
		if _, timed := ev.(event.Timed); !timed {
			rd := ev.Date().Abs()
			byDay[rd] = append(byDay[rd], ev)
		}
	}
	if cc == "" && il {
		cc = "IL"
	}
	var items []Item
	for _, ev := range events {
		timed, ok := ev.(event.Timed)
		if !ok {
			continue
		}
		rd := ev.Date().Abs()
		if rd > end {
			break
		}
		kind, _ := hebcal.TimedKindOf(ev)
		if kind != hebcal.CandleLighting && kind != hebcal.Havdalah {
			continue
		}
		t := zmanimsvc.RoundTime(timed.Time())
		timeStr := event.ReformatTimeStr(t.Format("15:04"), "", cc, clock)
		if kind == hebcal.Havdalah {
			if len(items) > 0 {
				items[len(items)-1].Havdalah = timeStr
			}
			continue
		}
		friday := ev.Date().Weekday() == time.Friday
		reason, yomTov := reasonFor(byDay[rd+1], friday, locale)
		if reason == "" {
			continue
		}
		items = append(items, Item{
			Date:   model.GregFromRD(rd),
			Reason: reason,
			YomTov: yomTov,
			Time:   timeStr,
		})
	}
	return items
}

// reasonFor names the day whose events are given, for the candles lit the
// evening before: the Torah portion when that evening is a Friday and one is
// read, otherwise the holiday -- a day of Yom Tov, or the festival whose
// intermediate Shabbat it is. The holiday is named without its day ("Pesach"
// for "Pesach VII"). It returns "" when the day is neither.
func reasonFor(day []event.CalEvent, friday bool, locale string) (reason string, yomTov bool) {
	if friday {
		for _, ev := range day {
			if ev.Flags().Has(event.ParshaHashavua) {
				// "Parashat Noach" without its first word
				name := ev.Render(locale)
				if sp := strings.IndexByte(name, ' '); sp != -1 {
					name = name[sp+1:]
				}
				return name, false
			}
		}
	}
	for _, ev := range day {
		if ev.Flags().Has(event.Chag) {
			return model.Gettext(ev.Basename(), locale), true
		}
	}
	if friday {
		for _, ev := range day {
			if ev.Flags().Has(event.CholHamoed) {
				return model.Gettext(ev.Basename(), locale), false
			}
		}
	}
	return "", false
}

// HavdalahNote describes how the Havdalah times were reckoned, in the two
// forms the page's footnote takes: a number of minutes after sunset, or the
// sun's depression below the horizon in degrees. None is set when the
// request asked for no Havdalah at all.
type HavdalahNote struct {
	None    bool
	Minutes int
	Degrees string
}

// Havdalah returns the page's Havdalah footnote.
func (c *Calendar) Havdalah() HavdalahNote {
	o := &c.Options
	switch {
	case o.SuppressHavdalah:
		return HavdalahNote{None: true}
	case o.HavdalahMins > 0:
		return HavdalahNote{Minutes: o.HavdalahMins}
	}
	return HavdalahNote{Degrees: formatFloat(o.HavdalahDeg)}
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// BaseURL returns the page's own URL without a year: its location and its
// resolved candle-lighting, Havdalah and language settings, so the links to
// other years carry the same ones however the request spelled them.
func (c *Calendar) BaseURL(q url.Values) string {
	loc := c.Location
	var b strings.Builder
	b.WriteString("/shabbat/fridge.cgi?")
	sep := ""
	add := func(k, v string) {
		b.WriteString(sep + k + "=" + url.QueryEscape(v))
		sep = "&"
	}
	switch {
	case loc.Geo == "zip" && loc.Zip != "":
		add("zip", loc.Zip)
	case loc.Geo == "pos" || loc.GeonameID == 0:
		add("geo", "pos")
		add("latitude", formatFloat(loc.Latitude))
		add("longitude", formatFloat(loc.Longitude))
		if loc.Elevation > 0 {
			add("elev", strconv.Itoa(loc.Elevation))
		}
		add("tzid", loc.TimeZoneID)
		if name := strings.TrimSpace(q.Get("city-typeahead")); name != "" {
			add("city-typeahead", name)
		}
	default:
		add("geonameid", strconv.Itoa(loc.GeonameID))
	}
	if c.Options.UseElevation {
		add("ue", "on")
	} else {
		add("ue", "off")
	}
	if c.IL && !loc.IsIsrael() {
		add("i", "on")
	}
	add("b", strconv.Itoa(c.Options.CandleLightingMins))
	switch h := c.Havdalah(); {
	case h.None:
		add("M", "off")
		add("m", "0")
	case h.Minutes > 0:
		add("M", "off")
		add("m", strconv.Itoa(h.Minutes))
	case h.Degrees == "8.5":
		add("M", "on")
	default:
		add("td", h.Degrees)
	}
	lg := c.Lg
	if lg == "" {
		lg = "s"
	}
	add("lg", lg)
	return b.String()
}

// utf16Len counts s in UTF-16 code units, the measure the narrow-font
// thresholds were tuned in.
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}
