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
