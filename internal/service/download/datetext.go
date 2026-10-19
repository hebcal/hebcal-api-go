package download

import (
	"fmt"
	"strings"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// nonEnglishDateLocales are the resolved locales that format a short date as
// "D MMM" rather than the English "MMM D". Anything not listed falls through to
// the English template.
var nonEnglishDateLocales = map[string]bool{
	"de": true, "es": true, "fi": true, "fr": true, "he": true,
	"he-x-nonikud": true, "hu": true, "nl": true, "pl": true, "pt": true,
	"ru": true, "ro": true, "uk": true,
}

// GregorianDateText renders the Gregorian date shown on a Hebrew-month calendar's
// day-number line: "Jun 12" in English, "12 Jun" (localized month) elsewhere.
// No year.
func GregorianDateText(hd hdate.HDate, locale string) string {
	g := hd.Gregorian()
	mon := model.NamesFor(locale).MonthsShort[int(g.Month())-1]
	if nonEnglishDateLocales[strings.ToLower(locale)] {
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
