package converter

import (
	"testing"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

func TestFutureYearsHebAdar(t *testing.T) {
	// 30 Adar I 5784 (leap year); in non-leap years becomes 1 Nisan
	orig := hdate.New(5784, hdate.Adar1, 30)
	found := false
	for _, hd := range FutureYearsHeb(orig, 5) {
		if hd.Year() == 5785 {
			found = true
			if hd.Month() != hdate.Nisan || hd.Day() != 1 {
				t.Errorf("5785: got %s, want 1 Nisan", model.HDateString(hd))
			}
		}
	}
	if !found {
		t.Error("year 5785 missing")
	}
	// 15 Adar (non-leap) becomes 15 Adar II in leap years
	orig = hdate.New(5785, hdate.Adar1, 15)
	for _, hd := range FutureYearsHeb(orig, 5) {
		if hd.Year() == 5787 && hd.Month() != hdate.Adar2 { // 5787 is a leap year
			t.Errorf("5787: got month %v, want Adar II", hd.Month())
		}
	}
}

func TestStripCallback(t *testing.T) {
	if got := StripCallback("foo.bar<x>"); got != "foo.barx" {
		t.Errorf("got %q", got)
	}
	if got := StripCallback("cb_1.fn"); got != "cb_1.fn" {
		t.Errorf("got %q", got)
	}
}
