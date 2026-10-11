// Package zmanim computes the halachic times behind the /zmanim API. It returns
// times for a single date or a date range, and (with im=1) an "is work
// prohibited" status.
package zmanim

import (
	"time"

	"github.com/hebcal/hebcal-go/hebcal"
	zman "github.com/hebcal/hebcal-go/zmanim"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// ---------------------------------------------------------------------------
// Zman name tables. timeFuncs covers the fixed and degree-based times;
// tzeitDeg and tzeitMin cover the tzeit variants (degree-based vs
// fixed-minutes after sunset).
// ---------------------------------------------------------------------------

var timeFuncs = map[string]func(z *zman.Zmanim) time.Time{
	"chatzotNight":             (*zman.Zmanim).ChatzotNight,
	"alosBaalHatanya":          (*zman.Zmanim).AlosBaalHatanya,
	"alotHaShachar":            (*zman.Zmanim).AlotHaShachar,
	"misheyakir":               (*zman.Zmanim).Misheyakir,
	"misheyakirMachmir":        (*zman.Zmanim).MisheyakirMachmir,
	"dawn":                     (*zman.Zmanim).Dawn,
	"sunrise":                  (*zman.Zmanim).Sunrise,
	"seaLevelSunrise":          (*zman.Zmanim).SeaLevelSunrise,
	"sofZmanShmaMGA19Point8":   (*zman.Zmanim).SofZmanShmaMGA19Point8,
	"sofZmanShmaMGA16Point1":   (*zman.Zmanim).SofZmanShmaMGA16Point1,
	"sofZmanShmaMGA":           (*zman.Zmanim).SofZmanShmaMGA,
	"sofZmanShmaBaalHatanya":   (*zman.Zmanim).SofZmanShmaBaalHatanya,
	"sofZmanShma":              (*zman.Zmanim).SofZmanShma,
	"sofZmanTfillaMGA19Point8": (*zman.Zmanim).SofZmanTfillaMGA19Point8,
	"sofZmanTfillaMGA16Point1": (*zman.Zmanim).SofZmanTfillaMGA16Point1,
	"sofZmanTfillaMGA":         (*zman.Zmanim).SofZmanTfillaMGA,
	"sofZmanTfilaBaalHatanya":  (*zman.Zmanim).SofZmanTfilaBaalHatanya,
	"sofZmanTfilla":            (*zman.Zmanim).SofZmanTfilla,
	"chatzot":                  (*zman.Zmanim).Chatzot,
	"minchaGedola":             (*zman.Zmanim).MinchaGedola,
	"minchaGedolaBaalHatanya":  (*zman.Zmanim).MinchaGedolaBaalHatanya,
	"minchaGedolaMGA":          (*zman.Zmanim).MinchaGedolaMGA,
	"minchaKetana":             (*zman.Zmanim).MinchaKetana,
	"minchaKetanaBaalHatanya":  (*zman.Zmanim).MinchaKetanaBaalHatanya,
	"minchaKetanaMGA":          (*zman.Zmanim).MinchaKetanaMGA,
	"plagHaMincha":             (*zman.Zmanim).PlagHaMincha,
	"plagHaminchaBaalHatanya":  (*zman.Zmanim).PlagHaminchaBaalHatanya,
	"seaLevelSunset":           (*zman.Zmanim).SeaLevelSunset,
	"sunset":                   (*zman.Zmanim).Sunset,
	"beinHaShmashos":           (*zman.Zmanim).BeinHashmashos,
	"dusk":                     (*zman.Zmanim).Dusk,
	"tzaisBaalHatanya":         (*zman.Zmanim).TzaisBaalHatanya,
}

// timesOrder is the output ordering of the fixed/degree-based times.
var timesOrder = []string{
	"chatzotNight", "alosBaalHatanya", "alotHaShachar", "misheyakir",
	"misheyakirMachmir", "dawn", "sunrise", "seaLevelSunrise",
	"sofZmanShmaMGA19Point8", "sofZmanShmaMGA16Point1", "sofZmanShmaMGA",
	"sofZmanShmaBaalHatanya", "sofZmanShma", "sofZmanTfillaMGA19Point8",
	"sofZmanTfillaMGA16Point1", "sofZmanTfillaMGA", "sofZmanTfilaBaalHatanya",
	"sofZmanTfilla", "chatzot", "minchaGedola", "minchaGedolaBaalHatanya",
	"minchaGedolaMGA", "minchaKetana", "minchaKetanaBaalHatanya",
	"minchaKetanaMGA", "plagHaMincha", "plagHaminchaBaalHatanya",
	"seaLevelSunset", "sunset", "beinHaShmashos", "dusk", "tzaisBaalHatanya",
}

// seaLevelTimes are only reported when elevation is enabled (they are identical
// to sunrise/sunset otherwise).
var seaLevelTimes = map[string]bool{
	"seaLevelSunrise": true,
	"seaLevelSunset":  true,
}

// tzeitDeg maps degree-based tzeit names to their solar depression angle.
var tzeitDeg = map[string]float64{
	"tzeit7083deg": 7.083,
	"tzeit85deg":   8.5,
}

// tzeitMin maps fixed-minutes tzeit names to their offset after sunset.
var tzeitMin = map[string]int{
	"tzeit42min": 42,
	"tzeit50min": 50,
	"tzeit72min": 72,
}

// tzeitOrder is the output ordering of the tzeit times.
var tzeitOrder = []string{
	"tzeit7083deg", "tzeit85deg", "tzeit42min", "tzeit50min", "tzeit72min",
}

// allTimesOrder is the concatenation of timesOrder and tzeitOrder.
var allTimesOrder = append(append([]string{}, timesOrder...), tzeitOrder...)

// moladTime is a time computed from the molad. Each falls on a single civil
// date a month, so a date it does not fall on omits it instead of reporting
// null.
type moladTime struct {
	name string
	fn   func(z *zman.Zmanim) time.Time
	// round rounds to the minute. The earliest Kiddush Levana times round up
	// and the latest round down, so that rounding never widens the period in
	// which it may be said.
	round func(time.Time) time.Time
}

// moladTimes are the molad and Kiddush Levana times, in output order, after
// the tzeit times. They are reported as computed, even when they fall during
// the day, when Kiddush Levana is not said; alotHaShachar and tzeit85deg are
// in the same response for a consumer that wants to move them to the night.
var moladTimes = []moladTime{
	{"molad", (*zman.Zmanim).ZmanMolad, RoundTime},
	{"tchilasZmanKidushLevana3Days", func(z *zman.Zmanim) time.Time {
		return z.TchilasZmanKidushLevana3Days(time.Time{}, time.Time{})
	}, ceilMinute},
	{"tchilasZmanKidushLevana7Days", func(z *zman.Zmanim) time.Time {
		return z.TchilasZmanKidushLevana7Days(time.Time{}, time.Time{})
	}, ceilMinute},
	{"sofZmanKidushLevanaBetweenMoldos", func(z *zman.Zmanim) time.Time {
		return z.SofZmanKidushLevanaBetweenMoldos(time.Time{}, time.Time{})
	}, floorMinute},
	{"sofZmanKidushLevana15Days", func(z *zman.Zmanim) time.Time {
		return z.SofZmanKidushLevana15Days(time.Time{}, time.Time{})
	}, floorMinute},
}

// RoundTime discards seconds, rounding to the nearest minute (>= 30s rounds
// up).
func RoundTime(dt time.Time) time.Time {
	if dt.IsZero() {
		return dt
	}
	sec := dt.Second()
	ns := dt.Nanosecond()
	if sec == 0 && ns == 0 {
		return dt
	}
	if sec >= 30 {
		return dt.Add(time.Duration(60-sec)*time.Second - time.Duration(ns))
	}
	return dt.Add(-time.Duration(sec)*time.Second - time.Duration(ns))
}

// floorMinute discards seconds, rounding down to the minute.
func floorMinute(dt time.Time) time.Time {
	return dt.Add(-time.Duration(dt.Second())*time.Second - time.Duration(dt.Nanosecond()))
}

// ceilMinute rounds up to the next minute unless dt is already on one.
func ceilMinute(dt time.Time) time.Time {
	if dt.Second() == 0 && dt.Nanosecond() == 0 {
		return dt
	}
	return floorMinute(dt).Add(time.Minute)
}

// formatISOWithTimeZone renders a time as "2022-04-01T13:06:00-11:00", or nil
// (JSON null) for the zero time, which is what a time that does not occur
// (e.g. at polar latitudes) is reported as.
func formatISOWithTimeZone(dt time.Time) *string {
	if dt.IsZero() {
		return nil
	}
	s := dt.Format("2006-01-02T15:04:05-07:00")
	return &s
}

// forDate constructs a Zmanim calculator for the given calendar date.
func forDate(d model.GregDate, loc *geodb.Location, useElevation bool) zman.Zmanim {
	zloc := loc.ZmanimLocation()
	date := time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC)
	z := zman.New(&zloc, date)
	z.UseElevation = useElevation
	return z
}

