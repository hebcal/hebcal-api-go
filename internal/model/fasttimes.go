package model

import (
	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// FastTimes holds the requested start and end times of fast days. Each pair is
// mutually exclusive -- at most one of StartDeg and StartMins is nonzero, and
// likewise for the other two -- and a zero pair means hebcal-go's default.
// They map one-to-one onto the CalOptions fields of the same meaning.
type FastTimes struct {
	// StartDeg starts minor fasts when the sun is this many degrees below the
	// horizon in the morning; StartMins starts them this many minutes before
	// sunrise.
	StartDeg  float64
	StartMins int
	// EndDeg ends minor fasts at tzeit for this many degrees; EndMins ends them
	// this many minutes after sunset.
	EndDeg  float64
	EndMins int
	// TishaBavEndDeg and TishaBavEndMins end Tish'a B'Av the same way.
	TishaBavEndDeg  float64
	TishaBavEndMins int
}

// FastTimeParams names the query parameters that carry FastTimes, as
// (degrees, minutes) pairs: minor fast start, minor fast end, Tish'a B'Av end.
var FastTimeParams = [3][2]string{{"fsd", "fsm"}, {"fed", "fem"}, {"tbed", "tbem"}}

const (
	maxFastDeg  = 90
	maxFastMins = 240
)

// ParseFastTimes reads the fsd/fsm, fed/fem and tbed/tbem parameters. get
// returns a parameter's value, or "" when it is absent.
//
// Degrees must be in (0, 90) and minutes in 1..240; a negative value is taken
// as positive, and anything unparsable or out of range is ignored. When both
// halves of a pair are given, degrees wins.
func ParseFastTimes(get func(string) string) FastTimes {
	var f FastTimes
	dst := [3]struct {
		deg  *float64
		mins *int
	}{
		{&f.StartDeg, &f.StartMins},
		{&f.EndDeg, &f.EndMins},
		{&f.TishaBavEndDeg, &f.TishaBavEndMins},
	}
	for i, names := range FastTimeParams {
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
	return f
}

// Apply copies the fast times into the calendar options.
func (f FastTimes) Apply(o *hebcal.CalOptions) {
	o.FastStartDeg, o.FastStartMins = f.StartDeg, f.StartMins
	o.FastEndDeg, o.FastEndMins = f.EndDeg, f.EndMins
	o.TishaBavEndDeg, o.TishaBavEndMins = f.TishaBavEndDeg, f.TishaBavEndMins
}
