package main

import (
	"fmt"
	"regexp"
	"time"

	"github.com/hebcal/greg"
	"github.com/hebcal/hdate"
)

// httpError carries an HTTP status code along with the error message.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

func badRequest(format string, args ...interface{}) *httpError {
	return &httpError{status: 400, message: fmt.Sprintf(format, args...)}
}

// gregDate is a proleptic Gregorian calendar date.
type gregDate struct {
	Year  int
	Month time.Month
	Day   int
}

func (g gregDate) RD() int64 {
	return greg.ProlepticToRD(g.Year, g.Month, g.Day)
}

func (g gregDate) String() string {
	return isoDateString(g.Year, g.Month, g.Day)
}

func gregFromRD(rd int64) gregDate {
	y, m, d := greg.ProlepticFromRD(rd)
	return gregDate{Year: y, Month: m, Day: d}
}

// rdEpochHebrew is the R.D. day number of 1 Tishrei 1
// (proleptic Gregorian -003760-09-07).
var rdEpochHebrew = hdate.ToRD(1, hdate.Tishrei, 1)

var reIsoDate = regexp.MustCompile(`^\d\d\d\d-\d\d-\d\d`)

// isoDateStringToDate parses a YYYY-MM-DD string like the JS
// isoDateStringToDate: only the prefix is validated, and out-of-range
// month/day values roll over the way a JavaScript Date does.
func isoDateStringToDate(s string) (gregDate, error) {
	if !reIsoDate.MatchString(s) {
		return gregDate{}, badRequest("Date does not match format YYYY-MM-DD: %s", s)
	}
	yy, _ := parseInt(s)
	mm, _ := parseInt(s[5:7])
	dd, _ := parseInt(s[8:10])
	// normalize out-of-range month/day the same way new Date(y, m, d) does
	t := time.Date(yy, time.Month(mm), dd, 12, 0, 0, 0, time.UTC)
	y2, m2, d2 := t.Date()
	return gregDate{Year: y2, Month: m2, Day: d2}, nil
}

// makeGregDate validates a Gregorian yy/mm/dd from query-string values and
// returns the date. Ported from hebcal-web src/dateUtil.js makeGregDate().
func makeGregDate(gy, gm, gd string) (gregDate, error) {
	yy, okY := parseInt(gy)
	mm, okM := parseInt(gm)
	dd, okD := parseInt(gd)
	if !okD {
		return gregDate{}, badRequest("Gregorian day must be numeric: %s", gd)
	} else if !okM {
		return gregDate{}, badRequest("Gregorian month must be numeric: %s", gm)
	} else if !okY {
		return gregDate{}, badRequest("Gregorian year must be numeric: %s", gy)
	} else if mm > 12 || mm < 1 {
		return gregDate{}, badRequest("Gregorian month out of valid range 1-12: %s", gm)
	} else if yy > 9999 {
		return gregDate{}, badRequest("Gregorian year cannot be greater than 9999: %s", gy)
	}
	maxDay := greg.DaysIn(time.Month(mm), yy)
	if dd < 1 || dd > maxDay {
		return gregDate{}, badRequest("Gregorian day %d out of valid range for %d/%d", dd, mm, yy)
	}
	dt := gregDate{Year: yy, Month: time.Month(mm), Day: dd}
	// Hebrew date 1 Tishrei 1 == Gregorian -003760-09-07. The JS epoch
	// comparison rejects 1 Tishrei 1 itself, so <= rather than <.
	if dt.RD() <= rdEpochHebrew {
		return gregDate{}, badRequest("Gregorian date before Hebrew year 1: %s", dt.String())
	}
	return dt, nil
}

// makeHebDate validates a Hebrew yy/mm/dd from query-string values.
// Ported from hebcal-web src/dateUtil.js makeHebDate().
func makeHebDate(hyStr, hmStr, hdStr string) (hdate.HDate, error) {
	hy, okY := parseInt(hyStr)
	hd, okD := parseInt(hdStr)
	if !okD {
		return hdate.HDate{}, badRequest("Hebrew day must be numeric: %s", hdStr)
	} else if !okY {
		return hdate.HDate{}, badRequest("Hebrew year must be numeric: %s", hyStr)
	} else if hy < 1 {
		return hdate.HDate{}, badRequest("Hebrew year must be year 1 or later: %d", hy)
	} else if hy > 32000 {
		return hdate.HDate{}, badRequest("Hebrew year is too large: %d", hy)
	}
	if hmStr == "" {
		return hdate.HDate{}, badRequest("Hebrew month is required")
	}
	hm, err := hdate.MonthFromName(hmStr)
	if err != nil {
		return hdate.HDate{}, badRequest("bad monthName: %s", hmStr)
	}
	if hm == hdate.Adar2 && !hdate.IsLeapYear(hy) {
		hm = hdate.Adar1
	}
	maxDay := hdate.DaysInMonth(hm, hy)
	if hd < 1 || hd > maxDay {
		monthName := monthNameEn(hm, hy)
		return hdate.HDate{}, badRequest("Hebrew day out of valid range 1-%d for %s %d", maxDay, monthName, hy)
	}
	return hdate.New(hy, hm, hd), nil
}

// hdateFromRD is a convenience wrapper.
func hdateFromRD(rd int64) hdate.HDate {
	return hdate.FromRD(rd)
}

// newHDateLenient behaves like the JavaScript `new HDate(day, month, year)`,
// which rolls an out-of-range day over into the following month
// (e.g. 30 Cheshvan in a year when Cheshvan has 29 days becomes 1 Kislev).
func newHDateLenient(year int, month hdate.HMonth, day int) hdate.HDate {
	if month == hdate.Adar2 && !hdate.IsLeapYear(year) {
		month = hdate.Adar1
	}
	dim := hdate.DaysInMonth(month, year)
	if day > dim {
		return hdate.FromRD(hdate.ToRD(year, month, dim) + int64(day-dim))
	}
	return hdate.New(year, month, day)
}

// simchatTorahDate returns the date of Simchat Torah for the given Hebrew
// year (22 Tishrei in Israel, 23 Tishrei in the Diaspora).
func simchatTorahDate(year int, il bool) hdate.HDate {
	mday := 23
	if il {
		mday = 22
	}
	return hdate.New(year, hdate.Tishrei, mday)
}
