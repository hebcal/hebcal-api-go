package main

// GeoDB reads the pre-built geonames.sqlite3 and zips.sqlite3 databases and
// resolves the four documented ways of specifying a location for the Hebcal
// calendar APIs (GeoNames id, US ZIP code, latitude/longitude, and the legacy
// city identifier). It is a Go port of the @hebcal/geo-sqlite GeoDb class.

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	lru "github.com/hashicorp/golang-lru/v2"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/hebcal/hebcal-go/zmanim"
)

//go:embed city2geonameid.json
var city2geonameidJSON []byte

// geoLocation is a resolved location, carrying both the fields needed for the
// solar/zmanim calculation and the descriptive fields returned in the API's
// "location" object.
type geoLocation struct {
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

// shortName returns the city portion of the location name (the text before the
// first comma), with a special case for US "City, DC" style names.
func (g *geoLocation) shortName() string {
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

// isIsrael reports whether the location uses the Israel holiday schedule.
func (g *geoLocation) isIsrael() bool {
	return g.CC == "IL" || g.IL
}

// zmanimLocation adapts the geoLocation to the hebcal-go zmanim.Location used by
// the solar calculators.
func (g *geoLocation) zmanimLocation() zmanim.Location {
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

const (
	geonameSQL = `SELECT g.name, g.asciiname, g.country, c.Country, a.asciiname,
  g.latitude, g.longitude, g.population, g.gtopo30, g.timezone
FROM geoname g
LEFT JOIN country c ON g.country = c.ISO
LEFT JOIN admin1 a ON g.country||'.'||g.admin1 = a.key
WHERE g.geonameid = ?`
	zipSQL = `SELECT ZipCode, CityMixedCase, State, Latitude, Longitude, Elevation,
TimeZone, DayLightSaving, Population
FROM ZIPCodes_Primary WHERE ZipCode = ?`
)

// GeoDB wraps the geonames and zips SQLite databases with prepared statements
// and small LRU caches (mirroring the @hebcal/geo-sqlite QuickLRU sizes).
type GeoDB struct {
	geonamesDB   *sql.DB
	zipsDB       *sql.DB
	geonameStmt  *sql.Stmt
	zipStmt      *sql.Stmt
	geonameCache *lru.Cache[int, *geoLocation]
	zipCache     *lru.Cache[string, *geoLocation]
	legacyCities map[string]int
}

// NewGeoDB opens the zips and geonames databases read-only and prepares the
// per-id lookup statements. The caller must Close the returned GeoDB.
func NewGeoDB(zipsPath, geonamesPath string) (*GeoDB, error) {
	zipsDB, err := sql.Open("sqlite3", "file:"+zipsPath+"?mode=ro&immutable=1")
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", zipsPath, err)
	}
	geonamesDB, err := sql.Open("sqlite3", "file:"+geonamesPath+"?mode=ro&immutable=1")
	if err != nil {
		zipsDB.Close()
		return nil, fmt.Errorf("opening %s: %w", geonamesPath, err)
	}
	db := &GeoDB{geonamesDB: geonamesDB, zipsDB: zipsDB}
	if db.geonameStmt, err = geonamesDB.Prepare(geonameSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing geoname query: %w", err)
	}
	if db.zipStmt, err = zipsDB.Prepare(zipSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing zip query: %w", err)
	}
	db.geonameCache, _ = lru.New[int, *geoLocation](750)
	db.zipCache, _ = lru.New[string, *geoLocation](150)

	var raw map[string]int
	if err := json.Unmarshal(city2geonameidJSON, &raw); err != nil {
		db.Close()
		return nil, fmt.Errorf("parsing city2geonameid.json: %w", err)
	}
	db.legacyCities = make(map[string]int, len(raw))
	for name, id := range raw {
		db.legacyCities[munge(name)] = id
	}
	return db, nil
}

// Close releases the prepared statements and database handles.
func (db *GeoDB) Close() error {
	if db.geonameStmt != nil {
		db.geonameStmt.Close()
	}
	if db.zipStmt != nil {
		db.zipStmt.Close()
	}
	var err error
	if db.geonamesDB != nil {
		err = db.geonamesDB.Close()
	}
	if db.zipsDB != nil {
		if e := db.zipsDB.Close(); err == nil {
			err = e
		}
	}
	return err
}

// lookupGeoname returns the location for a GeoNames numeric id, or nil if it is
// not present.
func (db *GeoDB) lookupGeoname(geonameid int) *geoLocation {
	if geonameid == 0 {
		return nil
	}
	if geonameid == 293396 { // legacy alias fixup, matching @hebcal/geo-sqlite
		geonameid = 293397
	}
	if loc, ok := db.geonameCache.Get(geonameid); ok {
		return loc
	}
	var name, asciiname, cc, timezone string
	var country, admin1 sql.NullString
	var latitude, longitude float64
	var population, gtopo30 sql.NullInt64
	err := db.geonameStmt.QueryRow(geonameid).Scan(&name, &asciiname, &cc, &country,
		&admin1, &latitude, &longitude, &population, &gtopo30, &timezone)
	if err != nil {
		db.geonameCache.Add(geonameid, nil)
		return nil
	}
	elevation := 0
	if gtopo30.Valid && gtopo30.Int64 > 0 {
		elevation = int(gtopo30.Int64)
	}
	countryName := country.String
	admin1Name := admin1.String
	loc := &geoLocation{
		Name:       geonameCityDescr(name, admin1Name, countryName),
		Asciiname:  asciiname,
		CC:         cc,
		Country:    countryName,
		Admin1:     admin1Name,
		Latitude:   latitude,
		Longitude:  longitude,
		Elevation:  elevation,
		TimeZoneID: timezone,
		Geo:        "geoname",
		GeonameID:  geonameid,
	}
	if population.Valid {
		loc.Population = int(population.Int64)
	}
	db.geonameCache.Add(geonameid, loc)
	return loc
}

// lookupZip returns the location for a 5-digit US ZIP code, or nil if it is not
// present.
func (db *GeoDB) lookupZip(zip string) *geoLocation {
	zip5 := zip
	if len(zip5) > 5 {
		zip5 = zip5[:5]
	}
	if loc, ok := db.zipCache.Get(zip5); ok {
		return loc
	}
	var city, state, tz, dst string
	var latitude, longitude float64
	var elevation, population sql.NullInt64
	err := db.zipStmt.QueryRow(zip5).Scan(&zip5, &city, &state, &latitude, &longitude,
		&elevation, &tz, &dst, &population)
	if err != nil {
		db.zipCache.Add(zip5, nil)
		return nil
	}
	tzNum, _ := parseInt(tz)
	elev := 0
	if elevation.Valid && elevation.Int64 > 0 {
		elev = int(elevation.Int64)
	}
	loc := &geoLocation{
		// hebcal-web's ZIP Location carries no asciiname, so we omit it too.
		Name:       fmt.Sprintf("%s, %s %s", city, state, zip5),
		CC:         "US",
		Country:    "United States",
		Admin1:     state,
		Latitude:   latitude,
		Longitude:  longitude,
		Elevation:  elev,
		TimeZoneID: getUsaTzid(state, tzNum, dst),
		Geo:        "zip",
		Zip:        zip5,
		State:      state,
		StateName:  stateNames[state],
	}
	if population.Valid {
		loc.Population = int(population.Int64)
	}
	db.zipCache.Add(zip5, loc)
	return loc
}

// lookupLegacyCity resolves one of the ~480 legacy Hebcal city identifiers (e.g.
// "GB-London") to a location, falling back to the built-in classic city table.
func (db *GeoDB) lookupLegacyCity(cityName string) *geoLocation {
	if id, ok := db.legacyCities[munge(cityName)]; ok {
		return db.lookupGeoname(id)
	}
	if classic := zmanim.LookupCity(cityName); classic != nil {
		return &geoLocation{
			Name:       classic.Name,
			Asciiname:  classic.Name,
			CC:         classic.CountryCode,
			Country:    countryNames[classic.CountryCode],
			Latitude:   classic.Latitude,
			Longitude:  classic.Longitude,
			Elevation:  classic.Elevation,
			TimeZoneID: classic.TimeZoneId,
			Geo:        "geoname",
		}
	}
	return nil
}

// geonameCityDescr builds a display name from geonames components, matching
// @hebcal/geo-sqlite's GeoDb.geonameCityDescr.
func geonameCityDescr(cityName, admin1, countryName string) string {
	switch countryName {
	case "United States":
		countryName = "USA"
	case "United Kingdom":
		countryName = "UK"
	}
	cityDescr := cityName
	if countryName != "Israel" && admin1 != "" && !strings.Contains(admin1, cityName) {
		tlitCity := foldAccents(cityName)
		tlitAdmin1 := foldAccents(admin1)
		if !strings.Contains(tlitAdmin1, tlitCity) {
			cityDescr += ", " + admin1
		}
	}
	if countryName != "" {
		cityDescr += ", " + countryName
	}
	return cityDescr
}

// munge normalizes a city name for legacy lookups: lowercase, and strip
// apostrophes, spaces and plus signs.
func munge(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "+", "")
	return s
}

// foldAccents removes diacritical marks (São -> Sao), approximating the
// transliteration used by @hebcal/geo-sqlite for the city-description dedup.
func foldAccents(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}

// zipcodesTzMap maps the numeric timezone column of the ZIP database to an IANA
// tz identifier, matching @hebcal/core Location.ZIPCODES_TZ_MAP.
var zipcodesTzMap = map[int]string{
	0:  "UTC",
	4:  "America/Puerto_Rico",
	5:  "America/New_York",
	6:  "America/Chicago",
	7:  "America/Denver",
	8:  "America/Los_Angeles",
	9:  "America/Anchorage",
	10: "Pacific/Honolulu",
	11: "Pacific/Pago_Pago",
	13: "Pacific/Funafuti",
	14: "Pacific/Guam",
	15: "Pacific/Palau",
	16: "Pacific/Chuuk",
}

// getUsaTzid resolves a US state + numeric timezone + DST flag to an IANA tz
// identifier, matching @hebcal/core Location.getUsaTzid.
func getUsaTzid(state string, tz int, dst string) string {
	if tz == 10 && state == "AK" {
		return "America/Adak"
	}
	if tz == 7 && state == "AZ" {
		if dst == "Y" {
			return "America/Denver"
		}
		return "America/Phoenix"
	}
	return zipcodesTzMap[tz]
}
