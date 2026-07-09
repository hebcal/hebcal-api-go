package main

// autoComplete is a Go port of @hebcal/geo-sqlite GeoDb.autoComplete, backing
// the /complete geographic typeahead. A leading digit routes to a ZIP-code
// lookup (exact 5-digit or numeric prefix); anything else runs a full-text
// search against both the geonames and US-ZIP FTS5 tables, merges and
// de-duplicates the two result sets, sorts by population, and keeps the top 12.
//
// The FTS5 queries require the mattn/go-sqlite3 driver to be built with the
// sqlite_fts5 tag (see Makefile and .github/workflows/ci.yml).

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

const (
	// geonameCompleteSQL matches @hebcal/geo-sqlite GEONAME_COMPLETE_SQL.
	geonameCompleteSQL = `SELECT geonameid, longname, city, admin1, country
FROM geoname_fulltext
WHERE geoname_fulltext MATCH ?
ORDER BY population DESC
LIMIT 20`
	// zipCompleteSQL matches ZIP_COMPLETE_SQL (numeric prefix search).
	zipCompleteSQL = `SELECT ZipCode,CityMixedCase,State,Latitude,Longitude,TimeZone,DayLightSaving,Population
FROM ZIPCodes_Primary
WHERE ZipCode >= ? AND ZipCode < ?
ORDER BY Population DESC
LIMIT 10`
	// zipFulltextCompleteSQL matches ZIP_FULLTEXT_COMPLETE_SQL.
	zipFulltextCompleteSQL = `SELECT ZipCode
FROM ZIPCodes_CityFullText5
WHERE ZIPCodes_CityFullText5 MATCH ?
ORDER BY Population DESC
LIMIT 20`
)

// acItem is one autocomplete result before JSON serialization. The population
// is kept for the population-descending sort even when it is not emitted, and
// the isZip/numeric flags drive field ordering and the lat/long visibility
// rules applied by acItemToObj.
type acItem struct {
	id         interface{} // int GeoNames id, or ZIP string
	value      string
	admin1     string
	asciiname  string
	country    string
	cc         string
	latitude   float64
	longitude  float64
	timezone   string
	population int
	geo        string // "geoname" or "zip"
	name       string // geoname only: the FTS "city" when it differs from asciiname
	isZip      bool   // controls field order (zip layout vs geoname layout)
	numeric    bool   // numeric ZIP path: latitude/longitude/timezone are always kept
}

// autoComplete generates typeahead results for qraw. When latlong is false the
// caller strips latitude/longitude/timezone/population from text-search results
// (see acItemToObj); numeric ZIP results always retain their coordinates.
func (db *GeoDB) autoComplete(qraw string, latlong bool) []acItem {
	qraw = strings.TrimSpace(qraw)
	if qraw == "" {
		return nil
	}
	if c := qraw[0]; c >= '0' && c <= '9' {
		// A leading digit means a ZIP code query rather than a full-table scan.
		if is5DigitZip(qraw) {
			loc := db.lookupZip(qraw)
			if loc == nil {
				return nil
			}
			return []acItem{zipLocToAutocomplete(loc, true)}
		}
		// 1-4 digit ZIP prefix: search the half-open range [zipA, zipB).
		zipA := qraw
		if len(zipA) > 5 {
			zipA = zipA[:5]
		}
		zipB := "A"
		if zipA != "9" {
			n, _ := parseInt(zipA)
			zipB = fmt.Sprintf("%0*d", len(zipA), n+1)
		}
		return db.zipPrefixComplete(zipA, zipB)
	}
	// Full-text search of both geonames and US ZIP cities.
	match := `{longname} : "` + strings.ReplaceAll(qraw, `"`, `""`) + `" *`
	geoMatches := db.geonameFulltextComplete(match)
	zipMatches := db.zipFulltextComplete(match)
	values := mergeZipGeo(zipMatches, geoMatches)
	sort.SliceStable(values, func(i, j int) bool {
		return values[i].population > values[j].population
	})
	if len(values) > 12 {
		values = values[:12]
	}
	return values
}

// geonameFulltextComplete runs the geonames FTS query, de-duplicates by
// GeoNames id (preserving population-descending order), and resolves each hit
// to a full location.
func (db *GeoDB) geonameFulltextComplete(match string) []acItem {
	rows, err := db.geonameCompStmt.Query(match)
	if err != nil {
		return nil
	}
	defer rows.Close()
	seen := make(map[int]bool)
	var out []acItem
	for rows.Next() {
		var geonameid int
		var longname, city, admin1, country sql.NullString
		if err := rows.Scan(&geonameid, &longname, &city, &admin1, &country); err != nil {
			continue
		}
		if seen[geonameid] {
			continue
		}
		seen[geonameid] = true
		loc := db.lookupGeoname(geonameid)
		if loc == nil {
			continue
		}
		out = append(out, db.geonameLocToAutocomplete(geonameid, loc, city.String, country.String))
	}
	return out
}

