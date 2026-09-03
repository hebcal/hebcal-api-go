package handler

// The /complete endpoint is a Go port of the hebcal-web src/complete.js
// geographic typeahead (also reachable as /complete.php). It returns a JSON
// array of location suggestions for the ?q= query, with an emoji country flag
// appended to each result. ?g=on (or ?g=1) additionally returns
// latitude/longitude/timezone/population.

import (
	"net/http"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/service/complete"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// complete implements GET /complete (and /complete.php).
func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		httpx.CORSPreflight(w, "GET")
		return
	}
	q := r.URL.Query()
	jsutil.TrimTrailingWhitespace(q)
	qraw := strings.TrimSpace(q.Get("q"))
	if qraw == "" {
		// hebcal-web returns 404 {"error":"Not Found"} with no Cache-Control
		// for an empty query.
		writeNotFoundJSON(w)
		return
	}
	if s.DB == nil {
		httpx.WriteJSONError(w, dbUnavailable())
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=259200")
	callerIP := httpx.ClientIP(r)
	etag := httpx.MakeETag(r, callerIP)
	w.Header().Set("ETag", etag)
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	latlong := jsutil.IsOn(q.Get("g"))
	// The GeoIP hint is best-effort: a missing or unreachable service just
	// means results are ranked without a proximity bias.
	var near *geodb.Point
	if p, err := s.GeoIP.LookupPoint(r.Context(), callerIP); err == nil {
		near = &geodb.Point{Latitude: p.Latitude, Longitude: p.Longitude}
	}
	items := s.DB.AutoComplete(qraw, near)
	if len(items) == 0 {
		// No matches: drop the ETag (matching hebcal-web) and return 404. The
		// Cache-Control set above is retained, as in hebcal-web.
		w.Header().Del("ETag")
		writeNotFoundJSON(w)
		return
	}
	arr := make([]jsutil.OrderedObj, len(items))
	for i, it := range items {
		arr[i] = complete.ItemToObj(it, latlong)
	}
	w.Header().Set("Content-Type", httpx.ContentTypeJSON)
	w.Write(jsutil.Marshal(arr))
}

// writeNotFoundJSON emits the 404 {"error":"Not Found"} body used by /complete.
func writeNotFoundJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", httpx.ContentTypeJSON)
	w.WriteHeader(http.StatusNotFound)
	w.Write(jsutil.Marshal(map[string]string{"error": "Not Found"}))
}
