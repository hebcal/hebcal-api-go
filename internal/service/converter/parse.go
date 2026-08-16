// Package converter implements the Hebrew Date Converter API: parsing the
// /converter query string into a single-date or date-range conversion, and
// rendering the result as JSON, XML, or CSV.
package converter

import (
	"net/url"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
)

// RangeRequiresCfgJSON is the error message for a range request that did not
// ask for cfg=json, the only format the batch response is defined for.
const RangeRequiresCfgJSON = "Date range conversion using 'start' and 'end' requires cfg=json"

// Props is the result of parsing the converter query string:
// either a single-date conversion or a date range.
type Props struct {
	IsRange bool
	// range conversion
	StartRD, EndRD int64
	// single conversion
	DT      model.GregDate // Gregorian civil date (before any sunset adjustment)
	HD      hdate.HDate    // Hebrew date (after sunset adjustment when GS is true)
	GS      bool           // after sunset
	NoCache bool           // date came from the current clock
}

// G2H converts a Gregorian date to Props, advancing the Hebrew date by
// one day when afterSunset is set.
func G2H(dt model.GregDate, gs, noCache bool) Props {
	hd := hdate.FromProlepticGregorian(dt.Year, dt.Month, dt.Day)
	if gs {
		hd = hd.Next()
	}
	return Props{DT: dt, HD: hd, GS: gs, NoCache: noCache}
}

// ParseQuery parses the /converter query string.
// Ported from hebcal-web src/converter.js parseConverterQuery().
func ParseQuery(q url.Values, now model.GregDate) (Props, error) {
	if !jsutil.QueryEmpty(q, "start") && !jsutil.QueryEmpty(q, "end") {
		return parseStartAndEnd(q)
	}
	if q.Has("h2g") && q.Get("strict") == "1" {
		for _, param := range []string{"hy", "hm", "hd"} {
			if jsutil.QueryEmpty(q, param) {
				return Props{}, model.BadRequest(
					"Missing parameter '%s' for conversion from Hebrew to Gregorian", param)
			}
		}
	}
	if q.Has("h2g") {
		if jsutil.QueryEmpty(q, "ndays") && jsutil.QueryEmpty(q, "hy") && jsutil.QueryEmpty(q, "hm") && jsutil.QueryEmpty(q, "hd") {
			return G2H(now, false, true), nil
		}
		// in either mode, this will fail if the params are invalid
		hd, err := model.MakeHebDate(jsutil.QueryGet(q, "hy"), q.Get("hm"), jsutil.QueryGet(q, "hd"))
		if err != nil {
			return Props{}, err
		}
		rd := hd.Abs()
		dt := model.GregFromRD(rd)
		if dt.Year > 9999 {
			return Props{}, model.BadRequest("Gregorian year cannot be greater than 9999: %d", dt.Year)
		}
		if !jsutil.QueryEmpty(q, "ndays") {
			ndays, ok := jsutil.ParseInt(q.Get("ndays"))
			if !ok || ndays < 1 {
				return Props{}, model.BadRequest("Invalid value for ndays: %s", q.Get("ndays"))
			}
			numDays := ndays - 1
			if numDays > model.MaxRangeDays-1 {
				numDays = model.MaxRangeDays - 1
			}
			return Props{IsRange: true, StartRD: rd, EndRD: rd + int64(numDays)}, nil
		}
		return Props{DT: dt, HD: hd}, nil
	}
	if q.Has("g2h") && q.Get("strict") == "1" {
		if q.Has("date") {
			if _, err := model.IsoDateStringToDate(q.Get("date")); err != nil {
				return Props{}, err
			}
		} else {
			for _, param := range []string{"gy", "gm", "gd"} {
				if jsutil.QueryEmpty(q, param) {
					return Props{}, model.BadRequest(
						"Missing parameter '%s' for conversion from Gregorian to Hebrew", param)
				}
			}
		}
	}
	gs := jsutil.IsOn(q.Get("gs"))
	if !jsutil.QueryEmpty(q, "date") {
		dt, err := model.IsoDateStringToDate(q.Get("date"))
		if err != nil {
			return Props{}, err
		}
		return G2H(dt, gs, false), nil
	} else if jsutil.QueryEmpty(q, "gy") && jsutil.QueryEmpty(q, "gm") && jsutil.QueryEmpty(q, "gd") {
		return G2H(now, gs, true), nil
	}
	dt, err := model.MakeGregDate(jsutil.QueryGet(q, "gy"), jsutil.QueryGet(q, "gm"), jsutil.QueryGet(q, "gd"))
	if err != nil {
		return Props{}, err
	}
	return G2H(dt, gs, false), nil
}

// parseStartAndEnd handles the start/end date-range parameters.
// Ported from hebcal-web src/dateUtil.js getStartAndEnd().
func parseStartAndEnd(q url.Values) (Props, error) {
	start := q.Get("start")
	end := q.Get("end")
	if start == end {
		dt, err := model.IsoDateStringToDate(start)
		if err != nil {
			return Props{}, err
		}
		return G2H(dt, false, false), nil
	}
	startD, err := model.IsoDateStringToDate(start)
	if err != nil {
		return Props{}, err
	}
	endD, err := model.IsoDateStringToDate(end)
	if err != nil {
		return Props{}, err
	}
	StartRD := startD.RD()
	EndRD := endD.RD()
	if EndRD < StartRD {
		return G2H(startD, false, false), nil
	}
	if EndRD-StartRD > model.MaxRangeDays {
		EndRD = StartRD + model.MaxRangeDays
	}
	return Props{IsRange: true, StartRD: StartRD, EndRD: EndRD}, nil
}
