package model

import (
	"strings"

	"github.com/hebcal/locales"
)

// AliasLocale maps the short `lg` query-string values onto locale names
// understood by the locales package, in lower case ("he-x-nonikud"), which is
// how every catalog and comparison here spells them. The "h" suffix on "ah"
// and "sh" only asks for the Hebrew name to be appended to a subject, which the
// callers that want it read from the raw `lg`, so they resolve to the same
// locale as "a" and "s".
func AliasLocale(lg string) string {
	switch lg {
	case "h":
		return "he"
	case "a", "ah":
		return "ashkenazi"
	case "s", "sh", "":
		return "en"
	}
	return strings.ToLower(lg)
}

// IsHebrewLocale reports whether a locale -- resolved, or the raw `lg` "h" --
// renders in Hebrew script, right to left. Comparison is case-insensitive.
func IsHebrewLocale(locale string) bool {
	switch strings.ToLower(locale) {
	case "he", "he-x-nonikud", "h":
		return true
	}
	return false
}

// LocaleLanguage is the two-letter language a resolved locale is written in:
// the locale itself when it is two letters, the base language of the Hebrew
// and Romanian variants, and English for the transliterated locales
// ("ashkenazi", "sephardic") and anything else. An unknown two-letter locale
// is returned as given, lower-cased.
func LocaleLanguage(locale string) string {
	locale = strings.ToLower(locale)
	if len(locale) == 2 {
		return locale
	}
	switch {
	case IsHebrewLocale(locale):
		return "he"
	case locale == "ashkenazi_romanian":
		return "ro"
	}
	return "en"
}

// NamesFor returns the calendar vocabulary (month and weekday names) for a
// resolved locale, falling back to English. The lookup is case-insensitive,
// and he-x-nonikud shares he's names -- the no-nikud variant only affects
// event subjects.
func NamesFor(locale string) CalendarNames {
	if n, ok := namesByLocale[namesKey(locale)]; ok {
		return n
	}
	return namesByLocale["en"]
}

// HasCalendarNames reports whether NamesFor has names of its own for locale
// rather than falling back to English.
func HasCalendarNames(locale string) bool {
	_, ok := namesByLocale[namesKey(locale)]
	return ok
}

func namesKey(locale string) string {
	if IsHebrewLocale(locale) {
		return "he"
	}
	return strings.ToLower(locale)
}

// LocaleSupported reports whether `lg` names a locale this service can render.
// Comparison is case-insensitive. Note that "sephardic" is deliberately
// absent: the short form "s" is the only spelling accepted.
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

// LookupMemo looks up a MEMO catalog key, returning "" when there is no entry.
// A result equal to the key counts as none: the locales package echoes an
// unknown English key with ok=true.
func LookupMemo(key, locale string) string {
	if s, ok := locales.LookupTranslation(key, locale); ok && s != key {
		return s
	}
	return ""
}

// FixMonthSpelling reconciles the month name hebcal-go's event descriptions
// disagree on: hdate renders "Tammuz" with two m's, while this service spells
// the month "Tamuz" -- in "Rosh Chodesh Tamuz", "Molad Tamuz", "Mevarchim
// Chodesh Tamuz", "17th of Tamuz" and a PDF subtitle alike. The one exception
// is the 17th-of-Tammuz fast, whose hard-coded holiday name "Tzom Tammuz"
// keeps two m's. So normalize the month everywhere, then restore the fast.
// Hebrew text never contains the ASCII spelling, so this is a no-op on
// Hebrew-locale subjects.
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
