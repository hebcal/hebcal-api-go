package download

import (
	"fmt"
	"strings"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// GregorianDateText renders the Gregorian date shown on a Hebrew-month calendar's
// day-number line: "Jun 12" in English, "12 Jun" (localized month) in a
// language with month names of its own (model.HasCalendarNames). A locale
// without them -- the transliterated ones -- takes the English form. No year.
func GregorianDateText(hd hdate.HDate, locale string) string {
	g := model.GregorianOf(hd)
	mon := model.NamesFor(locale).MonthsShort[int(g.Month())-1]
	if model.HasCalendarNames(locale) && !strings.EqualFold(locale, "en") {
		return fmt.Sprintf("%d %s", g.Day(), mon)
	}
	return fmt.Sprintf("%s %d", mon, g.Day())
}

// HebrewDateBrief renders a Hebrew date without its year, as the alternate
// date drawn beside a Gregorian one: "1st of Sh’vat", "12. Tewet", "י״ב טֵבֵת".
// 1 Tishrei keeps its year.
func HebrewDateBrief(hd hdate.HDate, locale string) string {
	return event.RenderBrief(event.NewHebrewDateEvent(hd), locale)
}
