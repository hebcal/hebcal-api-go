// Package httpx bundles the transport-layer plumbing every route shares:
// content-type and cache-control constants, CORS headers, weak ETags and
// freshness checks, error rendering, client-IP extraction, and the middleware
// that buffers, compresses, measures, and logs each response.
package httpx

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
)

// Cache-Control values used across the routes.
//
// The two PDF lifetimes: 14 days for the /v4/ and /v2/ downloads, and 60 days
// for the /holidays/ calendars, which are a pure function of the year.
const (
	CacheControl1Year  = "public, max-age=31536000, s-maxage=31536000"
	CacheControl60Days = "public, max-age=5184000, s-maxage=5184000"
	CacheControl30Days = "public, max-age=2592000, s-maxage=2592000"
	CacheControl14Days = "public, max-age=1209600, s-maxage=1209600"
	CacheControl7Days  = "public, max-age=604800, s-maxage=604800"
	CacheControl1Day   = "public, max-age=86400, s-maxage=86400"
)

// Content types used across the routes.
const (
	ContentTypeJSON  = "application/json; charset=utf-8"
	ContentTypeXML   = "text/xml; charset=utf-8"
	ContentTypeCSV   = "text/x-csv; charset=utf-8"
	ContentTypeText  = "text/plain; charset=utf-8"
	ContentTypeJSONP = "text/javascript; charset=utf-8"
)

// PreflightMaxAge is the Access-Control-Max-Age of a preflight response, in
// seconds. Chromium caps it at 7200.
const PreflightMaxAge = "7200"

// SetCORS marks a response world-readable, for API responses (cfg param
// present).
func SetCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
}

// CORSPreflight answers an OPTIONS request with a CORS preflight response
// advertising the given allowed methods. OPTIONS responses are not cacheable
// by HTTP caches, so Access-Control-Max-Age is what lets a browser reuse the
// result (it would otherwise default to 5 seconds).
func CORSPreflight(w http.ResponseWriter, methods string) {
	SetCORS(w)
	w.Header().Set("Access-Control-Allow-Methods", methods)
	w.Header().Set("Access-Control-Max-Age", PreflightMaxAge)
	w.WriteHeader(http.StatusNoContent)
}

// RecordError stashes the error a response is rendering onto the request's
// reqlog.Collector, so the access-log middleware can emit its message under
// "msg". It reaches the collector through the middleware's own response writer;
// when w is some other writer (a bare httptest recorder in a unit test, say) it
// is a no-op, so callers never have to check. The shared error helpers below
// call it for free; a route that writes an error with http.Error directly
// (the PDF handler does, to control caching per status) calls it itself.
func RecordError(w http.ResponseWriter, err error) {
	if bw, ok := w.(*bufWriter); ok {
		bw.calls.SetError(err)
	}
}

// MethodNotAllowed answers with 405 and an Allow header listing allowed.
func MethodNotAllowed(w http.ResponseWriter, method, allowed string) {
	w.Header().Set("Allow", allowed)
	err := fmt.Errorf("Method %s not allowed", method)
	RecordError(w, err)
	http.Error(w, err.Error(), http.StatusMethodNotAllowed)
}

// cachingHeaders are the freshness and validator headers a handler may have
// set for the body it meant to send.
var cachingHeaders = []string{"Cache-Control", "Expires", "Last-Modified", "ETag"}

// clearCaching removes cachingHeaders. An error response describes this
// attempt, not the resource, so it must not inherit a lifetime or validator a
// handler set before something later failed (a PDF render error, a sidecar
// that did not answer): Varnish would otherwise keep a transient failure for
// as long as the calendar itself. A status that is meant to be cached (the
// download 410, /complete's no-match 404) is written without these helpers.
func clearCaching(w http.ResponseWriter) {
	h := w.Header()
	for _, k := range cachingHeaders {
		h.Del(k)
	}
}

// WritePlainError emits a plain-text error carrying the error's own status,
// with no caching headers.
func WritePlainError(w http.ResponseWriter, err error) {
	RecordError(w, err)
	clearCaching(w)
	w.Header().Set("Content-Type", ContentTypeText)
	w.WriteHeader(model.StatusOf(err))
	fmt.Fprintln(w, err.Error())
}

// JSONErrorBody marshals a {"error": message} body with a trailing newline,
// the shape every non-200 application/json response uses.
func JSONErrorBody(message string) []byte {
	return append(jsutil.Marshal(map[string]string{"error": message}), '\n')
}

// WriteJSONError emits a JSON {"error": ...} body carrying the error's own
// status. It is the error shape the /geo, /zmanim, /shabbat and /complete
// routes share. Like WritePlainError it drops any caching headers.
func WriteJSONError(w http.ResponseWriter, err error) {
	RecordError(w, err)
	clearCaching(w)
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(model.StatusOf(err))
	w.Write(JSONErrorBody(err.Error()))
}

// WriteNotFoundText emits the plain-text 404 used by /ping and the catch-all
// route.
func WriteNotFoundText(w http.ResponseWriter) {
	w.Header().Set("Content-Type", ContentTypeText)
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Not Found\n"))
}

// WriteForbiddenText emits the plain-text 403 for a path that tries to
// climb out of the document root or names a dot file. Like the 404, it
// carries no Cache-Control.
func WriteForbiddenText(w http.ResponseWriter) {
	w.Header().Set("Content-Type", ContentTypeText)
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte("Forbidden\n"))
}

// ClientIP returns the client IP address, preferring X-Client-IP over
// X-Forwarded-For when the service runs behind a reverse proxy.
func ClientIP(r *http.Request) string {
	if xcip := r.Header.Get("X-Client-IP"); xcip != "" {
		return strings.TrimSpace(xcip)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
