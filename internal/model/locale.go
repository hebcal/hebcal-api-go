package model

import (
	"strings"

	"github.com/hebcal/locales"
)

// AliasLocale maps the short `lg` query-string values onto locale names
// understood by the locales package, mirroring the lgToLocale map in
// hebcal-web src/lang.js. The "h" suffix on "ah" and "sh" only asks for the
// Hebrew name to be appended to email subject lines, which no route here
// renders, so they resolve to the same locale as "a" and "s".
func AliasLocale(lg string) string {
	switch lg {
	case "h":
		return "he"
	case "a", "ah":
		return "ashkenazi"
	case "s", "sh", "":
		return "en"
	}
	return lg
}

// LocaleSupported reports whether `lg` names a locale this service can
// render, matching the set @hebcal/core's Locale.useLocale() accepts once
// hebcal-web has applied lgToLocale. Comparison is case-insensitive, as it is
// in both locale registries. Note that "sephardic" is deliberately absent:
// the short form "s" is the only spelling hebcal-web takes.
func LocaleSupported(lg string) bool {
	switch lg {
	case "", "s", "sh", "a", "ah", "h":
		return true
	}
	want := strings.ToLower(lg)
	for _, name := range locales.AllLocales {
		if strings.ToLower(name) == want {
			return true
		}
	}
	return false
}

// IsEnLocale reports whether a resolved locale renders in English.
func IsEnLocale(locale string) bool {
	switch strings.ToLower(locale) {
	case "", "en", "sephardic", "s":
		return true
	}
	return false
}

// Gettext returns the translation for key, falling back to the key itself.
func Gettext(key, locale string) string {
	str, _ := locales.LookupTranslation(key, locale)
	return str
}

// FixMonthSpelling reconciles the month name that differs between hebcal-go's
// event descriptions and @hebcal/core's: hdate renders "Tammuz" with two m's,
// while @hebcal/core (and the website) render the month "Tamuz" -- in "Rosh
// Chodesh Tamuz", "Molad Tamuz", "Mevarchim Chodesh Tamuz", "17th of Tamuz" and
// a PDF subtitle alike. The one exception is the 17th-of-Tammuz fast, whose
// hard-coded holiday name "Tzom Tammuz" keeps two m's in both libraries. So
// normalize the month everywhere, then restore the fast. Hebrew text never
// contains the ASCII spelling, so this is a no-op on Hebrew-locale subjects.
//
// (internal/service/shabbat has its own normMonth, which leaves a whole string
// alone once it contains "Tzom Tammuz" rather than restoring the fast
// afterwards. The two agree on every string either service renders; the /shabbat
// one is kept as it is because its result feeds title_orig, the MEMO catalog key
// and the /leyning lookup, none of which this one touches.)
func FixMonthSpelling(s string) string {
	if !strings.Contains(s, "Tammuz") {
		return s
	}
	s = strings.ReplaceAll(s, "Tammuz", "Tamuz")
	return strings.ReplaceAll(s, "Tzom Tamuz", "Tzom Tammuz")
}
