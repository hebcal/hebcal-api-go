package jsutil

import (
	"math"
	"testing"
	"time"
)

func TestJsParseInt(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"2026", 2026, true},
		{" 12", 12, true},
		{"+5", 5, true},
		{"-5", -5, true},
		{"5.9", 5, true},        // ParseInt stops at the decimal point
		{"2026abc", 2026, true}, // trailing garbage ignored, like JS ParseInt
		{"", 0, false},
		{"abc", 0, false},
		{"abc123", 0, false},
		{"-", 0, false},
		{"- 5", 0, false},
		{"undefined", 0, false},
		// overflow saturates so year-range checks answer like the JS API
		{"99999999999999999999", math.MaxInt, true},
		{"-99999999999999999999", math.MinInt, true},
	}
	for _, c := range cases {
		got, ok := ParseInt(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseInt(%q) = %d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"7.083", 7.083, true},
		{"8.5deg", 8.5, true}, // trailing garbage ignored, like Number.parseFloat
		{"-122.143", -122.143, true},
		{" 40.7", 40.7, true},
		{"", 0, false},
		{"east", 0, false},
	}
	for _, c := range cases {
		got, err := ParseFloat(c.in)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("ParseFloat(%q) = %v,%v want %v, ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestIsoDateString(t *testing.T) {
	cases := []struct {
		y    int
		m    time.Month
		d    int
		want string
	}{
		{2026, time.July, 5, "2026-07-05"},
		{75, time.January, 2, "0075-01-02"},
		{-3760, time.September, 7, "-003760-09-07"},
		{28240, time.July, 1, "+028240-07-01"},
	}
	for _, c := range cases {
		if got := IsoDateString(c.y, c.m, c.d); got != c.want {
			t.Errorf("IsoDateString(%d,%d,%d) = %q, want %q", c.y, c.m, c.d, got, c.want)
		}
	}
}

func TestMakeAnchor(t *testing.T) {
	cases := map[string]string{
		"Tamuz":             "tamuz",
		"Sh'vat":            "shvat",
		"Adar I":            "adar-i",
		"Adar II":           "adar-ii",
		"Rosh Chodesh Elul": "rosh-chodesh-elul",
		"Yom HaAtzma'ut":    "yom-haatzmaut",
		// Only the straight apostrophe is deleted. The character class is
		// JavaScript's `\w` without the `u` flag, so the typographic one is not a
		// word character and becomes a hyphen like any other punctuation
		// (measured against @hebcal/rest-api: "Ta’anit Bechorot" gives
		// "ta-anit-bechorot").
		"Ta’anit Bechorot": "ta-anit-bechorot",
		// A PDF campaign slug (campaignFromTitle): punctuation collapses to
		// single hyphens and the ends are trimmed, rather than surviving into the
		// uc= campaign to be percent-encoded.
		"Washington, D.C": "washington-d-c",
		"St. Louis":       "st-louis",
		"GB-London":       "gb-london",
		// The degrees/minutes name a legacy /v2/ ladeg location produces.
		// Underscore is a word character and survives.
		"40°42′N 74°0′W America/New_York 2026": "40-42-n-74-0-w-america-new_york-2026",
	}
	for in, want := range cases {
		if got := MakeAnchor(in); got != want {
			t.Errorf("MakeAnchor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSmartApostrophe(t *testing.T) {
	if got := SmartApostrophe("Sh'vat (CH''M)"); got != "Sh’vat (CH’’M)" {
		t.Errorf("got %q", got)
	}
}
