package main

// autoCompleteNear is a Go port of @hebcal/geo-sqlite GeoDb.autoComplete,
// backing the /complete geographic typeahead. A leading digit routes to a ZIP-code
// lookup (exact 5-digit or numeric prefix); anything else runs a full-text
// search against both the geonames and US-ZIP FTS5 tables, merges and
// de-duplicates the two result sets, ranks them by a combined FTS5 bm25
// relevance + population score (optionally biased toward a nearby GeoIP
// location), and keeps the top 12.
//
// The FTS5 queries require the mattn/go-sqlite3 driver to be built with the
// sqlite_fts5 tag; the scoring SQL also calls ln(), which needs the
// sqlite_math_functions tag (SQLITE_ENABLE_MATH_FUNCTIONS). Both tags are set
// in the Makefile and .github/workflows/ci.yml.

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
)

// FTS5 bm25 column weights used to rank autocomplete matches, mirroring
// @hebcal/geo-sqlite. A match on the city name is weighted most heavily so that
// e.g. "Washington, D.C." outranks Seattle (whose admin1 is "Washington"). The
// country name gets a modest weight and admin1 the lowest, so region-name
// matches don't float to the top. The combined longname column keeps multi-word
// queries (e.g. "san fr") working across the city/admin1/country boundary.
const (
	ftsWeightCity     = 8.0
	ftsWeightCountry  = 2.0
	ftsWeightAdmin1   = 1.0
	ftsWeightLongname = 1.0
	// populationWeight scales the natural-log population term added to the bm25
	// relevance score. Larger values make population matter more relative to
	// how well the name matched.
	populationWeight = 1.5
)

// geonameMatchExpr scores a match on the separate city/admin1/country columns
// (so bm25 can weight them differently) while still matching the combined
// longname column so multi-word queries keep working. The (quote-escaped) query
// is substituted for each %s.
const geonameMatchExpr = `({city admin1 country} : "%s" * OR {longname} : "%s" *)`

// zipMatchExpr matches the USA ZIP fulltext table. ZIP matches are always plain
// city-name matches (no admin1/country ambiguity), so matching longname alone
// is sufficient.
const zipMatchExpr = `{longname} : "%s" *`

// zipCompleteSQL matches ZIP_COMPLETE_SQL (numeric prefix search).
const zipCompleteSQL = `SELECT ZipCode,CityMixedCase,State,Latitude,Longitude,TimeZone,DayLightSaving,Population
FROM ZIPCodes_Primary
WHERE ZipCode >= ? AND ZipCode < ?
ORDER BY Population DESC
LIMIT 12`

var (
	// geonameCompleteSQL matches @hebcal/geo-sqlite GEONAME_COMPLETE_SQL. The
	// bm25 weight arguments are positional over the geoname_fulltext columns:
	//   geonameid(0), longname(1), population(2), city(3), admin1(4), country(5)
	// bm25 returns smaller numbers for better matches, so it is negated and
	// added to the population term to form a "higher is better" score.
	geonameCompleteSQL = fmt.Sprintf(`SELECT geonameid, longname, city, admin1, country,
  -bm25(geoname_fulltext, 0.0, %[1]g, 0.0, %[2]g, %[3]g, %[4]g)
    + %[5]g * ln(CAST(population AS REAL) + 10) AS score
FROM geoname_fulltext
WHERE geoname_fulltext MATCH ?
ORDER BY score DESC
LIMIT 100`, ftsWeightLongname, ftsWeightCity, ftsWeightAdmin1, ftsWeightCountry, populationWeight)

	// zipFulltextCompleteSQL matches ZIP_FULLTEXT_COMPLETE_SQL. bm25 is
	// deliberately not used here: its scores are corpus-relative and therefore
	// not comparable to the geonames bm25 scores when the two result sets are
	// merged. Ranking ZIPs on a population-only score (on the same ln scale as
	// the geoname population term) preserves the "geonames take priority"
	// behavior.
	zipFulltextCompleteSQL = fmt.Sprintf(`SELECT ZipCode,
  %[1]g * ln(CAST(Population AS REAL) + 10) AS score
FROM ZIPCodes_CityFullText5
WHERE ZIPCodes_CityFullText5 MATCH ?
ORDER BY score DESC
LIMIT 20`, populationWeight)
)

// acItem is one autocomplete result before JSON serialization. The population
// is kept as the sort tiebreaker (and the sole ordering for the numeric ZIP
// paths) even when it is not emitted, and the isZip/numeric flags drive field
// ordering and the lat/long visibility rules applied by acItemToObj.
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
	// score is the combined FTS5 bm25 relevance + population term computed by
	// the fulltext SQL, used to rank merged results. It is intentionally not
	// serialized to JSON (acItemToObj never emits it).
	score float64
}

