package geodb

import (
	"strings"

	"github.com/hebcal/hebcal-go/zmanim"
)

// Location is a resolved location, carrying both the fields needed for the
// solar/zmanim calculation and the descriptive fields returned in the API's
// "location" object.
type Location struct {
	Name       string // full display name, e.g. "Jerusalem, Israel"
	Asciiname  string
	CC         string // ISO country code, e.g. "IL"
	Country    string // full country name, e.g. "Israel"
	Admin1     string // first-level administrative division
	Latitude   float64
	Longitude  float64
	Elevation  int    // meters above sea level (0 if unknown)
	TimeZoneID string // IANA tz identifier
	Geo        string // "geoname", "zip" or "pos"
	GeonameID  int
	Zip        string
	State      string // US state abbreviation
	StateName  string
	Population int
	IL         bool // Israel holiday schedule, for geo=pos locations without a CC
}

// ShortName returns the city portion of the location name (the text before the
// first comma), with a special case for US "City, DC" style names.
func (g *Location) ShortName() string {
	name := g.Name
	comma := strings.Index(name, ", ")
	if comma == -1 {
		return name
	}
	if g.CC == "US" && comma+2 < len(name) && name[comma+2] == 'D' {
		if comma+3 < len(name) && name[comma+3] == 'C' {
			return name[:comma+4]
		}
		if comma+4 < len(name) && name[comma+3] == '.' && name[comma+4] == 'C' {
			return name[:comma+6]
		}
	}
	return name[:comma]
}

// IsIsrael reports whether the location uses the Israel holiday schedule.
func (g *Location) IsIsrael() bool {
	return g.CC == "IL" || g.IL
}

// ZmanimLocation adapts the Location to the hebcal-go zmanim.Location used by
// the solar calculators.
func (g *Location) ZmanimLocation() zmanim.Location {
	elev := g.Elevation
	if elev < 0 {
		elev = 0
	}
	return zmanim.Location{
		Name:        g.Name,
		CountryCode: g.CC,
		Latitude:    g.Latitude,
		Longitude:   g.Longitude,
		Elevation:   elev,
		TimeZoneId:  g.TimeZoneID,
	}
}

// Is5DigitZip reports whether the (trimmed) string begins with five ASCII
// digits, matching @hebcal/geo-sqlite GeoDb.is5DigitZip.
func Is5DigitZip(s string) bool {
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
