package main

// zmanimHandler implements the /zmanim JSON API, a Go port of the getZmanim
// function in hebcal-web src/zmanim.js. It returns halachic times for a single
// date or a date range, and (with im=1) an "is work prohibited" status.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"
)

const cacheControl30Days = "public, max-age=2592000, s-maxage=2592000"

// apiVersion identifies the hebcal-go library and this service in API
// responses, mirroring hebcal-web's `${coreVersion}-${pkgVersion}`.
var apiVersion = func() string {
	coreVer, appVer := "unknown", "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" {
			appVer = info.Main.Version
		}
		for _, dep := range info.Deps {
			if dep.Path == "github.com/hebcal/hebcal-go" {
				coreVer = dep.Version
			}
		}
	}
	return coreVer + "-" + appVer
}()

// ---------------------------------------------------------------------------
// Ordered JSON helpers: the zmanim response preserves the ALL_TIMES ordering,
// which a Go map cannot, so times are emitted through an ordered object.
// ---------------------------------------------------------------------------

type jsonKV struct {
	Key string
	Val interface{}
}

// orderedObj marshals to a JSON object preserving insertion order.
type orderedObj []jsonKV

func (o orderedObj) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(kv.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		v, err := json.Marshal(kv.Val)
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Zman name tables, ported from the TIMES / TZEIT_TIMES objects in zmanim.js.
// timeFuncs covers the fixed and degree-based times; tzeitDeg and tzeitMin
// cover the tzeit variants (degree-based vs fixed-minutes after sunset).
// ---------------------------------------------------------------------------

var timeFuncs = map[string]func(z *zmanim.Zmanim) time.Time{
	"chatzotNight":             (*zmanim.Zmanim).ChatzotNight,
	"alosBaalHatanya":          (*zmanim.Zmanim).AlosBaalHatanya,
	"alotHaShachar":            (*zmanim.Zmanim).AlotHaShachar,
	"misheyakir":               (*zmanim.Zmanim).Misheyakir,
	"misheyakirMachmir":        (*zmanim.Zmanim).MisheyakirMachmir,
	"dawn":                     (*zmanim.Zmanim).Dawn,
	"sunrise":                  (*zmanim.Zmanim).Sunrise,
	"seaLevelSunrise":          (*zmanim.Zmanim).SeaLevelSunrise,
	"sofZmanShmaMGA19Point8":   (*zmanim.Zmanim).SofZmanShmaMGA19Point8,
	"sofZmanShmaMGA16Point1":   (*zmanim.Zmanim).SofZmanShmaMGA16Point1,
	"sofZmanShmaMGA":           (*zmanim.Zmanim).SofZmanShmaMGA,
	"sofZmanShmaBaalHatanya":   (*zmanim.Zmanim).SofZmanShmaBaalHatanya,
	"sofZmanShma":              (*zmanim.Zmanim).SofZmanShma,
	"sofZmanTfillaMGA19Point8": (*zmanim.Zmanim).SofZmanTfillaMGA19Point8,
	"sofZmanTfillaMGA16Point1": (*zmanim.Zmanim).SofZmanTfillaMGA16Point1,
	"sofZmanTfillaMGA":         (*zmanim.Zmanim).SofZmanTfillaMGA,
	"sofZmanTfilaBaalHatanya":  (*zmanim.Zmanim).SofZmanTfilaBaalHatanya,
	"sofZmanTfilla":            (*zmanim.Zmanim).SofZmanTfilla,
	"chatzot":                  (*zmanim.Zmanim).Chatzot,
	"minchaGedola":             (*zmanim.Zmanim).MinchaGedola,
	"minchaGedolaBaalHatanya":  (*zmanim.Zmanim).MinchaGedolaBaalHatanya,
	"minchaGedolaMGA":          (*zmanim.Zmanim).MinchaGedolaMGA,
	"minchaKetana":             (*zmanim.Zmanim).MinchaKetana,
	"minchaKetanaBaalHatanya":  (*zmanim.Zmanim).MinchaKetanaBaalHatanya,
	"minchaKetanaMGA":          (*zmanim.Zmanim).MinchaKetanaMGA,
	"plagHaMincha":             (*zmanim.Zmanim).PlagHaMincha,
	"plagHaminchaBaalHatanya":  (*zmanim.Zmanim).PlagHaminchaBaalHatanya,
	"seaLevelSunset":           (*zmanim.Zmanim).SeaLevelSunset,
	"sunset":                   (*zmanim.Zmanim).Sunset,
	"beinHaShmashos":           (*zmanim.Zmanim).BeinHashmashos,
	"dusk":                     (*zmanim.Zmanim).Dusk,
	"tzaisBaalHatanya":         (*zmanim.Zmanim).TzaisBaalHatanya,
}

// timesOrder is the ordering of the fixed/degree-based times (TIMES in JS).
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
// to sunrise/sunset otherwise). Matches the seaLevel* handling in getTimes().
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

// tzeitOrder is the ordering of the tzeit times (TZEIT_TIMES in JS).
var tzeitOrder = []string{
	"tzeit7083deg", "tzeit85deg", "tzeit42min", "tzeit50min", "tzeit72min",
}

// allTimesOrder is the concatenation of timesOrder and tzeitOrder (ALL_TIMES).
var allTimesOrder = append(append([]string{}, timesOrder...), tzeitOrder...)

// roundTime discards seconds, rounding to the nearest minute (>= 30s rounds
// up), matching @hebcal/core Zmanim.roundTime.
func roundTime(dt time.Time) time.Time {
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

// formatISOWithTimeZone renders a time as "2022-04-01T13:06:00-11:00", or nil
// (JSON null) for the zero time, matching zmanim.js which emits null when a
// time does not occur (e.g. polar latitudes).
func formatISOWithTimeZone(dt time.Time) *string {
	if dt.IsZero() {
		return nil
	}
	s := dt.Format("2006-01-02T15:04:05-07:00")
	return &s
}

// zmanimForDate constructs a Zmanim calculator for the given calendar date.
func zmanimForDate(d gregDate, loc *geoLocation, useElevation bool) zmanim.Zmanim {
	zloc := loc.zmanimLocation()
	date := time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC)
	z := zmanim.New(&zloc, date)
	z.UseElevation = useElevation
	return z
}

// getTimes returns the halachic times for a single date as an ordered object
// of name -> ISO 8601 string (or null). Ported from getTimes() in zmanim.js.
func getTimes(d gregDate, loc *geoLocation, roundMinute, useElevation bool) orderedObj {
	z := zmanimForDate(d, loc, useElevation)
	out := make(orderedObj, 0, len(allTimesOrder))
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
			dt = roundTime(dt)
		}
		out = append(out, jsonKV{name, formatISOWithTimeZone(dt)})
	}
	return out
}

