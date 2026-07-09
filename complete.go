package main

// The /complete endpoint is a Go port of the hebcal-web src/complete.js
// geographic typeahead (also reachable as /complete.php). It returns a JSON
// array of location suggestions for the ?q= query, with an emoji country flag
// appended to each result. ?g=on (or ?g=1) additionally returns
// latitude/longitude/timezone/population.

import (
	"net/http"
	"strings"
)

// cacheControl3Days matches hebcal-web cacheControl(3): public, 3-day max-age.
const cacheControl3Days = "public, max-age=259200, s-maxage=259200"

// completeHandler implements GET /complete (and /complete.php).
func (app *appServer) completeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		corsPreflight(w, "GET")
		return
	}
	q := r.URL.Query()
	qraw := strings.TrimSpace(q.Get("q"))
	if qraw == "" {
		// hebcal-web returns 404 {"error":"Not Found"} with no Cache-Control
		// for an empty query.
		writeNotFoundJSON(w)
		return
	}
	if app.db == nil {
		app.writeGeoError(w, &httpError{status: http.StatusServiceUnavailable,
			message: "Location database is not available"})
		return
	}
	w.Header().Set("Cache-Control", cacheControl3Days)
	etag := makeETag(r, "")
	w.Header().Set("ETag", etag)
	if checkFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	latlong := q.Get("g") == "on" || q.Get("g") == "1"
	items := app.db.autoComplete(qraw, latlong)
	if len(items) == 0 {
		// No matches: drop the ETag (matching hebcal-web) and return 404. The
		// Cache-Control set above is retained, as in hebcal-web.
		w.Header().Del("ETag")
		writeNotFoundJSON(w)
		return
	}
	arr := make([]orderedObj, len(items))
	for i, it := range items {
		arr[i] = acItemToObj(it, latlong)
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.Write(jsonMarshal(arr))
}

// writeNotFoundJSON emits the 404 {"error":"Not Found"} body used by /complete.
func writeNotFoundJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusNotFound)
	w.Write(jsonMarshal(map[string]string{"error": "Not Found"}))
}

// acItemToObj serializes an autocomplete result to JSON, reproducing the field
// order and visibility of @hebcal/geo-sqlite's zip/geoname autocomplete objects
// plus the country flag appended by hebcal-web's /complete handler. When
// latlong is false, latitude/longitude/timezone/population are dropped from
// text-search results; numeric ZIP results keep their coordinates but still
// drop population.
func acItemToObj(it acItem, latlong bool) orderedObj {
	includeLatLong := latlong || it.numeric
	o := orderedObj{
		{"id", it.id},
		{"value", it.value},
	}
	if it.isZip {
		o = append(o,
			jsonKV{"admin1", it.admin1},
			jsonKV{"asciiname", it.asciiname},
			jsonKV{"country", it.country},
			jsonKV{"cc", it.cc},
		)
		if includeLatLong {
			o = append(o,
				jsonKV{"latitude", it.latitude},
				jsonKV{"longitude", it.longitude},
				jsonKV{"timezone", it.timezone},
			)
		}
		if latlong {
			o = append(o, jsonKV{"population", it.population})
		}
		o = append(o, jsonKV{"geo", it.geo})
	} else {
		o = append(o,
			jsonKV{"admin1", it.admin1},
			jsonKV{"country", it.country},
			jsonKV{"cc", it.cc},
		)
		if includeLatLong {
			o = append(o,
				jsonKV{"latitude", it.latitude},
				jsonKV{"longitude", it.longitude},
				jsonKV{"timezone", it.timezone},
			)
		}
		o = append(o, jsonKV{"geo", it.geo})
		if latlong && it.population != 0 {
			o = append(o, jsonKV{"population", it.population})
		}
		if it.name != "" {
			o = append(o, jsonKV{"name", it.name})
		}
		if it.asciiname != "" {
			o = append(o, jsonKV{"asciiname", it.asciiname})
		}
	}
	if len(it.cc) == 2 {
		o = append(o, jsonKV{"flag", flagEmoji(it.cc)})
	}
	return o
}

// flagEmoji converts a 2-letter ISO country code to its regional-indicator
// emoji flag, matching hebcal-web src/emoji-flag.js.
func flagEmoji(cc string) string {
	cc = strings.ToUpper(cc)
	var b strings.Builder
	for _, c := range cc {
		b.WriteRune(0x1F1E6 + (c - 'A'))
	}
	return b.String()
}
