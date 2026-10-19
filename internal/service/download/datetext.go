package download

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hebcal/gematriya"
	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
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

// HebrewDateBrief renders a HEBREW_DATE alternate date without its year. hebcal-go's
// hebrewDateEvent only renders the full form (with year), so the brief is that
// form with the trailing year trimmed. Rosh Hashana (1 Tishrei) keeps its year.
//
// The apostrophe is smartened here as it is on an event subject, since hebcal-go
// does not: without this a day line reads "1st of Sh'vat" rather than "1st of
// Sh’vat". The ordinal for a locale that is neither English nor Spanish comes
// out as "12 Tewet" where a current reference draws "12. Tewet"; that
// difference belongs in hebcal-go rather than in a second date formatter here.
func HebrewDateBrief(hd hdate.HDate, locale string) string {
	full := jsutil.SmartApostrophe(model.FixMonthSpelling(event.NewHebrewDateEvent(hd).Render(locale)))
	if hd.Month() == hdate.Tishrei && hd.Day() == 1 {
		return full
	}
	switch strings.ToLower(locale) {
	case "he", "he-x-nonikud":
		return strings.TrimSuffix(full, " "+gematriya.Gematriya(hd.Year()))
	case "", "en", "sephardic", "ashkenazi",
		"ashkenazi_litvish", "ashkenazi_poylish", "ashkenazi_standard":
		return strings.TrimSuffix(full, ", "+strconv.Itoa(hd.Year()))
	default:
		return strings.TrimSuffix(full, " "+strconv.Itoa(hd.Year()))
	}
}
