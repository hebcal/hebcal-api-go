package zmanim

import (
	"net/url"
	"regexp"
	"strings"
	"time"

	zman "github.com/hebcal/hebcal-go/zmanim"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

var reHasTZOffset = regexp.MustCompile(`[+-]\d\d:\d\d$`)

// ParseMelachaDate emulates the JavaScript `new Date(dateStr)` (plus the
// location-offset fixup) used by the im=1 branch: a trailing Z is UTC, an
// explicit ±HH:MM offset is honored, a bare YYYY-MM-DD is UTC midnight, and a
// datetime without a zone is interpreted as wall-clock time in the location.
func ParseMelachaDate(dateStr string, tz *time.Location) (time.Time, bool) {
	if strings.HasSuffix(dateStr, "Z") {
		if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
			return t, true
		}
		return time.Time{}, false
	}
	if reHasTZOffset.MatchString(dateStr) {
		for _, layout := range []string{"2006-01-02T15:04:05-07:00", "2006-01-02T15:04-07:00"} {
			if t, err := time.Parse(layout, dateStr); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	if len(dateStr) == 10 {
		if t, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC); err == nil {
			return t, true
		}
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02T15"} {
		if t, err := time.ParseInLocation(layout, dateStr, tz); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// NowInTimezone returns today's calendar date in the given timezone.
func NowInTimezone(tzid string) model.GregDate {
	loc, err := zman.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	y, m, d := time.Now().In(loc).Date()
	return model.GregDate{Year: y, Month: m, Day: d}
}

// StartAndEnd resolves the start, end, and date query parameters to a date
// or date range, ported from getStartAndEnd() in hebcal-web src/dateUtil.js.
func StartAndEnd(q url.Values, tzid string) (isRange bool, startD, endD model.GregDate, err error) {
	start := q.Get("start")
	end := q.Get("end")
	if start != "" && end == "" {
		end = start
	} else if start == "" && end != "" {
		start = end
	}
	date := q.Get("date")
	if start != "" && end != "" && start == end {
		date = start
		start, end = "", ""
	}
	isRange = start != "" && end != ""
	if isRange {
		if startD, err = model.IsoDateStringToDate(start); err != nil {
			return false, model.GregDate{}, model.GregDate{}, err
		}
		if endD, err = model.IsoDateStringToDate(end); err != nil {
			return false, model.GregDate{}, model.GregDate{}, err
		}
		if endD.RD() < startD.RD() {
			return false, startD, startD, nil
		}
		if endD.RD()-startD.RD() > model.MaxRangeDays {
			endD = model.GregFromRD(startD.RD() + model.MaxRangeDays)
		}
		return true, startD, endD, nil
	}
	var single model.GregDate
	if date == "" {
		single = NowInTimezone(tzid)
	} else if single, err = model.IsoDateStringToDate(date); err != nil {
		return false, model.GregDate{}, model.GregDate{}, err
	}
	return false, single, single, nil
}

// ExpiresTomorrow returns "now" and tomorrow's midnight in the location's
// timezone. The handler stamps them onto Last-Modified and Expires, matching
// expires() in zmanim.js: a request without an explicit date describes today,
// so its answer stops being true when the day rolls over there.
func ExpiresTomorrow(tzid string) (now, expires time.Time) {
	loc, err := zman.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	now = time.Now().In(loc)
	tomorrow := now.AddDate(0, 0, 1)
	expires = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, loc)
	return now, expires
}
