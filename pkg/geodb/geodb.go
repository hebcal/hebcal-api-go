// Package geodb reads the pre-built geonames.sqlite3 and zips.sqlite3
// databases and resolves the documented ways of specifying a location for the
// Hebcal calendar APIs (GeoNames id, US ZIP code, and the legacy city
// identifier), plus the geographic typeahead behind /complete. It is a Go port
// of the @hebcal/geo-sqlite GeoDb class.
//
// The package depends only on the SQLite driver and hebcal-go, never on the
// rest of this service, so it can be reused (or split out) on its own.
//
// The autocomplete queries require the mattn/go-sqlite3 driver to be built
// with the sqlite_fts5 and sqlite_math_functions tags; see the Makefile.
package geodb

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

// DB wraps the geonames and zips SQLite databases with prepared statements
// and small LRU caches (mirroring the @hebcal/geo-sqlite QuickLRU sizes).
type DB struct {
	geonamesDB      *sql.DB
	zipsDB          *sql.DB
	geonameStmt     *sql.Stmt
	zipStmt         *sql.Stmt
	geonameCompStmt *sql.Stmt // geoname full-text autocomplete (FTS5)
	zipCompStmt     *sql.Stmt // ZIP prefix autocomplete
	zipFulltextStmt *sql.Stmt // ZIP city full-text autocomplete (FTS5)
	geonameCache    *lru.Cache[int, *Location]
	zipCache        *lru.Cache[string, *Location]
	legacyCities    map[string]int
	countryNames    map[string]string // ISO country code -> full country name
}

// New opens the zips and geonames databases read-only and prepares the
// per-id lookup statements. The caller must Close the returned DB.
func New(zipsPath, geonamesPath string) (*DB, error) {
	zipsDB, err := sql.Open("sqlite3", "file:"+zipsPath+"?mode=ro&immutable=1")
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", zipsPath, err)
	}
	geonamesDB, err := sql.Open("sqlite3", "file:"+geonamesPath+"?mode=ro&immutable=1")
	if err != nil {
		zipsDB.Close()
		return nil, fmt.Errorf("opening %s: %w", geonamesPath, err)
	}
	db := &DB{geonamesDB: geonamesDB, zipsDB: zipsDB}
	if db.geonameStmt, err = geonamesDB.Prepare(geonameSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing geoname query: %w", err)
	}
	if db.zipStmt, err = zipsDB.Prepare(zipSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing zip query: %w", err)
	}
	if db.geonameCompStmt, err = geonamesDB.Prepare(geonameCompleteSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing geoname autocomplete query: %w", err)
	}
	if db.zipCompStmt, err = zipsDB.Prepare(zipCompleteSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing zip prefix autocomplete query: %w", err)
	}
	if db.zipFulltextStmt, err = zipsDB.Prepare(zipFulltextCompleteSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing zip full-text autocomplete query: %w", err)
	}
	db.geonameCache, _ = lru.New[int, *Location](750)
	db.zipCache, _ = lru.New[string, *Location](150)

	var raw map[string]int
	if err := json.Unmarshal(city2geonameidJSON, &raw); err != nil {
		db.Close()
		return nil, fmt.Errorf("parsing city2geonameid.json: %w", err)
	}
	db.legacyCities = make(map[string]int, len(raw))
	for name, id := range raw {
		db.legacyCities[munge(name)] = id
	}

	if err := db.loadCountryNames(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// loadCountryNames reads the ISO country code -> full name mapping from the
// geonames "country" table. This is the same table joined by geonameSQL, so the
// names returned in the API's "location" object stay consistent with the city
// display names.
func (db *DB) loadCountryNames() error {
	rows, err := db.geonamesDB.Query("SELECT ISO, Country FROM country WHERE Country <> ''")
	if err != nil {
		return fmt.Errorf("querying country names: %w", err)
	}
	defer rows.Close()
	db.countryNames = make(map[string]string, 256)
	for rows.Next() {
		var iso, country string
		if err := rows.Scan(&iso, &country); err != nil {
			return fmt.Errorf("scanning country names: %w", err)
		}
		db.countryNames[iso] = country
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading country names: %w", err)
	}
	return nil
}

// Close releases the prepared statements and database handles.
func (db *DB) Close() error {
	if db.geonameStmt != nil {
		db.geonameStmt.Close()
	}
	if db.zipStmt != nil {
		db.zipStmt.Close()
	}
	if db.geonameCompStmt != nil {
		db.geonameCompStmt.Close()
	}
	if db.zipCompStmt != nil {
		db.zipCompStmt.Close()
	}
	if db.zipFulltextStmt != nil {
		db.zipFulltextStmt.Close()
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

// LookupGeoname returns the location for a GeoNames numeric id, or nil if it is
// not present.
func (db *DB) LookupGeoname(geonameid int) *Location {
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
	loc := &Location{
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

// LookupZip returns the location for a 5-digit US ZIP code, or nil if it is not
// present.
func (db *DB) LookupZip(zip string) *Location {
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
	loc := &Location{
		// hebcal-web's ZIP Location carries no asciiname, so we omit it too.
		Name:       fmt.Sprintf("%s, %s %s", city, state, zip5),
		CC:         "US",
		Country:    "United States",
		Admin1:     state,
		Latitude:   latitude,
		Longitude:  longitude,
		Elevation:  elev,
		TimeZoneID: UsaTzid(state, tzNum, dst),
		Geo:        "zip",
		Zip:        zip5,
		State:      state,
		StateName:  StateNames[state],
	}
	if population.Valid {
		loc.Population = int(population.Int64)
	}
	db.zipCache.Add(zip5, loc)
	return loc
}

// LookupLegacyCity resolves one of the ~480 legacy Hebcal city identifiers (e.g.
// "GB-London") to a location, falling back to the built-in classic city table.
func (db *DB) LookupLegacyCity(cityName string) *Location {
	if id, ok := db.legacyCities[munge(cityName)]; ok {
		return db.LookupGeoname(id)
	}
	if classic := zmanim.LookupCity(cityName); classic != nil {
		return &Location{
			Name:       classic.Name,
			Asciiname:  classic.Name,
			CC:         classic.CountryCode,
			Country:    db.countryNames[classic.CountryCode],
			Latitude:   classic.Latitude,
			Longitude:  classic.Longitude,
			Elevation:  classic.Elevation,
			TimeZoneID: classic.TimeZoneId,
			Geo:        "geoname",
		}
	}
	return nil
}

// CountryName returns the full country name for an ISO country code, or the
// empty string when the code is unknown.
func (db *DB) CountryName(cc string) string {
	return db.countryNames[cc]
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
