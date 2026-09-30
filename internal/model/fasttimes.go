package model

import (
	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// FastTimeParams names the query parameters that carry the fast start and end
// times, as (degrees, minutes) pairs: minor fast start, minor fast end, Tish'a
// B'Av end.
var FastTimeParams = [3][2]string{{"fsd", "fsm"}, {"fed", "fem"}, {"tbed", "tbem"}}

const (
	maxFastDeg  = 90
	maxFastMins = 240
)

// SetFastTimes reads the fsd/fsm, fed/fem and tbed/tbem parameters into the
// fast start and end fields of o. get returns a parameter's value, or "" when
// it is absent; a pair with nothing valid is left zero, which means the
// default.
//
// Degrees must be in (0, 90) and minutes in 1..240; a negative value is taken
// as positive, and anything unparsable or out of range is ignored. When both
// halves of a pair are given, degrees wins, so the pair is never the
// mutually-exclusive combination HebrewCalendar rejects.
func SetFastTimes(o *hebcal.CalOptions, get func(string) string) {
	dst := [3]struct {
		deg  *float64
		mins *int
	}{
		{&o.FastStartDeg, &o.FastStartMins},
		{&o.FastEndDeg, &o.FastEndMins},
		{&o.TishaBavEndDeg, &o.TishaBavEndMins},
	}
	for i, names := range FastTimeParams {
		*dst[i].deg, *dst[i].mins = 0, 0
		if v := get(names[0]); v != "" {
			if deg, err := jsutil.ParseFloat(v); err == nil {
				if deg < 0 {
					deg = -deg
				}
				if deg > 0 && deg < maxFastDeg {
					*dst[i].deg = deg
				}
			}
		}
		if v := get(names[1]); v != "" && *dst[i].deg == 0 {
			if mins, ok := jsutil.ParseInt(v); ok {
				if mins < 0 {
					mins = -mins
				}
				if mins > 0 && mins <= maxFastMins {
					*dst[i].mins = mins
				}
			}
		}
	}
}
