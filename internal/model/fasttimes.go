package model

import (
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// FastTimes holds the requested start and end times of fast days. Each pair is
// mutually exclusive -- at most one of StartDeg and StartMins is nonzero, and
// likewise for the other two -- and a zero pair means the default.
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

	// alot16Point1 is Alot HaShachar, the default start of a minor fast.
	alot16Point1 = 16.1
	// tzeitTucazinsky is the default end of Tish'a B'Av, 6.45 degrees as
	// calculated by Rabbi Yechiel Michel Tucazinsky.
	tzeitTucazinsky = 6.45
	// minorFastEndMinutesIL is the default end of a minor fast in Israel, 15
	// minutes after sunset (Rabbi Deblitzky's practice). Elsewhere it is tzeit
	// for three medium stars.
	minorFastEndMinutesIL = 15
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

// RetimeFasts recomputes every "Fast begins" and "Fast ends" time in events,
// which hebcal-go always places at fixed defaults, from f and the calendar's
// Israel setting:
//
//   - a minor fast (Yom Kippur Katan included) begins at StartMins before
//     sunrise, or when the sun is StartDeg below the horizon, or else at Alot
//     HaShachar (16.1 degrees);
//   - it ends EndMins after sunset, or at tzeit EndDeg, or else 15 minutes
//     after sunset in Israel and at tzeit 7.083 degrees elsewhere;
//   - Tish'a B'Av ends TishaBavEndMins after sunset, or at tzeit
//     TishaBavEndDeg, or else at tzeit 6.45 degrees, in Israel too. It begins
//     at sunset the evening before whatever f says.
//
// A time that does not occur at the location (polar day or night) keeps the
// time hebcal-go gave it.
func RetimeFasts(events []event.CalEvent, opts *hebcal.CalOptions, f FastTimes) {
	for i, ev := range events {
		timed, ok := ev.(hebcal.TimedEvent)
		if !ok || (timed.Desc != "Fast begins" && timed.Desc != "Fast ends") || timed.LinkedEvent == nil {
			continue
		}
		fast := timed.LinkedEvent.Render("en")
		if fast == "Erev Tish'a B'Av" {
			continue // begins at sunset
		}
		z := fastZmanim(timed.Date, opts)
		var t time.Time
		switch {
		case timed.Desc == "Fast begins":
			t = fastStartTime(&z, f)
		case strings.HasPrefix(fast, "Tish'a B'Av"):
			t = fastEndTime(&z, f.TishaBavEndMins, f.TishaBavEndDeg, tzeitTucazinsky)
		case f.EndMins == 0 && f.EndDeg == 0 && opts.IL:
			t = z.SunsetOffset(minorFastEndMinutesIL, true)
		default:
			t = fastEndTime(&z, f.EndMins, f.EndDeg, zmanim.Tzeit3MediumStars)
		}
		if t.IsZero() {
			continue
		}
		events[i] = hebcal.NewTimedEvent(timed.Date, timed.Desc, timed.Flags, t, 0, timed.LinkedEvent, opts)
	}
}

func fastZmanim(hd hdate.HDate, opts *hebcal.CalOptions) zmanim.Zmanim {
	gy, gm, gd := hd.Greg()
	z := zmanim.New(opts.Location, time.Date(gy, gm, gd, 0, 0, 0, 0, time.UTC))
	z.UseElevation = opts.UseElevation
	return z
}

func fastStartTime(z *zmanim.Zmanim, f FastTimes) time.Time {
	if f.StartMins != 0 {
		return z.SunriseOffset(-f.StartMins, true)
	}
	deg := f.StartDeg
	if deg == 0 {
		deg = alot16Point1
	}
	return z.TimeAtAngle(deg, true)
}

func fastEndTime(z *zmanim.Zmanim, mins int, deg, defaultDeg float64) time.Time {
	if mins != 0 {
		return z.SunsetOffset(mins, true)
	}
	if deg == 0 {
		deg = defaultDeg
	}
	return z.Tzeit(deg)
}
