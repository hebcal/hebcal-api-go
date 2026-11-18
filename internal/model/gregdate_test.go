package model

import (
	"testing"
	"time"

	"github.com/hebcal/hdate"
)

// Every Gregorian date here is proleptic, like time.Time: before the 1752
// switch the historical (Julian) calendar would put these dates eleven days
// off, give weekdays that disagree with time.Time, and panic on the eleven
// days the switch skipped.
func TestProlepticConversions(t *testing.T) {
	cases := []struct {
		g  GregDate
		hd hdate.HDate
	}{
		{GregDate{1700, time.January, 1}, hdate.New(5460, hdate.Tevet, 10)},
		{GregDate{1700, time.September, 14}, hdate.New(5461, hdate.Tishrei, 1)},
		{GregDate{1752, time.March, 1}, hdate.New(5512, hdate.Adar1, 15)},
		// skipped in Britain's calendar, a valid proleptic date
		{GregDate{1752, time.September, 5}, hdate.New(5512, hdate.Elul, 26)},
		{GregDate{1752, time.September, 14}, hdate.New(5513, hdate.Tishrei, 6)},
		{GregDate{2026, time.November, 18}, hdate.New(5787, hdate.Kislev, 8)},
	}
	for _, c := range cases {
		if got := c.g.HDate(); got != c.hd {
			t.Errorf("%s.HDate() = %v, want %v", c.g, got, c.hd)
		}
		if got := HDateOf(c.g.Noon(time.UTC)); got != c.hd {
			t.Errorf("HDateOf(%s) = %v, want %v", c.g, got, c.hd)
		}
		if got := GregDateOfHDate(c.hd); got != c.g {
			t.Errorf("GregDateOfHDate(%v) = %s, want %s", c.hd, got, c.g)
		}
		if got := IsoGreg(c.hd); got != c.g.String() {
			t.Errorf("IsoGreg(%v) = %s, want %s", c.hd, got, c.g)
		}
		gt := GregorianOf(c.hd)
		if GregDateOf(gt) != c.g || gt.Location() != time.UTC || gt.Hour() != 0 {
			t.Errorf("GregorianOf(%v) = %v, want midnight UTC on %s", c.hd, gt, c.g)
		}
		if gt.Weekday() != c.hd.Weekday() {
			t.Errorf("%s: time.Time says %v, the Hebrew date %v", c.g, gt.Weekday(), c.hd.Weekday())
		}
		if c.g.RD() != c.hd.Abs() {
			t.Errorf("%s.RD() = %d, Hebrew date's is %d", c.g, c.g.RD(), c.hd.Abs())
		}
	}
}