// getTimesForRange returns times for each date in [start, end] as an ordered
// object of name -> {isoDate -> value}. Ported from getTimesForRange().
func getTimesForRange(start, end gregDate, loc *geoLocation, roundMinute, useElevation bool) orderedObj {
	inner := make(map[string]*orderedObj, len(allTimesOrder))
	out := make(orderedObj, 0, len(allTimesOrder))
	for _, name := range allTimesOrder {
		o := &orderedObj{}
		inner[name] = o
		out = append(out, jsonKV{name, o})
	}
	for rd := start.RD(); rd <= end.RD(); rd++ {
		d := gregFromRD(rd)
		iso := d.String()
		for _, kv := range getTimes(d, loc, roundMinute, useElevation) {
			o := inner[kv.Key]
			*o = append(*o, jsonKV{iso, kv.Val})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// locationToPlainObj: the ordered "location" object, ported from
// @hebcal/rest-api locationToPlainObj. Fields are omitted when empty, and
// elevation is only present when elevation is enabled.
// ---------------------------------------------------------------------------

func locationToPlainObj(loc *geoLocation, useElevation bool) orderedObj {
	o := orderedObj{
		{"title", loc.Name},
		{"city", loc.shortName()},
		{"tzid", loc.TimeZoneID},
		{"latitude", loc.Latitude},
		{"longitude", loc.Longitude},
	}
	if loc.CC != "" {
		o = append(o, jsonKV{"cc", loc.CC})
		if loc.Country != "" {
			o = append(o, jsonKV{"country", loc.Country})
		}
	}
	// LOC_FIELDS order: elevation, admin1, asciiname, geo, zip, state,
	// stateName, geonameid (each omitted when falsy)
	if useElevation && loc.Elevation > 0 {
		o = append(o, jsonKV{"elevation", loc.Elevation})
	}
	if loc.Admin1 != "" {
		o = append(o, jsonKV{"admin1", loc.Admin1})
	}
	if loc.Asciiname != "" {
		o = append(o, jsonKV{"asciiname", loc.Asciiname})
	}
	if loc.Geo != "" {
		o = append(o, jsonKV{"geo", loc.Geo})
	}
	if loc.Zip != "" {
		o = append(o, jsonKV{"zip", loc.Zip})
	}
	if loc.State != "" {
		o = append(o, jsonKV{"state", loc.State})
	}
	if loc.StateName != "" {
		o = append(o, jsonKV{"stateName", loc.StateName})
	}
	if loc.GeonameID != 0 {
		o = append(o, jsonKV{"geonameid", loc.GeonameID})
	}
	return o
}

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

// zmanimHandler implements GET /zmanim (cfg=json only).
func (app *appServer) zmanimHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	setCORS(w)
	switch r.Method {
	case http.MethodOptions:
		corsPreflight(w, "GET")
		return
	case http.MethodGet, http.MethodHead:
		// handled below
	default:
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		http.Error(w, fmt.Sprintf("Method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	if app.db == nil {
		app.writeZmanimError(w, &httpError{status: http.StatusServiceUnavailable,
			message: "Location database is not available"})
		return
	}
	if q.Get("cfg") != "json" {
		app.writeZmanimError(w, badRequest("Parameter cfg=json is required"))
		return
	}
	loc, err := getLocationFromQuery(app.db, q)
	if err != nil {
		app.writeZmanimError(w, err)
		return
	}
	if loc == nil {
		app.writeZmanimError(w, badRequest("Location is required"))
		return
	}
	useElevation := q.Get("ue") == "on" || q.Get("ue") == "1"
	locObj := locationToPlainObj(loc, useElevation)

	if q.Get("im") == "on" || q.Get("im") == "1" {
		app.checkMelacha(w, r, q, loc, locObj, useElevation)
		return
	}

	isRange, startD, endD, err := getStartAndEnd(q, loc.TimeZoneID)
	if err != nil {
		app.writeZmanimError(w, err)
		return
	}
	if isRange || !empty(q, "date") {
		w.Header().Set("Cache-Control", cacheControl30Days)
		etag := makeETag(r, "")
		w.Header().Set("ETag", etag)
		if checkFresh(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	} else {
		setZmanimExpires(w, loc.TimeZoneID)
	}
	roundMinute := q.Get("sec") != "1"

	var body orderedObj
	if isRange {
		times := getTimesForRange(startD, endD, loc, roundMinute, useElevation)
		body = orderedObj{
			{"date", orderedObj{{"start", startD.String()}, {"end", endD.String()}}},
			{"version", apiVersion},
			{"location", locObj},
			{"times", times},
		}
	} else {
		times := getTimes(startD, loc, roundMinute, useElevation)
		body = orderedObj{
			{"date", startD.String()},
			{"version", apiVersion},
			{"location", locObj},
			{"times", times},
		}
	}
	w.Write(jsonMarshal(body))
}

var reHasTZOffset = regexp.MustCompile(`[+-]\d\d:\d\d$`)

// parseMelachaDate emulates the JavaScript `new Date(dateStr)` (plus the
// location-offset fixup) used by checkMelacha: a trailing Z is UTC, an
// explicit ±HH:MM offset is honored, a bare YYYY-MM-DD is UTC midnight, and a
// datetime without a zone is interpreted as wall-clock time in the location.
func parseMelachaDate(dateStr string, tz *time.Location) (time.Time, bool) {
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

// checkMelacha implements the im=1 branch: reports whether melacha (work) is
// prohibited at a given moment. Ported from checkMelacha() in zmanim.js.
func (app *appServer) checkMelacha(w http.ResponseWriter, r *http.Request, q url.Values,
	loc *geoLocation, locObj orderedObj, useElevation bool) {
	now := time.Now()
	w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
	tz, err := zmanim.LoadLocation(loc.TimeZoneID)
	if err != nil {
		app.writeZmanimError(w, badRequest("Invalid time zone specified: %s", loc.TimeZoneID))
		return
	}
	var dt time.Time
	dateStr := strings.TrimSpace(q.Get("dt"))
	if dateStr != "" && reIsoDate.MatchString(dateStr) {
		parsed, ok := parseMelachaDate(dateStr, tz)
		if !ok {
			app.writeZmanimError(w, badRequest("Invalid Date: %s", dateStr))
			return
		}
		dt = parsed
	} else {
		dt = now
		w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=60")
	}
	dt = dt.In(tz)
	zloc := loc.zmanimLocation()
	ok, err := hebcal.IsAssurBemlacha(dt, &zloc, loc.isIsrael(), useElevation)
	if err != nil {
		app.writeZmanimError(w, badRequest("%s", err.Error()))
		return
	}
	status := orderedObj{
		{"localTime", dt.Format("2006-01-02T15:04:05-07:00")},
		{"isAssurBemlacha", ok},
	}
	body := orderedObj{
		{"date", now.UTC().Format("2006-01-02T15:04:05.000Z")},
		{"version", apiVersion},
		{"location", locObj},
		{"status", status},
	}
	w.Write(jsonMarshal(body))
}

// nowInTimezone returns today's calendar date in the given timezone.
func nowInTimezone(tzid string) gregDate {
	loc, err := zmanim.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	y, m, d := time.Now().In(loc).Date()
	return gregDate{Year: y, Month: m, Day: d}
}

// getStartAndEnd resolves the start, end, and date query parameters to a date
// or date range, ported from getStartAndEnd() in hebcal-web src/dateUtil.js.
func getStartAndEnd(q url.Values, tzid string) (isRange bool, startD, endD gregDate, err error) {
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
		if startD, err = isoDateStringToDate(start); err != nil {
			return false, gregDate{}, gregDate{}, err
		}
		if endD, err = isoDateStringToDate(end); err != nil {
			return false, gregDate{}, gregDate{}, err
		}
		if endD.RD() < startD.RD() {
			return false, startD, startD, nil
		}
		if endD.RD()-startD.RD() > maxRangeDays {
			endD = gregFromRD(startD.RD() + maxRangeDays)
		}
		return true, startD, endD, nil
	}
	var single gregDate
	if date == "" {
		single = nowInTimezone(tzid)
	} else if single, err = isoDateStringToDate(date); err != nil {
		return false, gregDate{}, gregDate{}, err
	}
	return false, single, single, nil
}

// setZmanimExpires sets Expires to tomorrow at midnight in the location's
// timezone (and Last-Modified to now), matching expires() in zmanim.js.
func setZmanimExpires(w http.ResponseWriter, tzid string) {
	loc, err := zmanim.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
	tomorrow := now.AddDate(0, 0, 1)
	exp := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, loc)
	w.Header().Set("Expires", exp.UTC().Format(http.TimeFormat))
}

// writeZmanimError emits a JSON error response with the status from the error.
func (app *appServer) writeZmanimError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if herr, ok := err.(*httpError); ok {
		status = herr.status
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	w.Write(jsonMarshal(map[string]string{"error": err.Error()}))
}
