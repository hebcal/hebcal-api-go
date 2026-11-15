package fridge

import (
	"net/url"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

func TestParseYear(t *testing.T) {
	// 20 Tamuz 5786
	summer := model.GregDate{Year: 2026, Month: time.July, Day: 5}
	// 6 Kislev 5787
	autumn := model.GregDate{Year: 2026, Month: time.November, Day: 15}
	// 16 Av 5786: past 15 Av, so the default is already next year
	lateAv := model.GregDate{Year: 2026, Month: time.July, Day: 30}
	tests := []struct {
		query      string
		today      model.GregDate
		hebrew     bool
		number     int
		start, end string
		isDefault  bool
		status     int
	}{
		{"", summer, true, 5786, "2025-09-22", "2026-09-11", true, 0},
		{"", lateAv, true, 5787, "2026-09-11", "2027-10-01", true, 0},
		{"", autumn, true, 5787, "2026-09-11", "2027-10-01", true, 0},
		// yt alone does not choose a calendar
		{"yt=G", autumn, true, 5787, "2026-09-11", "2027-10-01", true, 0},
		{"year=5788", autumn, true, 5788, "2027-10-01", "2028-09-20", false, 0},
		{"year=5788&yt=H", autumn, true, 5788, "2027-10-01", "2028-09-20", false, 0},
		{"year=2027&yt=G", autumn, false, 2027, "2027-01-01", "2027-12-31", false, 0},
		{"year=now&yt=G", autumn, false, 2026, "2026-01-01", "2026-12-31", false, 0},
		{"year=x", autumn, true, 5787, "2026-09-11", "2027-10-01", false, 0},
		{"year=0", autumn, true, 5787, "2026-09-11", "2027-10-01", false, 0},
		// the window: four years back, twenty ahead
		{"year=2022&yt=G", autumn, false, 2022, "2022-01-01", "2022-12-31", false, 0},
		{"year=2021&yt=G", autumn, false, 0, "", "", false, 410},
		{"year=2046&yt=G", autumn, false, 2046, "2046-01-01", "2046-12-31", false, 0},
		{"year=2047&yt=G", autumn, false, 0, "", "", false, 410},
		{"year=5782", autumn, false, 0, "", "", false, 410},
		{"year=5808", autumn, false, 0, "", "", false, 410},
		// a Gregorian-looking year with no yt is a Hebrew year long gone
		{"year=2027", autumn, false, 0, "", "", false, 410},
		{"year=abc", autumn, false, 0, "", "", false, 400},
	}
	for _, tc := range tests {
		q, _ := url.ParseQuery(tc.query)
		y, err := ParseYear(q, tc.today)
		if tc.status != 0 {
			if err == nil || model.StatusOf(err) != tc.status {
				t.Errorf("%q: got %+v, %v; want status %d", tc.query, y, err, tc.status)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.query, err)
			continue
		}
		start, end := model.GregFromRD(y.Start).String(), model.GregFromRD(y.End).String()
		if y.Hebrew != tc.hebrew || y.Number != tc.number || start != tc.start || end != tc.end || y.Default != tc.isDefault {
			t.Errorf("%q: got hebrew=%v %d %s..%s default=%v", tc.query, y.Hebrew, y.Number, start, end, y.Default)
		}
	}
}

func TestNarrowClass(t *testing.T) {
	tests := []struct{ lang, reason, want string }{
		{"en", "Bereshit", ""},
		{"en", "Shmini Atzeret", ""},
		{"en", "Achrei Mot-Kedoshim", "narrow"},
		{"ru", "Ахарей Мот", ""},
		{"ru", "Шмини Ацерет", "narrow"},
		{"he", "אַחֲרֵי מוֹת־קְדֹשִׁים", "text-right"},
	}
	for _, tc := range tests {
		if got := narrowClass(tc.lang, tc.reason); got != tc.want {
			t.Errorf("narrowClass(%q, %q) = %q, want %q", tc.lang, tc.reason, got, tc.want)
		}
	}
}