// Times returns the halachic times for a single date as an ordered object
// of name -> ISO 8601 string (or null).
func Times(d model.GregDate, loc *geodb.Location, roundMinute, useElevation bool) jsutil.OrderedObj {
	z := forDate(d, loc, useElevation)
	out := make(jsutil.OrderedObj, 0, len(allTimesOrder))
	for _, name := range allTimesOrder {
		var dt time.Time
		switch {
		case seaLevelTimes[name] && !useElevation:
			continue
		case timeFuncs[name] != nil:
			dt = timeFuncs[name](&z)
		default:
			if angle, ok := tzeitDeg[name]; ok {
				dt = z.Tzeit(angle)
			} else if min, ok := tzeitMin[name]; ok {
				dt = z.SunsetOffset(min, roundMinute)
			} else {
				continue
			}
		}
		if roundMinute {
			dt = RoundTime(dt)
		}
		out = append(out, jsutil.KV{Key: name, Val: formatISOWithTimeZone(dt)})
	}
	for _, mt := range moladTimes {
		dt := mt.fn(&z)
		if dt.IsZero() {
			continue
		}
		if roundMinute {
			dt = mt.round(dt)
		}
		out = append(out, jsutil.KV{Key: mt.name, Val: formatISOWithTimeZone(dt)})
	}
	return out
}

