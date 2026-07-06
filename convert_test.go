package main

import (
	"math"
	"testing"
	"time"

	"github.com/hebcal/hdate"
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
		{"5.9", 5, true},        // parseInt stops at the decimal point
		{"2026abc", 2026, true}, // trailing garbage ignored, like JS parseInt
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
		got, ok := parseInt(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("parseInt(%q) = %d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
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
		if got := isoDateString(c.y, c.m, c.d); got != c.want {
			t.Errorf("isoDateString(%d,%d,%d) = %q, want %q", c.y, c.m, c.d, got, c.want)
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
		if got := makeAnchor(in); got != want {
			t.Errorf("makeAnchor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSmartApostrophe(t *testing.T) {
	if got := smartApostrophe("Sh'vat (CH''M)"); got != "Sh’vat (CH’’M)" {
		t.Errorf("got %q", got)
	}
}

func TestMonthNameEn(t *testing.T) {
	if got := monthNameEn(hdate.Tamuz, 5786); got != "Tamuz" {
		t.Errorf("Tamuz = %q", got)
	}
	if got := monthNameEn(hdate.Adar1, 5784); got != "Adar I" { // leap year
		t.Errorf("Adar1 leap = %q", got)
	}
	if got := monthNameEn(hdate.Adar1, 5785); got != "Adar" { // non-leap
		t.Errorf("Adar1 non-leap = %q", got)
	}
	if got := monthNameEn(hdate.Adar2, 5784); got != "Adar II" {
		t.Errorf("Adar2 = %q", got)
	}
}

func TestMakeHebDateAdar2NonLeap(t *testing.T) {
	hd, err := makeHebDate("5785", "Adar2", "15")
	if err != nil {
		t.Fatal(err)
	}
	if hd.Month() != hdate.Adar1 {
		t.Errorf("Adar2 in non-leap year should map to Adar, got %v", hd.Month())
	}
}

func TestFutureYearsHebAdar(t *testing.T) {
	// 30 Adar I 5784 (leap year); in non-leap years becomes 1 Nisan
	orig := hdate.New(5784, hdate.Adar1, 30)
	found := false
	for _, hd := range futureYearsHeb(orig, 5) {
		if hd.Year() == 5785 {
			found = true
			if hd.Month() != hdate.Nisan || hd.Day() != 1 {
				t.Errorf("5785: got %s, want 1 Nisan", hdateString(hd))
			}
		}
	}
	if !found {
		t.Error("year 5785 missing")
	}
	// 15 Adar (non-leap) becomes 15 Adar II in leap years
	orig = hdate.New(5785, hdate.Adar1, 15)
	for _, hd := range futureYearsHeb(orig, 5) {
		if hd.Year() == 5787 && hd.Month() != hdate.Adar2 { // 5787 is a leap year
			t.Errorf("5787: got month %v, want Adar II", hd.Month())
		}
	}
}

func TestGematriyaDate(t *testing.T) {
	if got := gematriyaDate(hdate.New(5786, hdate.Tamuz, 20)); got != "כ׳ בְּתַמּוּז תשפ״ו" {
		t.Errorf("got %q", got)
	}
	if got := gematriyaDate(hdate.New(1, hdate.Tishrei, 1)); got != "א׳ בְּתִשְׁרֵי א׳" {
		t.Errorf("got %q", got)
	}
	// leap year Adar I includes the aleph suffix
	if got := gematriyaDate(hdate.New(5784, hdate.Adar1, 15)); got != "ט״ו בַּאֲדָר א׳ תשפ״ד" {
		t.Errorf("got %q", got)
	}
}

func TestStripCallback(t *testing.T) {
	if got := stripCallback("foo.bar<x>"); got != "foo.barx" {
		t.Errorf("got %q", got)
	}
	if got := stripCallback("cb_1.fn"); got != "cb_1.fn" {
		t.Errorf("got %q", got)
	}
}
