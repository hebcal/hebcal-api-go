package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/omer"
	"github.com/hebcal/hebcal-go/sedra"
	"github.com/hebcal/locales"
)

// enMonthNames are the transliterated month names used by @hebcal/hdate
// (JavaScript). Note "Tamuz" with a single m; the Go hdate package spells it
// "Tammuz", but this service matches the JS API output ("hm":"Tamuz").
var enMonthNames = []string{
	"", "Nisan", "Iyyar", "Sivan", "Tamuz", "Av", "Elul",
	"Tishrei", "Cheshvan", "Kislev", "Tevet", "Sh'vat", "Adar", "Adar II",
}

// monthNameEn returns the JS-compatible English month name.
func monthNameEn(m hdate.HMonth, year int) string {
	if m == hdate.Adar1 && hdate.IsLeapYear(year) {
		return "Adar I"
	}
	return enMonthNames[m]
}

func hdMonthNameEn(hd hdate.HDate) string {
	return monthNameEn(hd.Month(), hd.Year())
}

// hdateString formats like the JS HDate.toString(), e.g. "20 Tamuz 5786".
func hdateString(hd hdate.HDate) string {
	return fmt.Sprintf("%d %s %d", hd.Day(), hdMonthNameEn(hd), hd.Year())
}

// aliasLocale maps the short `lg` query-string values onto locale names
// understood by the locales package, mirroring the alias map in @hebcal/hdate.
func aliasLocale(lg string) string {
	switch lg {
	case "h":
		return "he"
	case "a":
		return "ashkenazi"
	case "s", "":
		return "en"
	}
	return lg
}

func isEnLocale(locale string) bool {
	switch strings.ToLower(locale) {
	case "", "en", "sephardic", "s":
		return true
	}
	return false
}

// gettext returns the translation for key, falling back to the key itself.
func gettext(key, locale string) string {
	str, _ := locales.LookupTranslation(key, locale)
	return str
}

// calEv is a calendar event on a specific Hebrew date. The render method
// receives the raw `lg` query-string value.
type calEv interface {
	desc() string
	render(lg string) string
	url() string
	chanukahDay() int
}

// renderEvent renders an event description, renaming "Chanukah: N Candles"
// events to "Chanukah Day N" style. Ported from converter.js renameChanukah().
func renderEvent(ev calEv, lg string) string {
	if day := ev.chanukahDay(); day > 0 {
		locale := aliasLocale(lg)
		if !isEnLocale(locale) {
			if str, ok := locales.LookupTranslation(fmt.Sprintf("Chanukah Day %d", day), locale); ok {
				return str
			}
		}
		return gettext("Chanukah", locale) + " " + gettext("day", locale) + " " + strconv.Itoa(day)
	}
	return ev.render(lg)
}

// ------------------------------------------------------------------ holiday

type holidayEv struct {
	ev event.HolidayEvent
}

func (h holidayEv) desc() string     { return h.ev.Desc }
func (h holidayEv) chanukahDay() int { return h.ev.ChanukahDay }

func (h holidayEv) render(lg string) string {
	locale := strings.ToLower(aliasLocale(lg))
	if h.ev.Flags&event.ROSH_CHODESH != 0 {
		// look up the month translation with the JS spelling ("Tamuz"),
		// which is what the locale catalogues use
		month := strings.ReplaceAll(strings.TrimPrefix(h.ev.Desc, "Rosh Chodesh "), "Tammuz", "Tamuz")
		return smartApostrophe(gettext("Rosh Chodesh", locale) + " " + gettext(month, locale))
	}
	if h.ev.Date.Month() == hdate.Tishrei && h.ev.Date.Day() == 1 {
		// Rosh Hashana: the JS API renders the year as a number in all locales
		return gettext("Rosh Hashana", locale) + " " + strconv.Itoa(h.ev.Date.Year())
	}
	return smartApostrophe(h.ev.Render(locale))
}