// geonameLocToAutocomplete matches @hebcal/geo-sqlite geonameLocToAutocomplete.
// The FTS row supplies the id, the "city" used for the optional name override,
// and the country fallback; everything else comes from the resolved location.
func (db *GeoDB) geonameLocToAutocomplete(geonameid int, loc *geoLocation, resCity, resCountry string) acItem {
	country := resCountry
	if country == "" {
		country = db.countryNames[loc.CC]
	}
	it := acItem{
		id:         geonameid,
		value:      loc.Name,
		admin1:     loc.Admin1,
		country:    country,
		cc:         loc.CC,
		latitude:   loc.Latitude,
		longitude:  loc.Longitude,
		timezone:   loc.TimeZoneID,
		population: loc.Population,
		geo:        "geoname",
	}
	if resCity != loc.Asciiname {
		it.name = resCity
	}
	it.asciiname = loc.Asciiname
	return it
}

// zipFulltextComplete runs the ZIP-city FTS query and resolves each ZIP hit.
func (db *GeoDB) zipFulltextComplete(match string) []acItem {
	rows, err := db.zipFulltextStmt.Query(match)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []acItem
	for rows.Next() {
		var zip string
		if err := rows.Scan(&zip); err != nil {
			continue
		}
		loc := db.lookupZip(zip)
		if loc == nil {
			continue
		}
		out = append(out, zipLocToAutocomplete(loc, false))
	}
	return out
}

// zipLocToAutocomplete matches @hebcal/geo-sqlite zipLocToAutocomplete.
func zipLocToAutocomplete(loc *geoLocation, numeric bool) acItem {
	return acItem{
		id:         loc.Zip,
		value:      loc.Name,
		admin1:     loc.Admin1,
		asciiname:  loc.shortName(),
		country:    "United States",
		cc:         "US",
		latitude:   loc.Latitude,
		longitude:  loc.Longitude,
		timezone:   loc.TimeZoneID,
		population: loc.Population,
		geo:        "zip",
		isZip:      true,
		numeric:    numeric,
	}
}

// zipPrefixComplete runs the numeric ZIP-prefix query and builds a result per
// row, matching @hebcal/geo-sqlite zipResultToObj. ZIP_COMPLETE_SQL does not
// select the Elevation column, so (like the JS) no elevation field is emitted.
func (db *GeoDB) zipPrefixComplete(zipA, zipB string) []acItem {
	rows, err := db.zipCompStmt.Query(zipA, zipB)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []acItem
	for rows.Next() {
		var zip, city, state, tz, dst string
		var latitude, longitude float64
		var population sql.NullInt64
		if err := rows.Scan(&zip, &city, &state, &latitude, &longitude, &tz, &dst, &population); err != nil {
			continue
		}
		tzNum, _ := parseInt(tz)
		it := acItem{
			id:        zip,
			value:     fmt.Sprintf("%s, %s %s", city, state, zip),
			admin1:    state,
			asciiname: city,
			country:   "United States",
			cc:        "US",
			latitude:  latitude,
			longitude: longitude,
			timezone:  getUsaTzid(state, tzNum, dst),
			geo:       "zip",
			isZip:     true,
			numeric:   true,
		}
		if population.Valid {
			it.population = int(population.Int64)
		}
		out = append(out, it)
	}
	return out
}

// mergeZipGeo merges ZIP and geoname matches, matching @hebcal/geo-sqlite
// mergeZipGeo: GeoNames matches take priority over US ZIP matches for the same
// city, and insertion order is preserved (a geoname overwrites a ZIP in place).
func mergeZipGeo(zipMatches, geoMatches []acItem) []acItem {
	if len(zipMatches) > 0 && len(geoMatches) == 0 {
		return zipMatches
	}
	if len(geoMatches) > 0 && len(zipMatches) == 0 {
		return geoMatches
	}
	order := make([]string, 0, len(zipMatches)+len(geoMatches))
	m := make(map[string]acItem, len(zipMatches)+len(geoMatches))
	for _, obj := range zipMatches {
		// ZIP admin1 is a state abbreviation; map it to the full name so the
		// key lines up with the geoname admin1.
		key := obj.asciiname + "|" + stateNames[obj.admin1] + "|" + obj.cc
		if _, ok := m[key]; !ok {
			order = append(order, key)
			m[key] = obj
		}
	}
	for _, obj := range geoMatches {
		key := obj.asciiname + "|" + obj.admin1 + "|" + obj.cc
		if _, ok := m[key]; !ok {
			order = append(order, key)
		}
		m[key] = obj
	}
	out := make([]acItem, 0, len(order))
	for _, k := range order {
		out = append(out, m[k])
	}
	return out
}