// TimesForRange returns times for each date in [start, end] as an ordered
// object of name -> {isoDate -> value}. Ported from TimesForRange(). A molad
// time lists only the dates it falls on, so it can be an empty object.
func TimesForRange(start, end model.GregDate, loc *geodb.Location, roundMinute, useElevation bool) jsutil.OrderedObj {
	names := append([]string{}, allTimesOrder...)
	for _, mt := range moladTimes {
		names = append(names, mt.name)
	}
	inner := make(map[string]*jsutil.OrderedObj, len(names))
	out := make(jsutil.OrderedObj, 0, len(names))
	for _, name := range names {
		o := &jsutil.OrderedObj{}
		inner[name] = o
		out = append(out, jsutil.KV{Key: name, Val: o})
	}
	for rd := start.RD(); rd <= end.RD(); rd++ {
		d := model.GregFromRD(rd)
		iso := d.String()
		for _, kv := range Times(d, loc, roundMinute, useElevation) {
			o := inner[kv.Key]
			*o = append(*o, jsutil.KV{Key: iso, Val: kv.Val})
		}
	}
	return out
}

// IsAssurBemlacha reports whether melacha (work) is prohibited at the given
// moment for the location, backing the /zmanim im=1 branch.
func IsAssurBemlacha(dt time.Time, loc *geodb.Location, useElevation bool) (bool, error) {
	zloc := loc.ZmanimLocation()
	return hebcal.IsAssurBemlacha(dt, &zloc, loc.IsIsrael(), useElevation)
}
