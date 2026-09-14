package handler

// The /complete endpoint implements a geographic typeahead (also reachable
// as /complete.php). It returns a JSON array of location suggestions for the
// ?q= query, with an emoji country flag appended to each result. ?g=on (or
// ?g=1) additionally returns latitude/longitude/timezone/population, plus
// elevation when it is positive, for both ZIP and geoname results.

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
		// An empty query gets no Cache-Control header at all.
		writeNotFoundJSON(w)
		return
	}
	if s.DB == nil {
		httpx.WriteJSONError(w, dbUnavailable())
		return
	}
	// ?g={on,1} marks the request as a public query with no IP-address bias:
	// it skips the GeoIP nearness hint and gets a public, longer-lived
	// Cache-Control instead of the default per-caller private one.
	latlong := jsutil.IsOn(q.Get("g"))
	if latlong {
		w.Header().Set("Cache-Control", "public, max-age=259200")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=259200")
	}
	// A public query's ETag must not vary by caller IP, or it stops being
	// cacheable across clients.
	etagExtra := ""
	callerIP := httpx.ClientIP(r)
	if !latlong {
		etagExtra = callerIP
	}
	etag := httpx.MakeETag(r, etagExtra)
	w.Header().Set("ETag", etag)
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	// The GeoIP hint is best-effort: a missing or unreachable service just
	// means results are ranked without a proximity bias.
	var near *geodb.Point
	if !latlong {
		if p, err := s.GeoIP.LookupPoint(r.Context(), callerIP); err == nil {
			near = &geodb.Point{Latitude: p.Latitude, Longitude: p.Longitude}
		}
	}
	items := s.DB.AutoComplete(qraw, near)
	if len(items) == 0 {
		// No matches: drop the ETag and return 404, but keep the
		// Cache-Control header set above.
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
	w.Write(httpx.JSONErrorBody("Not Found"))
}
