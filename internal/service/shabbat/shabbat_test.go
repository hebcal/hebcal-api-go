package shabbat

import (
	"testing"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/molad"
)

// TestMoladInstant checks the conjunction timestamp against values captured
// from hebcal-web, including a millisecond ending in a zero: JavaScript's
// Temporal.Instant.toJSON() trims it, so ".170" prints as ".17".
func TestMoladInstant(t *testing.T) {
	tests := []struct {
		year  int
		month hdate.HMonth
		want  string
	}{
		{5787, hdate.Kislev, "2026-11-09T20:06:13.504Z"},
		{5784, hdate.Adar1, "2024-02-09T19:08:20.17Z"},
		{5784, hdate.Sivan, "2024-06-06T22:04:33.504Z"},
	}
	for _, tc := range tests {
		if got := moladInstant(molad.New(tc.year, tc.month)); got != tc.want {
			t.Errorf("molad %d %v: instant = %q, want %q", tc.year, tc.month, got, tc.want)
		}
	}
}

// TestNormMonth pins the "Tammuz" spellings: @hebcal/core writes the month
// as "Tamuz" but keeps "Tzom Tammuz" for the 17th-of-Tammuz fast, and that
// description is what title_orig, the MEMO key, the event URL and the
// /leyning lookup are all built from.
func TestNormMonth(t *testing.T) {
	cases := map[string]string{
		"Rosh Chodesh Tammuz":              "Rosh Chodesh Tamuz",
		"Shabbat Mevarchim Chodesh Tammuz": "Shabbat Mevarchim Chodesh Tamuz",
		"Tzom Tammuz":                      "Tzom Tammuz",
		"Rosh Chodesh Av":                  "Rosh Chodesh Av",
	}
	for in, want := range cases {
		if got := normMonth(in); got != want {
			t.Errorf("normMonth(%q) = %q, want %q", in, got, want)
		}
	}
}
