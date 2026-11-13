package model

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/omer"
	"github.com/hebcal/locales"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// CalEv is a calendar event on a specific Hebrew date, as /converter reports
// it. Render receives the raw `lg` query-string value. Desc and URL answer as
// the hebcal-go event behind the adapter does (event.CalEvent's Desc and URL);
// only the pseudo-parsha of a holiday week, which hebcal-go has no event for,
// answers for itself.
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
// events to "Chanukah Day N" style.
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

func (h HolidayEv) Desc() string     { return h.Ev.Desc() }
func (h HolidayEv) URL() string      { return h.Ev.URL() }
func (h HolidayEv) ChanukahDay() int { return h.Ev.ChanukahDay }

func (h HolidayEv) Render(lg string) string {
	locale := strings.ToLower(AliasLocale(lg))
	if h.Ev.Flags().Has(event.RoshChodesh) {
		// look up the month translation with the "Tamuz" spelling, which is
		// what the locale catalogues use
		month := strings.ReplaceAll(strings.TrimPrefix(h.Ev.Desc(), "Rosh Chodesh "), "Tammuz", "Tamuz")
		return jsutil.SmartApostrophe(Gettext("Rosh Chodesh", locale) + " " + Gettext(month, locale))
	}
	return h.Ev.Render(locale)
}

// GenericEv adapts any other hebcal-go event (Molad, Shabbat Mevarchim)
// unchanged. Neither has a page, so URL is empty for every event this wraps
// today.
type GenericEv struct {
	Ev event.CalEvent
}

func (g GenericEv) Desc() string     { return g.Ev.Desc() }
func (g GenericEv) URL() string      { return g.Ev.URL() }
func (g GenericEv) ChanukahDay() int { return 0 }

func (g GenericEv) Render(lg string) string {
	return jsutil.SmartApostrophe(g.Ev.Render(strings.ToLower(AliasLocale(lg))))
}

// ------------------------------------------------------------------- parsha

// ParshaEv adapts the weekly Torah portion read on a given Saturday.
type ParshaEv struct {
	Ev event.ParshaEvent
}

func (p ParshaEv) Desc() string     { return p.Ev.Desc() }
func (p ParshaEv) URL() string      { return p.Ev.URL() }
func (p ParshaEv) ChanukahDay() int { return 0 }

func (p ParshaEv) Render(lg string) string {
	locale := strings.ToLower(AliasLocale(lg))
	prefix := Gettext("Parashat", locale)
	return jsutil.SmartApostrophe(prefix + " " + p.Ev.Parsha.Render(locale))
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

func (o OmerEv) Desc() string     { return o.Ev.Desc() }
func (o OmerEv) URL() string      { return o.Ev.URL() }
func (o OmerEv) ChanukahDay() int { return 0 }

func (o OmerEv) Render(lg string) string {
	return o.Ev.Render(strings.ToLower(AliasLocale(lg)))
}
