// Package geodb reads the pre-built geonames.sqlite3 and zips.sqlite3
// databases and resolves the documented ways of specifying a location for the
// Hebcal calendar APIs (GeoNames id, US ZIP code, and the legacy city
// identifier), plus the geographic typeahead behind /complete.
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
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
// and small LRU caches.
//
// The geonames database can be replaced while the server runs (see
// ReloadGeonames): everything read from it -- the handle, its statements, its
// cache and the country names -- lives in one immutable geonames value behind
// an atomic pointer, so a request works against a single generation from start
// to finish and never sees a cache filled from the old file beside a statement
// on the new one.
type DB struct {
	geonamesPath    string
	geo             atomic.Pointer[geonames]
	reloadMu        sync.Mutex // serializes ReloadGeonames
	zipsDB          *sql.DB
	zipStmt         *sql.Stmt
	zipCompStmt     *sql.Stmt // ZIP prefix autocomplete
	zipFulltextStmt *sql.Stmt // ZIP city full-text autocomplete (FTS5)
	zipCache        *lru.Cache[string, *Location]
	legacyCities    map[string]int
}

// geonames is one opened generation of geonames.sqlite3.
type geonames struct {
	db           *sql.DB
	stmt         *sql.Stmt
	compStmt     *sql.Stmt // geoname full-text autocomplete (FTS5)
	cache        *lru.Cache[int, *Location]
	countryNames map[string]string // ISO country code -> full country name
	info         os.FileInfo       // the file as it was when opened
}

// retireDelay is how long a replaced geonames generation stays open. A request
// that loaded the old pointer just before the swap finishes against it; 30s
// is the server's WriteTimeout, so nothing still holds it after this.
const retireDelay = time.Minute

// New opens the zips and geonames databases read-only and prepares the
// per-id lookup statements. The caller must Close the returned DB.
func New(zipsPath, geonamesPath string) (*DB, error) {
	zipsDB, err := sql.Open("sqlite3", "file:"+zipsPath+"?mode=ro&immutable=1")
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", zipsPath, err)
	}
	db := &DB{geonamesPath: geonamesPath, zipsDB: zipsDB}
	g, err := openGeonames(geonamesPath)
	if err != nil {
		db.Close()
		return nil, err
	}
	db.geo.Store(g)
	if db.zipStmt, err = zipsDB.Prepare(zipSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing zip query: %w", err)
	}
	if db.zipCompStmt, err = zipsDB.Prepare(zipCompleteSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing zip prefix autocomplete query: %w", err)
	}
	if db.zipFulltextStmt, err = zipsDB.Prepare(zipFulltextCompleteSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing zip full-text autocomplete query: %w", err)
	}
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
	return db, nil
}

// openGeonames opens path read-only, prepares its statements and loads the
// country names. The file is stat'ed first, so a file replaced between the
// stat and the open is merely noticed again on the next check.
func openGeonames(path string) (*geonames, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	g := &geonames{db: sdb, info: info}
	if g.stmt, err = sdb.Prepare(geonameSQL); err != nil {
		g.close()
		return nil, fmt.Errorf("preparing geoname query: %w", err)
	}
	if g.compStmt, err = sdb.Prepare(geonameCompleteSQL); err != nil {
		g.close()
		return nil, fmt.Errorf("preparing geoname autocomplete query: %w", err)
	}
	g.cache, _ = lru.New[int, *Location](750)
	if err := g.loadCountryNames(); err != nil {
		g.close()
		return nil, err
	}
	return g, nil
}

// loadCountryNames reads the ISO country code -> full name mapping from the
// geonames "country" table. This is the same table joined by geonameSQL, so the
// names returned in the API's "location" object stay consistent with the city
// display names. An empty table is an error: it means a truncated or wrong
// file, which must not replace a working one.
func (g *geonames) loadCountryNames() error {
	rows, err := g.db.Query("SELECT ISO, Country FROM country WHERE Country <> ''")
	if err != nil {
		return fmt.Errorf("querying country names: %w", err)
	}
	defer rows.Close()
	g.countryNames = make(map[string]string, 256)
	for rows.Next() {
		var iso, country string
		if err := rows.Scan(&iso, &country); err != nil {
			return fmt.Errorf("scanning country names: %w", err)
		}
		g.countryNames[iso] = country
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading country names: %w", err)
	}
	if len(g.countryNames) == 0 {
		return fmt.Errorf("geonames database has no country names")
	}
	return nil
}

func (g *geonames) close() error {
	if g.stmt != nil {
		g.stmt.Close()
	}
	if g.compStmt != nil {
		g.compStmt.Close()
	}
	return g.db.Close()
}

// ReloadGeonames reopens the geonames database if the file at its path has
// been replaced since it was opened -- a different inode (the atomic rename a
// rebuild ends with), a different modification time, or a different size --
// and reports whether it did. Requests in flight finish against the old
// generation, which is closed a minute later.
//
// A file that cannot be opened, prepared or read leaves the current
// generation serving and returns the error; the next call tries again.
func (db *DB) ReloadGeonames() (bool, error) {
	db.reloadMu.Lock()
	defer db.reloadMu.Unlock()
	old := db.geo.Load()
	info, err := os.Stat(db.geonamesPath)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", db.geonamesPath, err)
	}
	if os.SameFile(old.info, info) && info.ModTime().Equal(old.info.ModTime()) &&
		info.Size() == old.info.Size() {
		return false, nil
	}
	g, err := openGeonames(db.geonamesPath)
	if err != nil {
		return false, err
	}
	db.geo.Store(g)
	time.AfterFunc(retireDelay, func() { old.close() })
	return true, nil
}

// Close releases the prepared statements and database handles.
func (db *DB) Close() error {
	if db.zipStmt != nil {
		db.zipStmt.Close()
	}
	if db.zipCompStmt != nil {
		db.zipCompStmt.Close()
	}
	if db.zipFulltextStmt != nil {
		db.zipFulltextStmt.Close()
	}
	var err error
	if g := db.geo.Load(); g != nil {
		err = g.close()
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
	return db.geo.Load().lookup(geonameid)
}

func (g *geonames) lookup(geonameid int) *Location {
	if geonameid == 0 {
		return nil
	}
	if geonameid == 293396 { // legacy alias fixup
		geonameid = 293397
	}
	if loc, ok := g.cache.Get(geonameid); ok {
		return loc
	}
	var name, asciiname, cc, timezone string
	var country, admin1 sql.NullString
	var latitude, longitude float64
	var population, gtopo30 sql.NullInt64
	err := g.stmt.QueryRow(geonameid).Scan(&name, &asciiname, &cc, &country,
		&admin1, &latitude, &longitude, &population, &gtopo30, &timezone)
	if err != nil {
		g.cache.Add(geonameid, nil)
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
	g.cache.Add(geonameid, loc)
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
		// A ZIP Location carries no asciiname.
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
			Country:    db.CountryName(classic.CountryCode),
			Latitude:   classic.Latitude,
			Longitude:  classic.Longitude,
			Elevation:  classic.Elevation,
			TimeZoneID: classic.TimeZoneID,
			Geo:        "geoname",
		}
	}
	return nil
}

// CountryName returns the full country name for an ISO country code, or the
// empty string when the code is unknown.
func (db *DB) CountryName(cc string) string {
	return db.geo.Load().countryNames[cc]
}

// geonameCityDescr builds a display name from geonames components.
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

// foldAccents removes diacritical marks (São -> Sao), used for the
// city-description dedup.
func foldAccents(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}
