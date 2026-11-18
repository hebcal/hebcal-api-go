// Package model holds the domain entities and calendar logic shared by the
// service layer: Gregorian and Hebrew date values, their validation and
// formatting, the locale resolution the API's `lg` parameter drives, and the
// calendar events a Hebrew date carries.
package model

import (
	"regexp"
	"time"

	"github.com/hebcal/greg"
	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// MaxRangeDays caps a date-range request on /converter and /zmanim.
const MaxRangeDays = 179

// GregDate is a proleptic Gregorian calendar date: the Gregorian rules run
// back before 1752 with no Julian period, as in time.Time.
//
// The hdate and greg packages also offer a historical calendar that is Julian
// before 14 September 1752 (hdate.FromGregorian, FromTime, HDate.Greg and
// HDate.Gregorian). This service never uses it: those dates disagree with
// time.Time by up to eleven days, so a weekday computed from one is wrong for
// the other, and the eleven dates the switch skipped make FromGregorian panic.
// Convert through HDateOf, GregDate.HDate, GregDateOfHDate and GregorianOf.
type GregDate struct {
	Year  int
	Month time.Month
	Day   int
}

// RD returns the R.D. (rata die) day number of the date.
func (g GregDate) RD() int64 {
	return greg.ProlepticToRD(g.Year, g.Month, g.Day)
}

// String formats the date as YYYY-MM-DD.
func (g GregDate) String() string {
	return jsutil.IsoDateString(g.Year, g.Month, g.Day)
}

// HDate returns the Hebrew date of the same day.
func (g GregDate) HDate() hdate.HDate {
	return hdate.FromProlepticGregorian(g.Year, g.Month, g.Day)
}

// HDateOf returns the Hebrew date of t's calendar date in t's own location.
func HDateOf(t time.Time) hdate.HDate {
	return GregDateOf(t).HDate()
}

// GregDateOfHDate returns the Gregorian date of a Hebrew date.
func GregDateOfHDate(hd hdate.HDate) GregDate {
	y, m, d := hd.ProlepticGreg()
	return GregDate{Year: y, Month: m, Day: d}
}

// GregorianOf returns midnight UTC on the Gregorian date of a Hebrew date.
func GregorianOf(hd hdate.HDate) time.Time {
	return GregDateOfHDate(hd).Midnight(time.UTC)
}

// Midnight returns the start of the date in loc.
func (g GregDate) Midnight(loc *time.Location) time.Time {
	return time.Date(g.Year, g.Month, g.Day, 0, 0, 0, 0, loc)
}

// Noon returns midday of the date in loc, an instant that stays on the same
// date whatever a DST transition does to the hours around it.
func (g GregDate) Noon(loc *time.Location) time.Time {
	return time.Date(g.Year, g.Month, g.Day, 12, 0, 0, 0, loc)
}

// GregDateOf returns the calendar date of t in t's own location.
func GregDateOf(t time.Time) GregDate {
	y, m, d := t.Date()
	return GregDate{Year: y, Month: m, Day: d}
}

// GregFromRD converts an R.D. day number back to a Gregorian date.
func GregFromRD(rd int64) GregDate {
	y, m, d := greg.ProlepticFromRD(rd)
	return GregDate{Year: y, Month: m, Day: d}
}

// rdEpochHebrew is the R.D. day number of 1 Tishrei 1
// (proleptic Gregorian -003760-09-07).
var rdEpochHebrew = hdate.ToRD(1, hdate.Tishrei, 1)

// ReIsoDate matches the YYYY-MM-DD prefix the routes validate against.
var ReIsoDate = regexp.MustCompile(`^\d\d\d\d-\d\d-\d\d`)

// nyLoc is the America/New_York location used to resolve "today" for the
// /converter route, which has no per-request location of its own.
var nyLoc *time.Location

// LoadNewYork loads the America/New_York timezone used by TodayNewYork. It is
// called once at startup so a missing tzdata is reported there rather than
// silently changing which day "today" is.
func LoadNewYork() error {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return err
	}
	nyLoc = loc
	return nil
}

// TodayNewYork returns the current civil date in the America/New_York
// timezone; used when the query string omits the date entirely.
func TodayNewYork() GregDate {
	t := time.Now()
	if nyLoc != nil {
		t = t.In(nyLoc)
	}
	return GregDateOf(t)
}
