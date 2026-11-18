package model

import "testing"

func TestIsHebrewLocale(t *testing.T) {
	for locale, want := range map[string]bool{
		"he": true, "h": true, "he-x-NoNikud": true, "he-x-nonikud": true, "HE": true,
		"": false, "en": false, "ashkenazi": false, "ah": false, "sh": false, "fr": false,
	} {
		if got := IsHebrewLocale(locale); got != want {
			t.Errorf("IsHebrewLocale(%q) = %v, want %v", locale, got, want)
		}
	}
}

// LocaleLanguage names the PRODID language of the holiday, static and zmanim
// feeds. The zmanim feed passes the raw lg through AliasLocale first, so an
// unknown two-letter lg is named as given while its titles fall back to
// English.
func TestLocaleLanguage(t *testing.T) {
	for locale, want := range map[string]string{
		"en": "en", "he": "he", "fr": "fr", "he-x-NoNikud": "he", "ashkenazi": "en",
		"ashkenazi_romanian": "ro", "s": "en", "sephardic": "en", "xx": "xx",
		"ashkenazi_litvish": "en",
	} {
		if got := LocaleLanguage(locale); got != want {
			t.Errorf("LocaleLanguage(%q) = %q, want %q", locale, got, want)
		}
	}
	for lg, want := range map[string]string{
		"": "en", "h": "he", "a": "en", "ah": "en", "s": "en", "sh": "en",
		"xx": "xx", "he-x-NoNikud": "he", "ashkenazi_romanian": "ro",
	} {
		if got := LocaleLanguage(AliasLocale(lg)); got != want {
			t.Errorf("LocaleLanguage(AliasLocale(%q)) = %q, want %q", lg, got, want)
		}
	}
}

func TestNamesFor(t *testing.T) {
	if got := NamesFor("he-x-NoNikud").Months; got != NamesFor("he").Months {
		t.Errorf("he-x-NoNikud months = %v, want he's", got)
	}
	if got := NamesFor("ashkenazi").Months[0]; got != "January" {
		t.Errorf("ashkenazi falls back to English, got %q", got)
	}
	if HasCalendarNames("ashkenazi") || !HasCalendarNames("ro") || !HasCalendarNames("he-x-nonikud") {
		t.Error("HasCalendarNames: ashkenazi has none, ro and he-x-nonikud do")
	}
}
