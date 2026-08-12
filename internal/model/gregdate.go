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

// MaxRangeDays caps a date-range request, matching the limit hebcal-web
// applies to /converter and /zmanim.
const MaxRangeDays = 399

// GregDate is a proleptic Gregorian calendar date.
type GregDate struct {
	Year  int
	Month time.Month
	Day   int
}

// RD returns the R.D. (rata die) day number of the date.
func (g GregDate) RD() int64 {
	return greg.ProlepticToRD(g.Year, g.Month, g.Day)
}

// String formats the date as YYYY-MM-DD, matching JS Date.toISOString.
func (g GregDate) String() string {
	return jsutil.IsoDateString(g.Year, g.Month, g.Day)
}

// GregFromRD converts an R.D. day number back to a Gregorian date.
func GregFromRD(rd int64) GregDate {
	y, m, d := greg.ProlepticFromRD(rd)
	return GregDate{Year: y, Month: m, Day: d}
}

// rdEpochHebrew is the R.D. day number of 1 Tishrei 1
// (proleptic Gregorian -003760-09-07).
var rdEpochHebrew = hdate.ToRD(1, hdate.Tishrei, 1)

// ReIsoDate matches the YYYY-MM-DD prefix the JS routes validate against.
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
	y, m, d := t.Date()
	return GregDate{Year: y, Month: m, Day: d}
}
