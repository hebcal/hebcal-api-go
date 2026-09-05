package model

import (
	"testing"

	"github.com/hebcal/hdate"
)

func TestMonthNameEn(t *testing.T) {
	if got := MonthNameEn(hdate.Tamuz, 5786); got != "Tamuz" {
		t.Errorf("Tamuz = %q", got)
	}
	if got := MonthNameEn(hdate.Adar1, 5784); got != "Adar I" { // leap year
		t.Errorf("Adar1 leap = %q", got)
	}
	if got := MonthNameEn(hdate.Adar1, 5785); got != "Adar" { // non-leap
		t.Errorf("Adar1 non-leap = %q", got)
	}
	if got := MonthNameEn(hdate.Adar2, 5784); got != "Adar II" {
		t.Errorf("Adar2 = %q", got)
	}
}

func TestMakeHebDateAdar2NonLeap(t *testing.T) {
	hd, err := MakeHebDate("5785", "Adar2", "15")
	if err != nil {
		t.Fatal(err)
	}
	if hd.Month() != hdate.Adar1 {
		t.Errorf("Adar2 in non-leap year should map to Adar, got %v", hd.Month())
	}
}

// TestMakeHebDateNumericMonth pins the leniency for a bare Hebrew month
// number (hdate.HMonth's own numbering: 1=Nisan .. 7=Tishrei .. 13=Adar II),
// seen in production logs (hm=7, hm=8, hm=10) alongside the documented month
// name.
func TestMakeHebDateNumericMonth(t *testing.T) {
	cases := []struct {
		hy, hm, hd string
		want       hdate.HMonth
	}{
		{"5785", "10", "14", hdate.Tevet},   // 10=Tevet
		{"5787", "8", "12", hdate.Cheshvan}, // 8=Cheshvan
		{"5787", "2", "10", hdate.Iyyar},    // 2=Iyyar
		{"5786", "7", "1", hdate.Tishrei},   // 7=Tishrei
		{"5786", "1", "1", hdate.Nisan},     // 1=Nisan
	}
	for _, tc := range cases {
		hd, err := MakeHebDate(tc.hy, tc.hm, tc.hd)
		if err != nil {
			t.Errorf("MakeHebDate(%s,%s,%s) unexpected error: %v", tc.hy, tc.hm, tc.hd, err)
			continue
		}
		if hd.Month() != tc.want {
			t.Errorf("MakeHebDate(%s,%s,%s).Month() = %v, want %v", tc.hy, tc.hm, tc.hd, hd.Month(), tc.want)
		}
	}
}

// TestMakeHebDateNumericMonth13NonLeap pins the same Adar-II-in-a-non-leap-year
// downgrade for the numeric path as MakeHebDateAdar2NonLeap pins for the name.
func TestMakeHebDateNumericMonth13NonLeap(t *testing.T) {
	hd, err := MakeHebDate("5785", "13", "15")
	if err != nil {
		t.Fatal(err)
	}
	if hd.Month() != hdate.Adar1 {
		t.Errorf("month 13 in non-leap year should map to Adar, got %v", hd.Month())
	}
}

// TestMakeHebDateNumericMonthOutOfRange pins that an out-of-range numeric
// month still falls through to the "bad monthName" error, not a panic or a
// silently wrapped month.
func TestMakeHebDateNumericMonthOutOfRange(t *testing.T) {
	for _, hm := range []string{"0", "14", "-1"} {
		if _, err := MakeHebDate("5786", hm, "1"); err == nil {
			t.Errorf("MakeHebDate month=%q: expected error, got none", hm)
		}
	}
}

func TestGematriyaDate(t *testing.T) {
	if got := GematriyaDate(hdate.New(5786, hdate.Tamuz, 20)); got != "כ׳ בְּתַמּוּז תשפ״ו" {
		t.Errorf("got %q", got)
	}
	if got := GematriyaDate(hdate.New(1, hdate.Tishrei, 1)); got != "א׳ בְּתִשְׁרֵי א׳" {
		t.Errorf("got %q", got)
	}
	// leap year Adar I includes the aleph suffix
	if got := GematriyaDate(hdate.New(5784, hdate.Adar1, 15)); got != "ט״ו בַּאֲדָר א׳ תשפ״ד" {
		t.Errorf("got %q", got)
	}
}