func (h holidayEv) url() string {
	gy, gm, gd := h.ev.Date.ProlepticGreg()
	if gy < 100 || gy > 2999 {
		return ""
	}
	var suffix string
	switch {
	case h.ev.Desc == "Asara B'Tevet":
		// occurs twice in some Gregorian years, so the URL uses YYYYMMDD
		suffix = fmt.Sprintf("%04d%02d%02d", gy, int(gm), gd)
	case strings.HasPrefix(h.ev.Desc, "Chanukah"):
		// Chanukah sometimes starts in December and ends in January
		year := gy
		if gm == time.January {
			year--
		}
		suffix = strconv.Itoa(year)
	default:
		suffix = strconv.Itoa(gy)
	}
	url := "https://www.hebcal.com/holidays/" + urlFriendly(h.ev.Basename()) + "-" + suffix
	if h.ev.Flags&event.IL_ONLY != 0 {
		url += "?i=on"
	}
	return url
}

// genericEv adapts any other hebcal-go event (e.g. Molad) unchanged.
type genericEv struct {
	ev event.CalEvent
}

func (g genericEv) desc() string     { return g.ev.Basename() }
func (g genericEv) chanukahDay() int { return 0 }
func (g genericEv) url() string      { return "" }

func (g genericEv) render(lg string) string {
	return smartApostrophe(g.ev.Render(strings.ToLower(aliasLocale(lg))))
}

// ------------------------------------------------------------------- parsha

type parshaEv struct {
	sat    hdate.HDate // the Saturday on which the parsha is read
	parsha sedra.Parsha
	il     bool
}

func (p parshaEv) desc() string     { return "Parashat " + strings.Join(p.parsha.Name, "-") }
func (p parshaEv) chanukahDay() int { return 0 }

func (p parshaEv) render(lg string) string {
	locale := strings.ToLower(aliasLocale(lg))
	prefix := gettext("Parashat", locale)
	return smartApostrophe(prefix + " " + p.parsha.Render(locale))
}

func (p parshaEv) url() string {
	gy, gm, gd := p.sat.ProlepticGreg()
	if gy < 100 || gy > 2999 {
		return ""
	}
	url := "https://www.hebcal.com/sedrot/" +
		urlFriendly(strings.Join(p.parsha.Name, "-")) +
		fmt.Sprintf("-%04d%02d%02d", gy, int(gm), gd)
	if p.il {
		url += "?i=on"
	}
	return url
}

// pseudoParshaEv represents "Parashat <holiday>" when the upcoming Saturday
// has a special holiday Torah reading instead of a regular parsha.
type pseudoParshaEv struct {
	h holidayEv
}

func (p pseudoParshaEv) desc() string     { return "Parashat " + p.h.ev.Basename() }
func (p pseudoParshaEv) chanukahDay() int { return 0 }
func (p pseudoParshaEv) url() string      { return p.h.url() }

func (p pseudoParshaEv) render(lg string) string {
	locale := strings.ToLower(aliasLocale(lg))
	return gettext("Parashat", locale) + " " + gettext(p.h.ev.Basename(), locale)
}

// --------------------------------------------------------------------- omer

type omerEv struct {
	ev omer.OmerEvent
}

func (o omerEv) desc() string     { return "Omer " + strconv.Itoa(o.ev.OmerDay) }
func (o omerEv) chanukahDay() int { return 0 }

func (o omerEv) render(lg string) string {
	return o.ev.Render(strings.ToLower(aliasLocale(lg)))
}

func (o omerEv) url() string {
	hy := o.ev.Date.Year()
	if hy < 5000 || hy > 6759 {
		return ""
	}
	return fmt.Sprintf("https://www.hebcal.com/omer/%d/%d", hy, o.ev.OmerDay)
}

// ---------------------------------------------------------------- getEvents

