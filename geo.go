package main

// The /geo endpoint resolves an HTTP request's query parameters to a location
// and returns the raw @hebcal/core Location object as JSON, matching the
// hebcal-web src/router.js "/geo" route:
//
//	ctx.response.type = ctx.request.header['accept'] = 'application/json';
//	ctx.body = getLocationFromQuery(ctx.db, ctx.request.query);
//
// The JSON shape here deliberately mirrors how Koa serializes an @hebcal/core
// Location (field names locationName/timeZoneId/geoid, an always-present
// elevation, plus il/population/geonameid) rather than the trimmed "location"
// object used by /zmanim and /shabbat (locationToPlainObj). The two are not
// interchangeable.

import (
	"fmt"
	"net/http"
)

// geoHandler implements GET/HEAD /geo. It returns the resolved location as
// JSON, 204 No Content when no location parameters are supplied (matching Koa
// setting ctx.body = null), or a JSON error for malformed/unresolvable input.
func (app *appServer) geoHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if r.Method == http.MethodOptions {
		corsPreflight(w, "GET")
		return
	}
	// hebcal-web only sets CORS headers when a cfg parameter is present; the
	// /geo route is normally called without one.
	if q.Get("cfg") != "" {
		setCORS(w)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, fmt.Sprintf("Method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	if app.db == nil {
		app.writeGeoError(w, &httpError{status: http.StatusServiceUnavailable,
			message: "Location database is not available"})
		return
	}
	loc, err := getLocationFromQuery(app.db, q)
	if err != nil {
		app.writeGeoError(w, err)
		return
	}
	if loc == nil {
		// No location parameters: hebcal-web assigns ctx.body = null, which Koa
		// turns into a 204 No Content with no body.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.Write(jsonMarshal(locationToGeoJSON(loc)))
}

// writeGeoError emits a JSON error object with the httpError's status.
func (app *appServer) writeGeoError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if herr, ok := err.(*httpError); ok {
		status = herr.status
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	w.Write(jsonMarshal(map[string]string{"error": err.Error()}))
}

// locationToGeoJSON serializes a geoLocation exactly as Koa serializes the
// @hebcal/core Location returned by /geo. The key order and presence rules
// reproduce the class field declaration order (latitude, longitude,
// locationName, timeZoneId, elevation, il, cc, geoid, admin1, stateName, geo,
// zip, population, asciiname) followed by any dynamically-added properties
// (geonameid for geoname lookups, state for zip lookups).
func locationToGeoJSON(loc *geoLocation) orderedObj {
	o := orderedObj{
		{"latitude", loc.Latitude},
		{"longitude", loc.Longitude},
		{"locationName", loc.Name},
		{"timeZoneId", loc.TimeZoneID},
		{"elevation", loc.Elevation}, // always present, even when 0
		{"il", loc.isIsrael()},
	}
	if loc.CC != "" {
		o = append(o, jsonKV{"cc", loc.CC})
	}
	// geoid is the 7th Location constructor argument: the numeric GeoNames id
	// for geoname lookups, but the ZIP string for zip lookups.
	if loc.Geo == "zip" && loc.Zip != "" {
		o = append(o, jsonKV{"geoid", loc.Zip})
	} else if loc.GeonameID != 0 {
		o = append(o, jsonKV{"geoid", loc.GeonameID})
	}
	if loc.Admin1 != "" {
		o = append(o, jsonKV{"admin1", loc.Admin1})
	}
	if loc.StateName != "" {
		o = append(o, jsonKV{"stateName", loc.StateName})
	}
	o = append(o, jsonKV{"geo", loc.Geo})
	if loc.Zip != "" {
		o = append(o, jsonKV{"zip", loc.Zip})
	}
	if loc.Population != 0 {
		o = append(o, jsonKV{"population", loc.Population})
	}
	if loc.Asciiname != "" {
		o = append(o, jsonKV{"asciiname", loc.Asciiname})
	}
	// geonameid is set as a separate own property (in addition to geoid) by
	// @hebcal/geo-sqlite's makeGeonameLocation, so it sorts after asciiname.
	if loc.Geo == "geoname" && loc.GeonameID != 0 {
		o = append(o, jsonKV{"geonameid", loc.GeonameID})
	}
	// state is a dynamically-added own property on ZIP Locations, emitted last.
	if loc.State != "" {
		o = append(o, jsonKV{"state", loc.State})
	}
	return o
}
