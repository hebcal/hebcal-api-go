package model

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/omer"
	"github.com/hebcal/hebcal-go/sedra"
	"github.com/hebcal/locales"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// CalEv is a calendar event on a specific Hebrew date. Render receives the raw
// `lg` query-string value; the other methods answer in the untranslated form
// the API uses for lookups and links.
type CalEv interface {
	// Desc is the canonical, untranslated description.
	Desc() string
	// Render returns the localized title for the given `lg` value.
	Render(lg string) string
	// URL is the event's page on hebcal.com, or "" when it has none.
	URL() string
	// ChanukahDay is the 1-based day of Chanukah, or 0 for other events.
	ChanukahDay() int
}

// RenderEvent renders an event description, renaming "Chanukah: N Candles"
// events to "Chanukah Day N" style. Ported from converter.js renameChanukah().
func RenderEvent(ev CalEv, lg string) string {
	if day := ev.ChanukahDay(); day > 0 {
		locale := AliasLocale(lg)
		if !IsEnLocale(locale) {
			if str, ok := locales.LookupTranslation(fmt.Sprintf("Chanukah Day %d", day), locale); ok {
				return str
			}
		}
		return Gettext("Chanukah", locale) + " " + Gettext("day", locale) + " " + strconv.Itoa(day)
	}
	return ev.Render(lg)
}

// ------------------------------------------------------------------ holiday

// HolidayEv adapts a hebcal-go HolidayEvent.
type HolidayEv struct {
	Ev event.HolidayEvent
}

func (h HolidayEv) Desc() string     { return h.Ev.Desc }
func (h HolidayEv) ChanukahDay() int { return h.Ev.ChanukahDay }

func (h HolidayEv) Render(lg string) string {
	locale := strings.ToLower(AliasLocale(lg))
	if h.Ev.Flags&event.ROSH_CHODESH != 0 {
		// look up the month translation with the JS spelling ("Tamuz"),
		// which is what the locale catalogues use
		month := strings.ReplaceAll(strings.TrimPrefix(h.Ev.Desc, "Rosh Chodesh "), "Tammuz", "Tamuz")
		return jsutil.SmartApostrophe(Gettext("Rosh Chodesh", locale) + " " + Gettext(month, locale))
	}
	if h.Ev.Date.Month() == hdate.Tishrei && h.Ev.Date.Day() == 1 {
		// Rosh Hashana: the JS API renders the year as a number in all locales
		return Gettext("Rosh Hashana", locale) + " " + strconv.Itoa(h.Ev.Date.Year())
	}
	return jsutil.SmartApostrophe(h.Ev.Render(locale))
}

// URL returns the holiday's page on hebcal.com. hebcal-go builds it, which
// gets the cases this used to miss: the site files Rosh Chodesh Tammuz under
// "tamuz", gives each Adar of a leap year its own page, and has no page at all
// for Yom Kippur Katan.
func (h HolidayEv) URL() string {
	return event.URL(h.Ev)
}

// GenericEv adapts any other hebcal-go event (e.g. Molad) unchanged.
type GenericEv struct {
	Ev event.CalEvent
}

func (g GenericEv) Desc() string     { return g.Ev.Basename() }
func (g GenericEv) ChanukahDay() int { return 0 }

// URL is empty for every event this wraps today -- Molad and Shabbat
// Mevarchim have no page -- but asking hebcal-go keeps that answer tied to
// what the website actually publishes.
func (g GenericEv) URL() string { return event.URL(g.Ev) }

func (g GenericEv) Render(lg string) string {
	return jsutil.SmartApostrophe(g.Ev.Render(strings.ToLower(AliasLocale(lg))))
}

// ------------------------------------------------------------------- parsha

// ParshaEv is the weekly Torah portion read on a given Saturday.
type ParshaEv struct {
	Sat    hdate.HDate // the Saturday on which the parsha is read
	Parsha sedra.Parsha
	IL     bool
}

func (p ParshaEv) Desc() string     { return "Parashat " + strings.Join(p.Parsha.Name, "-") }
func (p ParshaEv) ChanukahDay() int { return 0 }

func (p ParshaEv) Render(lg string) string {
	locale := strings.ToLower(AliasLocale(lg))
	prefix := Gettext("Parashat", locale)
	return jsutil.SmartApostrophe(prefix + " " + p.Parsha.Render(locale))
}

func (p ParshaEv) URL() string {
	return event.URL(event.NewParshaEvent(p.Sat, p.Parsha, p.IL))
}

// PseudoParshaEv represents "Parashat <holiday>" when the upcoming Saturday
// has a special holiday Torah reading instead of a regular parsha.
type PseudoParshaEv struct {
	H HolidayEv
}

func (p PseudoParshaEv) Desc() string     { return "Parashat " + p.H.Ev.Basename() }
func (p PseudoParshaEv) ChanukahDay() int { return 0 }
func (p PseudoParshaEv) URL() string      { return p.H.URL() }

func (p PseudoParshaEv) Render(lg string) string {
	locale := strings.ToLower(AliasLocale(lg))
	return Gettext("Parashat", locale) + " " + Gettext(p.H.Ev.Basename(), locale)
}

// --------------------------------------------------------------------- omer

// OmerEv is the Sefirat HaOmer count for a date.
type OmerEv struct {
	Ev omer.OmerEvent
}

func (o OmerEv) Desc() string     { return "Omer " + strconv.Itoa(o.Ev.OmerDay) }
func (o OmerEv) ChanukahDay() int { return 0 }

func (o OmerEv) Render(lg string) string {
	return o.Ev.Render(strings.ToLower(AliasLocale(lg)))
}

func (o OmerEv) URL() string {
	return event.URL(o.Ev)
}
