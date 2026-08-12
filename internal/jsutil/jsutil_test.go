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
		"Tamuz":   "tamuz",
		"Sh'vat":  "shvat",
		"Adar I":  "adar-i",
		"Adar II": "adar-ii",
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