// getEvents returns the list of holidays and other calendar events occurring
// on the given Hebrew date. Ported from converter.js getEvents(), but leaning
// on hebcal.HebrewCalendar for holiday, Shabbat Mevarchim, and Molad events.
func getEvents(hd hdate.HDate, il bool) []calEv {
	// Matan Torah traditionally on 6 Sivan 2448
	if hd.Abs() < -479441 {
		return nil
	}
	// Look up this day's holiday/Shabbat Mevarchim/Molad events from the
	// memoized whole-year computation instead of recomputing the year per day.
	evs := holidayEventsForYear(hd.Year(), il)[hd.Abs()]
	events := make([]calEv, 0, 4)
	for _, ev := range evs {
		if hev, ok := ev.(event.HolidayEvent); ok {
			if hev.Desc == "Chanukah: 1 Candle" {
				continue
			}
			events = append(events, holidayEv{hev})
		} else {
			events = append(events, genericEv{ev})
		}
	}
	events = append(events, parshaEvents(hd, il)...)
	events = append(events, omerEvents(hd)...)
	return events
}

// holidaysOnDate returns the holiday events for a single Hebrew date, using the
// memoized per-year holiday index.
func holidaysOnDate(hd hdate.HDate, il bool) []event.HolidayEvent {
	return holidaysForYearByDate(hd.Year(), il)[hd.Abs()]
}

// hasHolidayReading reports whether the date has a special (non-parsha) full
// Torah reading. This approximates @hebcal/leyning getLeyningOnDate() with
// `fullkriyah && !parshaNum`: major holidays, chol hamoed, Rosh Chodesh, fast
// days, Chanukah, and Purim all have full kriyah readings.
func hasHolidayReading(hd hdate.HDate, il bool) bool {
	const readingFlags = event.CHAG | event.CHOL_HAMOED | event.ROSH_CHODESH |
		event.MINOR_FAST | event.MAJOR_FAST | event.CHANUKAH_CANDLES
	for _, hev := range holidaysOnDate(hd, il) {
		if hev.Desc == "Chanukah: 1 Candle" {
			continue // candle-lighting the previous evening; no Torah reading
		}
		if hev.Flags&event.YOM_KIPPUR_KATAN != 0 ||
			hev.Desc == "Ta'anit BeHaB" || hev.Desc == "Ta'anit Bechorot" {
			continue // fast-day flag but no special Torah reading in leyning
		}
		if hev.Flags&readingFlags != 0 {
			return true
		}
		if hev.Desc == "Purim" || hev.Desc == "Shushan Purim" {
			return true
		}
	}
	return false
}

// parshaEvents returns the upcoming Torah reading for the date.
// Ported from converter.js getParshaEvents().
func parshaEvents(hd hdate.HDate, il bool) []calEv {
	saturday := hd.OnOrAfter(time.Saturday)
	hy := saturday.Year()
	s := sedra.New(hy, il)
	parsha := s.Lookup(saturday)
	if !parsha.Chag {
		return []calEv{parshaEv{sat: saturday, parsha: parsha, il: il}}
	}
	if hasHolidayReading(hd, il) {
		return nil
	}
	mm := hd.Month()
	dd := hd.Day()
	if mm == hdate.Tishrei && dd > 2 && dd < 15 {
		st := simchatTorahDate(hy, il)
		p := sedra.Parsha{Name: []string{"Vezot Haberakhah"}}
		return []calEv{parshaEv{sat: st, parsha: p, il: il}}
	}
	var events []calEv
	for _, hev := range holidaysOnDate(saturday, il) {
		events = append(events, pseudoParshaEv{holidayEv{hev}})
	}
	return events
}

// omerEvents returns the Sefirat HaOmer count for the date, if within the
// Omer. Ported from converter.js makeOmer(); appended after the parsha so
// the events array matches the documented API output order.
func omerEvents(hd hdate.HDate) []calEv {
	mm := hd.Month()
	if mm == hdate.Nisan || mm == hdate.Iyyar || mm == hdate.Sivan {
		beginOmer := hdate.ToRD(hd.Year(), hdate.Nisan, 16)
		abs := hd.Abs()
		if abs >= beginOmer && abs < beginOmer+49 {
			return []calEv{omerEv{omer.NewOmerEvent(hd, int(abs-beginOmer)+1)}}
		}
	}
	return nil
}