// autoCompleteNear generates typeahead results for qraw, biasing text-search
// results toward near (the caller's GeoIP location) when it is non-nil. A
// leading digit routes to a ZIP lookup: an exact 5-digit code resolves to a
// single result, while a 1-4 digit prefix scans the half-open range
// [zipA, zipB). Otherwise it runs the merged geonames+ZIP full-text search,
// sorts, and returns the top 12.
func (db *GeoDB) autoCompleteNear(qraw string, near *geoIPPoint) []acItem {
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
	// Full-text search of both geonames and US ZIP cities (when query 3+ chars).
	// Geonames match the weighted city/admin1/country columns (plus longname
	// for multi-word queries); ZIPs match longname alone.
	q := strings.ReplaceAll(qraw, `"`, `""`)
	geoMatches := db.geonameFulltextComplete(fmt.Sprintf(geonameMatchExpr, q, q))
	var values []acItem
	if len(qraw) > 2 {
		zipMatches := db.zipFulltextComplete(fmt.Sprintf(zipMatchExpr, q))
		values = mergeZipGeo(zipMatches, geoMatches)
	} else {
		values = geoMatches
	}
	sortAutocomplete(values, near)
	if len(values) > 12 {
		values = values[:12]
	}
	return values
}

// sortAutocomplete stably orders values best-first. When near is non-nil, items
// are ranked by their GeoIP-biased score (see autocompleteScore); otherwise by
// the base fulltext score. Population is the final tiebreaker (and the sole
// ordering for numeric ZIP paths, which carry no fulltext score).
func sortAutocomplete(values []acItem, near *geoIPPoint) {
	sort.SliceStable(values, func(i, j int) bool {
		if near != nil {
			si := autocompleteScore(values[i], near)
			sj := autocompleteScore(values[j], near)
			if si != sj {
				return si > sj
			}
		} else if values[i].score != values[j].score {
			return values[i].score > values[j].score
		}
		// Population is the final tiebreaker (and the sole ordering for the
		// numeric ZIP paths, which carry no fulltext score).
		return values[i].population > values[j].population
	})
}

// autocompleteScore biases the base fulltext score (bm25 relevance + a
// population term) toward locations near the caller's GeoIP point by applying a
// distance penalty on top of it.
func autocompleteScore(item acItem, near *geoIPPoint) float64 {
	distanceKm := haversineKm(near.Latitude, near.Longitude, item.latitude, item.longitude)
	// Since very small distances can overly dominate the score and
	// IP geocoding isn't super precise, we treat small distances as
	// equivalent to 40km away
	distanceKm = math.Max(distanceKm, 40)
	return item.score - 1.25*math.Log(distanceKm)
}

// toRad converts degrees to radians.
func toRad(deg float64) float64 { return deg * math.Pi / 180 }

// haversineKm returns the great-circle distance in kilometers between two
// points given in decimal degrees, using the haversine formula.
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0088
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	rLat1 := toRad(lat1)
	rLat2 := toRad(lat2)
	sdLat := math.Sin(dLat / 2)
	sdLon := math.Sin(dLon / 2)
	a := sdLat*sdLat + math.Cos(rLat1)*math.Cos(rLat2)*sdLon*sdLon
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// geonameFulltextComplete runs the geonames FTS query, de-duplicates by
// GeoNames id (preserving the SQL's score-descending order, where score is the
// combined bm25 relevance + population term), and resolves each hit to a full
// location.
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
		var score float64
		if err := rows.Scan(&geonameid, &longname, &city, &admin1, &country, &score); err != nil {
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
		item := db.geonameLocToAutocomplete(geonameid, loc, city.String, country.String)
		item.score = score
		out = append(out, item)
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
		var score float64
		if err := rows.Scan(&zip, &score); err != nil {
			continue
		}
		loc := db.lookupZip(zip)
		if loc == nil {
			continue
		}
		item := zipLocToAutocomplete(loc, false)
		item.score = score
		out = append(out, item)
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
	for _, obj := range geoMatches {
		key := obj.asciiname + "|" + obj.admin1 + "|" + obj.cc
		if _, ok := m[key]; !ok {
			order = append(order, key)
		}
		m[key] = obj
	}
	for _, obj := range zipMatches {
		// ZIP admin1 is a state abbreviation; map it to the full name so the
		// key lines up with the geoname admin1.
		key := obj.asciiname + "|" + stateNames[obj.admin1] + "|" + obj.cc
		if _, ok := m[key]; !ok {
			order = append(order, key)
			m[key] = obj
		}
	}
	out := make([]acItem, 0, len(order))
	for _, k := range order {
		out = append(out, m[k])
	}
	// Establish a deterministic base order by descending relevance score (with
	// population as a tiebreaker) before the caller applies any GeoIP bias.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].population > out[j].population
	})
	return out
}
