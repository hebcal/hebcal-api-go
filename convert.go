package main

import (
	"net/url"
	"time"

	"github.com/hebcal/hdate"
)

const rangeRequiresCfgJSON = "Date range conversion using 'start' and 'end' requires cfg=json"

const maxRangeDays = 399

// convProps is the result of parsing the converter query string:
// either a single-date conversion or a date range.
type convProps struct {
	isRange bool
	// range conversion
	startRD, endRD int64
	// single conversion
	dt      gregDate    // Gregorian civil date (before any sunset adjustment)
	hd      hdate.HDate // Hebrew date (after sunset adjustment when gs is true)
	gs      bool        // after sunset
	noCache bool        // date came from the current clock
}

// qget returns the query param value, or "undefined" when the parameter is
// absent, mimicking how the JS code interpolates missing values into error
// messages (parseInt(undefined) => "must be numeric: undefined").
func qget(q url.Values, key string) string {
	if !q.Has(key) {
		return "undefined"
	}
	return q.Get(key)
}

func empty(q url.Values, key string) bool {
	return q.Get(key) == ""
}

// g2h converts a Gregorian date to convProps, advancing the Hebrew date by
// one day when afterSunset is set.
func g2h(dt gregDate, gs bool, noCache bool) convProps {
	hd := hdate.FromProlepticGregorian(dt.Year, dt.Month, dt.Day)
	if gs {
		hd = hd.Next()
	}
	return convProps{dt: dt, hd: hd, gs: gs, noCache: noCache}
}

// parseConverterQuery parses the /converter query string.
// Ported from hebcal-web src/converter.js parseConverterQuery().
func parseConverterQuery(q url.Values, now gregDate) (convProps, error) {
	if !empty(q, "start") && !empty(q, "end") {
		return parseStartAndEnd(q)
	}
	if q.Has("h2g") && q.Get("strict") == "1" {
		for _, param := range []string{"hy", "hm", "hd"} {
			if empty(q, param) {
				return convProps{}, badRequest(
					"Missing parameter '%s' for conversion from Hebrew to Gregorian", param)
			}
		}
	}
	if q.Has("h2g") {
		if empty(q, "ndays") && empty(q, "hy") && empty(q, "hm") && empty(q, "hd") {
			return g2h(now, false, true), nil
		}
		// in either mode, this will fail if the params are invalid
		hd, err := makeHebDate(qget(q, "hy"), q.Get("hm"), qget(q, "hd"))
		if err != nil {
			return convProps{}, err
		}
		rd := hd.Abs()
		dt := gregFromRD(rd)
		if dt.Year > 9999 {
			return convProps{}, badRequest("Gregorian year cannot be greater than 9999: %d", dt.Year)
		}
		if !empty(q, "ndays") {
			ndays, ok := parseInt(q.Get("ndays"))
			if !ok || ndays < 1 {
				return convProps{}, badRequest("Invalid value for ndays: %s", q.Get("ndays"))
			}
			numDays := ndays - 1
			if numDays > maxRangeDays-1 {
				numDays = maxRangeDays - 1
			}
			return convProps{isRange: true, startRD: rd, endRD: rd + int64(numDays)}, nil
		}
		return convProps{dt: dt, hd: hd}, nil
	}
	if q.Has("g2h") && q.Get("strict") == "1" {
		if q.Has("date") {
			if _, err := isoDateStringToDate(q.Get("date")); err != nil {
				return convProps{}, err
			}
		} else {
			for _, param := range []string{"gy", "gm", "gd"} {
				if empty(q, param) {
					return convProps{}, badRequest(
						"Missing parameter '%s' for conversion from Gregorian to Hebrew", param)
				}
			}
		}
	}
	gs := q.Get("gs") == "on" || q.Get("gs") == "1"
	if !empty(q, "date") {
		dt, err := isoDateStringToDate(q.Get("date"))
		if err != nil {
			return convProps{}, err
		}
		return g2h(dt, gs, false), nil
	} else if empty(q, "gy") && empty(q, "gm") && empty(q, "gd") {
		return g2h(now, gs, true), nil
	}
	dt, err := makeGregDate(qget(q, "gy"), qget(q, "gm"), qget(q, "gd"))
	if err != nil {
		return convProps{}, err
	}
	return g2h(dt, gs, false), nil
}

// parseStartAndEnd handles the start/end date-range parameters.
// Ported from hebcal-web src/dateUtil.js getStartAndEnd().
func parseStartAndEnd(q url.Values) (convProps, error) {
	start := q.Get("start")
	end := q.Get("end")
	if start == end {
		dt, err := isoDateStringToDate(start)
		if err != nil {
			return convProps{}, err
		}
		return g2h(dt, false, false), nil
	}
	startD, err := isoDateStringToDate(start)
	if err != nil {
		return convProps{}, err
	}
	endD, err := isoDateStringToDate(end)
	if err != nil {
		return convProps{}, err
	}
	startRD := startD.RD()
	endRD := endD.RD()
	if endRD < startRD {
		return g2h(startD, false, false), nil
	}
	if endRD-startRD > maxRangeDays {
		endRD = startRD + maxRangeDays
	}
	return convProps{isRange: true, startRD: startRD, endRD: endRD}, nil
}

// nowInNewYork returns the current civil date in the America/New_York
// timezone; used when the query string omits the date entirely.
var nyLoc *time.Location

func todayNewYork() gregDate {
	t := time.Now()
	if nyLoc != nil {
		t = t.In(nyLoc)
	}
	y, m, d := t.Date()
	return gregDate{Year: y, Month: m, Day: d}
}
