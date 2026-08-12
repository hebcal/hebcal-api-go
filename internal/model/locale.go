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
