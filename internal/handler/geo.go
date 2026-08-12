package handler

// The /geo endpoint resolves an HTTP request's query parameters to a location
// and returns the raw @hebcal/core Location object as JSON, matching the
// hebcal-web src/router.js "/geo" route:
//
//	ctx.response.type = ctx.request.header['accept'] = 'application/json';
//	ctx.body = getLocationFromQuery(ctx.db, ctx.request.query);
//
// The JSON shape deliberately mirrors how Koa serializes an @hebcal/core
// Location rather than the trimmed "location" object used by /zmanim and
// /shabbat; see location.ToGeoJSON.

import (
	"net/http"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/service/location"
)

// geo implements GET/HEAD /geo. It returns the resolved location as JSON, 204
// No Content when no location parameters are supplied (matching Koa setting
// ctx.body = null), or a JSON error for malformed/unresolvable input.
func (s *Server) geo(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if r.Method == http.MethodOptions {
		httpx.CORSPreflight(w, "GET")
		return
	}
	// hebcal-web only sets CORS headers when a cfg parameter is present; the
	// /geo route is normally called without one.
	if q.Get("cfg") != "" {
		httpx.SetCORS(w)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
		return
	}
	if s.DB == nil {
		httpx.WriteJSONError(w, dbUnavailable())
		return
	}
	loc, err := location.FromQuery(s.DB, q)
	if err != nil {
		httpx.WriteJSONError(w, err)
		return
	}
	if loc == nil {
		// No location parameters: hebcal-web assigns ctx.body = null, which Koa
		// turns into a 204 No Content with no body.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", httpx.ContentTypeJSON)
	w.Write(jsutil.Marshal(location.ToGeoJSON(loc)))
}
