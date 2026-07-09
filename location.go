package main

// getLocationFromQuery resolves an HTTP request's query parameters to a
// geoLocation, supporting the four documented ways to specify a location for
// the Hebcal APIs. It is a Go port of the getLocationFromQuery function in
// hebcal-web src/location.js, limited to the four query-based methods (the
// GeoIP and legacy ladeg/lamin degree-minute forms are out of scope here).

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/hebcal/hebcal-go/zmanim"
)

// notFound is like badRequest but carries a 404 status.
func notFound(format string, args ...interface{}) *httpError {
	return &httpError{status: 404, message: fmt.Sprintf(format, args...)}
}

// is5DigitZip reports whether the (trimmed) string begins with five ASCII
// digits, matching @hebcal/geo-sqlite GeoDb.is5DigitZip.
func is5DigitZip(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 5 {
		return false
	}
	for i := 0; i < 5; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

var reTrailingZip = regexp.MustCompile(` \d\d\d\d\d$`)
var reGmtOffset = regexp.MustCompile(`^([ +-])(\d\d):00$`)

// getLocationFromQuery resolves the request location. It returns (nil, nil)
// when no location parameters are present (the caller decides whether that is
// an error), or a non-nil *httpError for malformed or unresolvable input.
func getLocationFromQuery(db *GeoDB, q url.Values) (*geoLocation, error) {
	cityTypeahead := strings.TrimSpace(q.Get("city-typeahead"))
	if is5DigitZip(cityTypeahead) {
		q.Set("zip", cityTypeahead)
	} else if cityTypeahead != "" && empty(q, "zip") && reTrailingZip.MatchString(cityTypeahead) {
		q.Set("zip", cityTypeahead[len(cityTypeahead)-5:])
	}

	switch {
	case !empty(q, "geonameid"):
		geonameid, ok := parseInt(q.Get("geonameid"))
		if !ok {
			return nil, notFound("Sorry, can't find geonameid: %s", q.Get("geonameid"))
		}
		loc := db.lookupGeoname(geonameid)
		if loc == nil {
			return nil, notFound("Sorry, can't find geonameid: %s", q.Get("geonameid"))
		}
		return loc, nil

	case !empty(q, "zip"):
		zip := q.Get("zip")
		if !is5DigitZip(zip) {
			return nil, badRequest("Sorry, invalid ZIP code: %s", zip)
		}
		zip = strings.TrimSpace(zip)[:5]
		loc := db.lookupZip(zip)
		if loc == nil {
			return nil, notFound("Sorry, can't find ZIP code: %s", q.Get("zip"))
		}
		return loc, nil

	case !empty(q, "city"):
		loc := db.lookupLegacyCity(strings.TrimSpace(q.Get("city")))
		if loc == nil {
			return nil, notFound("Invalid legacy city specified: %s", q.Get("city"))
		}
		return loc, nil

	case !empty(q, "latitude") && !empty(q, "longitude"):
		return locationFromLatLong(q, cityTypeahead)

	case hasLatLongLegacy(q):
		return locationFromLatLongLegacy(q, cityTypeahead)
	}
	return nil, nil
}

// geoposLegacy lists the legacy degree/minute parameters and their maximum
// values, matching hebcal-web src/urlArgs.js.
var geoposLegacy = []struct {
	key string
	max int
}{
	{"ladeg", 90}, {"lamin", 60}, {"lodeg", 180}, {"lomin", 60},
}

// hasLatLongLegacy reports whether all of the legacy degree/minute/direction
// parameters are present.
func hasLatLongLegacy(q url.Values) bool {
	if empty(q, "ladir") || empty(q, "lodir") {
		return false
	}
	for _, g := range geoposLegacy {
		if empty(q, g.key) {
			return false
		}
	}
	return true
}

// locationFromLatLongLegacy builds a geo=pos location from the legacy
// ladeg/lamin/ladir + lodeg/lomin/lodir degree-minute-direction form, ported
// from the hasLatLongLegacy branch of getLocationFromQuery in location.js.
// Unlike the decimal form, west/south are expressed as positive magnitudes
// with a direction letter rather than negative numbers.
func locationFromLatLongLegacy(q url.Values, cityTypeahead string) (*geoLocation, error) {
	for _, g := range geoposLegacy {
		v := q.Get(g.key)
		if n, ok := parseInt(v); !ok || n > g.max {
			return nil, badRequest("Sorry, %s=%s out of valid range 0-%d", g.key, v, g.max)
		}
	}
	ladeg, _ := parseInt(q.Get("ladeg"))
	lamin, _ := parseInt(q.Get("lamin"))
	lodeg, _ := parseInt(q.Get("lodeg"))
	lomin, _ := parseInt(q.Get("lomin"))
	latitude := float64(ladeg) + float64(lamin)/60
	longitude := float64(lodeg) + float64(lomin)/60
	if q.Get("ladir") == "s" {
		latitude = -latitude
	}
	if q.Get("lodir") == "w" {
		longitude = -longitude
	}
	tzid := q.Get("tzid")
	if tzid == "" && !empty(q, "tz") && !empty(q, "dst") {
		tzid = legacyTzToTzid(q.Get("tz"), q.Get("dst"))
	}
	if tzid == "" {
		// hebcal-web falls back to a geo-tz shape lookup here; that data is
		// not available to this service, so a timezone is required.
		return nil, badRequest("Timezone required")
	}
	if _, err := zmanim.LoadLocation(tzid); err != nil {
		return nil, badRequest("Invalid time zone specified: %s", tzid)
	}
	il := q.Get("i") == "on"
	if tzid == "Asia/Jerusalem" {
		il = true
	}
	cityName := cityTypeahead
	if cityName == "" {
		cityName = makeGeoCityName(latitude, longitude, tzid)
	}
	return &geoLocation{
		Name:       cityName,
		Latitude:   latitude,
		Longitude:  longitude,
		TimeZoneID: tzid,
		Geo:        "pos",
		IL:         il,
	}, nil
}

// legacyTzToTzid resolves a legacy numeric timezone plus DST rule to an IANA
// tzid, ported from @hebcal/core Location.legacyTzToTzid. It returns "" when
// the combination is unrecognized. Note the reversed Etc/GMT sign convention
// (tz=-5 becomes "Etc/GMT-5", i.e. UTC+5).
func legacyTzToTzid(tz, dst string) string {
	tzNum, _ := parseInt(tz)
	switch {
	case dst == "none":
		if tzNum == 0 {
			return "UTC"
		}
		plus := ""
		if tzNum > 0 {
			plus = "+"
		}
		return fmt.Sprintf("Etc/GMT%s%d", plus, tzNum)
	case tzNum == 2 && dst == "israel":
		return "Asia/Jerusalem"
	case dst == "eu":
		switch tzNum {
		case -2:
			return "Atlantic/Cape_Verde"
		case -1:
			return "Atlantic/Azores"
		case 0:
			return "Europe/London"
		case 1:
			return "Europe/Paris"
		case 2:
			return "Europe/Athens"
		}
	case dst == "usa":
		return zipcodesTzMap[tzNum*-1]
	}
	return ""
}

// locationFromLatLong builds a geo=pos location from latitude/longitude/tzid.
func locationFromLatLong(q url.Values, cityTypeahead string) (*geoLocation, error) {
	latitude, err := strconv.ParseFloat(q.Get("latitude"), 64)
	if err != nil || math.IsNaN(latitude) || latitude > 90 || latitude < -90 {
		return nil, badRequest("Invalid latitude specified: %s", q.Get("latitude"))
	}
	longitude, err := strconv.ParseFloat(q.Get("longitude"), 64)
	if err != nil || math.IsNaN(longitude) || longitude > 180 || longitude < -180 {
		return nil, badRequest("Invalid longitude specified: %s", q.Get("longitude"))
	}
	if empty(q, "tzid") {
		// hebcal-web guesses the timezone from geo-tz shape data here; that
		// dataset is not available to this service, so a timezone is required.
		return nil, badRequest("Timezone required")
	}
	il := q.Get("i") == "on"
	tzid := q.Get("tzid")
	if tzid == "Asia/Jerusalem" {
		il = true
	} else if tz0 := tzid[0]; tz0 == ' ' || tz0 == '-' || tz0 == '+' {
		// hack for clients who pass +03:00 or -02:00 ("+" url-decodes to " ")
		if m := reGmtOffset.FindStringSubmatch(tzid); m != nil {
			dir := "+"
			if m[1] == "-" {
				dir = "-"
			}
			n, _ := parseInt(m[2])
			tzid = fmt.Sprintf("Etc/GMT%s%d", dir, n)
		}
	}
	if _, err := zmanim.LoadLocation(tzid); err != nil {
		return nil, badRequest("Invalid time zone specified: %s", q.Get("tzid"))
	}
	cityName := cityTypeahead
	if cityName == "" {
		cityName = makeGeoCityName(latitude, longitude, tzid)
	}
	elevation := 0
	if elev, err := strconv.ParseFloat(q.Get("elev"), 64); err == nil && elev > 0 {
		elevation = int(elev)
	}
	return &geoLocation{
		Name:       cityName,
		Latitude:   latitude,
		Longitude:  longitude,
		Elevation:  elevation,
		TimeZoneID: tzid,
		Geo:        "pos",
		IL:         il,
	}, nil
}

// makeGeoCityName formats a latitude/longitude/tzid as a human-readable name
// like "37°25′N 122°5′W America/Los_Angeles", matching hebcal-web.
func makeGeoCityName(latitude, longitude float64, tzid string) string {
	ladir := "N"
	if latitude < 0 {
		ladir = "S"
	}
	ladeg := int(math.Floor(latitude))
	if latitude < 0 {
		ladeg = int(math.Ceil(latitude)) * -1
	}
	lamin := int(math.Floor(60 * (math.Abs(latitude) - float64(ladeg))))
	lodir := "E"
	if longitude < 0 {
		lodir = "W"
	}
	lodeg := int(math.Floor(longitude))
	if longitude < 0 {
		lodeg = int(math.Ceil(longitude)) * -1
	}
	lomin := int(math.Floor(60 * (math.Abs(longitude) - float64(lodeg))))
	return fmt.Sprintf("%d°%d′%s %d°%d′%s %s",
		ladeg, lamin, ladir, lodeg, lomin, lodir, tzid)
}
