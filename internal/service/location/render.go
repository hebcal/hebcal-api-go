package location

import (
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// ToGeoJSON serializes a Location exactly as Koa serializes the @hebcal/core
// Location returned by hebcal-web's /geo route. The key order and presence
// rules reproduce the class field declaration order (latitude, longitude,
// locationName, timeZoneId, elevation, il, cc, geoid, admin1, stateName, geo,
// zip, population, asciiname) followed by any dynamically-added properties
// (geonameid for geoname lookups, state for zip lookups).
//
// This shape is deliberately not the trimmed "location" object ToPlainObj
// builds for /zmanim and /shabbat. The two are not interchangeable.
func ToGeoJSON(loc *geodb.Location) jsutil.OrderedObj {
	o := jsutil.OrderedObj{
		{Key: "latitude", Val: loc.Latitude},
		{Key: "longitude", Val: loc.Longitude},
		{Key: "locationName", Val: loc.Name},
		{Key: "timeZoneId", Val: loc.TimeZoneID},
		{Key: "elevation", Val: loc.Elevation}, // always present, even when 0
		{Key: "il", Val: loc.IsIsrael()},
	}
	if loc.CC != "" {
		o = append(o, jsutil.KV{Key: "cc", Val: loc.CC})
	}
	// geoid is the 7th Location constructor argument: the numeric GeoNames id
	// for geoname lookups, but the ZIP string for zip lookups.
	if loc.Geo == "zip" && loc.Zip != "" {
		o = append(o, jsutil.KV{Key: "geoid", Val: loc.Zip})
	} else if loc.GeonameID != 0 {
		o = append(o, jsutil.KV{Key: "geoid", Val: loc.GeonameID})
	}
	if loc.Admin1 != "" {
		o = append(o, jsutil.KV{Key: "admin1", Val: loc.Admin1})
	}
	if loc.StateName != "" {
		o = append(o, jsutil.KV{Key: "stateName", Val: loc.StateName})
	}
	o = append(o, jsutil.KV{Key: "geo", Val: loc.Geo})
	if loc.Zip != "" {
		o = append(o, jsutil.KV{Key: "zip", Val: loc.Zip})
	}
	if loc.Population != 0 {
		o = append(o, jsutil.KV{Key: "population", Val: loc.Population})
	}
	if loc.Asciiname != "" {
		o = append(o, jsutil.KV{Key: "asciiname", Val: loc.Asciiname})
	}
	// geonameid is set as a separate own property (in addition to geoid) by
	// @hebcal/geo-sqlite's makeGeonameLocation, so it sorts after asciiname.
	if loc.Geo == "geoname" && loc.GeonameID != 0 {
		o = append(o, jsutil.KV{Key: "geonameid", Val: loc.GeonameID})
	}
	// state is a dynamically-added own property on ZIP Locations, emitted last.
	if loc.State != "" {
		o = append(o, jsutil.KV{Key: "state", Val: loc.State})
	}
	return o
}

// ToPlainObj builds the ordered "location" object embedded in the /zmanim and
// /shabbat responses, ported from @hebcal/rest-api locationToPlainObj. Fields
// are omitted when empty, and elevation is only present when elevation is
// enabled.
func ToPlainObj(loc *geodb.Location, useElevation bool) jsutil.OrderedObj {
	o := jsutil.OrderedObj{
		{Key: "title", Val: loc.Name},
		{Key: "city", Val: loc.ShortName()},
		{Key: "tzid", Val: loc.TimeZoneID},
		{Key: "latitude", Val: loc.Latitude},
		{Key: "longitude", Val: loc.Longitude},
	}
	if loc.CC != "" {
		o = append(o, jsutil.KV{Key: "cc", Val: loc.CC})
		if loc.Country != "" {
			o = append(o, jsutil.KV{Key: "country", Val: loc.Country})
		}
	}
	// LOC_FIELDS order: elevation, admin1, asciiname, geo, zip, state,
	// stateName, geonameid (each omitted when falsy)
	if useElevation && loc.Elevation > 0 {
		o = append(o, jsutil.KV{Key: "elevation", Val: loc.Elevation})
	}
	if loc.Admin1 != "" {
		o = append(o, jsutil.KV{Key: "admin1", Val: loc.Admin1})
	}
	if loc.Asciiname != "" {
		o = append(o, jsutil.KV{Key: "asciiname", Val: loc.Asciiname})
	}
	if loc.Geo != "" {
		o = append(o, jsutil.KV{Key: "geo", Val: loc.Geo})
	}
	if loc.Zip != "" {
		o = append(o, jsutil.KV{Key: "zip", Val: loc.Zip})
	}
	if loc.State != "" {
		o = append(o, jsutil.KV{Key: "state", Val: loc.State})
	}
	if loc.StateName != "" {
		o = append(o, jsutil.KV{Key: "stateName", Val: loc.StateName})
	}
	if loc.GeonameID != 0 {
		o = append(o, jsutil.KV{Key: "geonameid", Val: loc.GeonameID})
	}
	return o
}
