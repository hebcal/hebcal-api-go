package model

import "testing"

// These four came across with the PDF calendars, which is what the month and
// weekday tables and the Tamuz spelling are for; they live here because that is
// where the locale vocabulary now lives.

// AliasLocale mirrors lgToLocale in hebcal-web's src/lang.js.
func TestAliasLocale(t *testing.T) {
	tests := []struct{ lg, want string }{
		{"h", "he"},
		{"a", "ashkenazi"},
		{"ah", "ashkenazi"},
		{"s", "en"},
		{"sh", "en"},
		{"", "en"},
		{"de", "de"},
		{"pt", "pt"},
	}
	for _, tt := range tests {
		if got := AliasLocale(tt.lg); got != tt.want {
			t.Errorf("AliasLocale(%q) = %q, want %q", tt.lg, got, tt.want)
		}
	}
}

func TestFixMonthSpelling(t *testing.T) {
	cases := map[string]string{
		"Rosh Chodesh Tammuz":      "Rosh Chodesh Tamuz",
		"Erev Rosh Chodesh Tammuz": "Erev Rosh Chodesh Tamuz",
		"Molad Tammuz":             "Molad Tamuz",
		"Tzom Tammuz":              "Tzom Tammuz", // the fast keeps two m's
		"Rosh Chodesh Sh’vat":      "Rosh Chodesh Sh’vat",
	}
	for in, want := range cases {
		if got := FixMonthSpelling(in); got != want {
			t.Errorf("FixMonthSpelling(%q) = %q, want %q", in, got, want)
		}
	}
}

// Every locale hebcal-web can render needs a full set of names, or a calendar
// in that language would fall back to English mid-page.
func TestLocaleTablesAreComplete(t *testing.T) {
	for _, lc := range []string{"en", "de", "es", "fi", "fr", "he", "hu", "nl", "pl", "pt", "ru", "ro", "uk"} {
		n, ok := namesByLocale[lc]
		if !ok {
			t.Errorf("no names for locale %q", lc)
			continue
		}
		for i, m := range n.Months {
			if m == "" {
				t.Errorf("%s: month %d is empty", lc, i+1)
			}
		}
		for i, m := range n.MonthsShort {
			if m == "" {
				t.Errorf("%s: short month %d is empty", lc, i+1)
			}
		}
		for i, d := range n.Weekdays {
			if d == "" {
				t.Errorf("%s: weekday %d is empty", lc, i)
			}
		}
	}
}

// An unknown locale falls back to English rather than rendering blanks.
func TestUnknownLocaleFallsBackToEnglish(t *testing.T) {
	if got := NamesFor("klingon").Months[0]; got != "January" {
		t.Errorf("NamesFor(unknown).Months[0] = %q, want January", got)
	}
	// The transliterated locales are Latin script and deliberately absent from
	// the table, so they take the English names.
	if got := NamesFor("ashkenazi").Weekdays[6]; got != "Saturday" {
		t.Errorf("ashkenazi weekday = %q, want Saturday", got)
	}
}

func TestHebrewLocaleHasHebrewNames(t *testing.T) {
	he := NamesFor("he")
	if he.Months[0] != "ינואר" {
		t.Errorf("Hebrew January = %q, want ינואר", he.Months[0])
	}
	if he.Weekdays[0] != "ראשון" {
		t.Errorf("Hebrew Sunday = %q, want ראשון", he.Weekdays[0])
	}
}
