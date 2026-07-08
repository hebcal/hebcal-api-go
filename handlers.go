package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	cacheControl1Year = "public, max-age=31536000, s-maxage=31536000"
	cacheControl7Days = "public, max-age=604800, s-maxage=604800"
)

const contentTypeJSON = "application/json; charset=utf-8"
const contentTypeXML = "text/xml; charset=utf-8"
const contentTypeCSV = "text/x-csv; charset=utf-8"

const defaultPingFile = "/var/www/html/ping"

// appServer holds the shared state for HTTP handlers.
type appServer struct {
	logger   *accessLogger
	now      func() gregDate // injectable for tests
	pingFile string
	db       *GeoDB // geonames/zips database for the /zmanim API; may be nil
}

func newAppServer(logger *accessLogger) *appServer {
	return &appServer{logger: logger, now: todayNewYork, pingFile: defaultPingFile}
}

// mux builds the HTTP routing table.
func (app *appServer) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", app.serve(app.pingHandler))
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/converter/csv", app.serve(app.csvHandler))
	mux.HandleFunc("/converter", app.serve(app.converterHandler))
	mux.HandleFunc("/converter/", app.serve(app.converterHandler))
	mux.HandleFunc("/zmanim", app.serve(app.zmanimHandler))
	mux.HandleFunc("/zmanim/", app.serve(app.zmanimHandler))
	mux.HandleFunc("/shabbat", app.serve(app.shabbatHandler))
	mux.HandleFunc("/shabbat/", app.serve(app.shabbatHandler))
	mux.HandleFunc("/", app.serve(app.notFoundHandler))
	return mux
}

// pingHandler serves the contents of the ping file (like hebcal-web, which
// serves /var/www/html/ping via koa-send). The file is read on every request
// so operators can create or remove it to move the host in or out of
// load-balancer rotation; a missing file yields a 404.
func (app *appServer) pingHandler(w http.ResponseWriter, r *http.Request) {
	body, err := os.ReadFile(app.pingFile)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found\n"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}

func (app *appServer) notFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Not Found\n"))
}

// setCORS mirrors hebcal-web: API responses (cfg param present) are
// world-readable.
func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
}

// converterHandler implements the /converter JSON and XML APIs.
func (app *appServer) converterHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cfg := q.Get("cfg")
	if cfg != "" {
		setCORS(w)
	}
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodGet, http.MethodPost, http.MethodHead:
		// POST is accepted but any request body is ignored;
		// conversion parameters come from the URL only
	default:
		w.Header().Set("Allow", "GET, POST, HEAD, OPTIONS")
		http.Error(w, fmt.Sprintf("Method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	now := app.now()
	p, err := parseConverterQuery(q, now)
	// Ported from hebcal-web src/converter.js: a GET request that omits
	// every date parameter (bare /converter, or h2g=1/g2h=1 with no
	// hy/hm/hd or gy/gm/gd) resolves against "today" and so must not be
	// cached under a stable URL. Redirect to an equivalent request that
	// pins the date explicitly, with a short private Cache-Control.
	if err == nil && r.Method == http.MethodGet && p.noCache {
		app.redirectConverterNoCache(w, q, cfg, now)
		return
	}
	if cfg != "json" && cfg != "xml" {
		w.Header().Set("Content-Type", contentTypeJSON)
		w.WriteHeader(http.StatusNotImplemented)
		w.Write(jsonMarshal(map[string]string{
			"error": "Only cfg={json,xml} is supported by this endpoint",
		}))
		return
	}
	lg := q.Get("lg")
	if lg == "" {
		lg = "s"
	}
	if err != nil {
		app.writeConverterError(w, cfg, err)
		return
	}
	if p.isRange && cfg != "json" {
		app.writeConverterError(w, cfg, badRequest(rangeRequiresCfgJSON))
		return
	}
	if !p.noCache {
		etag := makeETag(r, "")
		w.Header().Set("ETag", etag)
		// RFC 7232 §4.1: a 304 SHOULD carry the same Cache-Control
		// it would have sent on a 200
		w.Header().Set("Cache-Control", cacheControl1Year)
		if checkFresh(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	if cfg == "xml" {
		w.Header().Set("Content-Type", contentTypeXML)
		w.Write(renderXML(p, q, lg))
		return
	}
	var body []byte
	if p.isRange {
		body = renderRangeJSON(p, q, lg)
	} else {
		body = renderSingleJSON(p, q, lg)
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	if cb := stripCallback(q.Get("callback")); cb != "" {
		w.Write([]byte(cb + "("))
		w.Write(body)
		w.Write([]byte(")\n"))
		return
	}
	w.Write(body)
}

// writeConverterError emits a 400 response in the format matching cfg.
func (app *appServer) writeConverterError(w http.ResponseWriter, cfg string, err error) {
	status := http.StatusBadRequest
	if herr, ok := err.(*httpError); ok {
		status = herr.status
	}
	if cfg == "xml" {
		w.Header().Set("Content-Type", contentTypeXML)
		w.WriteHeader(status)
		fmt.Fprintf(w, "<error message=\"%s\" />\n", xmlEscape(err.Error()))
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	w.Write(jsonMarshal(map[string]string{"error": err.Error()}))
}

// redirectConverterNoCache 302s a date-less /converter GET to an equivalent
// URL with gd/gm/gy pinned to today. Ported from the noCache/message check
// in hebcal-web src/converter.js. Unlike hebcal-web, this microservice does
// not resolve a per-request location (there is no IP geolocation for
// /converter, only the fixed New-York "today" also used by app.now()), so
// the redirect never appends &gs=on or &i=on: those require knowing the
// caller's location to determine after-sunset/Israel status.
func (app *appServer) redirectConverterNoCache(w http.ResponseWriter, q url.Values, cfg string, gd gregDate) {
	json := ""
	if cfg == "json" {
		json = "&cfg=json"
	}
	lg := ""
	if v := q.Get("lg"); v != "" {
		lg = "&lg=" + url.QueryEscape(v)
	}
	location := fmt.Sprintf("/converter?gd=%d&gm=%d&gy=%d&g2h=1%s%s",
		gd.Day, int(gd.Month), gd.Year, json, lg)
	w.Header().Set("Cache-Control", "private, max-age=1200")
	w.Header().Set("Location", location)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusFound)
	fmt.Fprintf(w, "Redirecting to %s.\n", location)
}

// csvHandler implements the /converter/csv download.
func (app *appServer) csvHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, fmt.Sprintf("Method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	if q.Get("cfg") != "" {
		setCORS(w)
	}
	p, err := parseConverterQuery(q, app.now())
	if err != nil {
		writePlainError(w, err)
		return
	}
	if p.isRange {
		writePlainError(w, badRequest("Date range conversion is not supported for CSV download"))
		return
	}
	if !p.noCache && r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", cacheControl7Days)
	}
	etag := makeETag(r, "")
	w.Header().Set("ETag", etag)
	if checkFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", csvFilename(p.hd)))
	w.Header().Set("Content-Type", contentTypeCSV)
	w.Write(renderCSV(p.hd))
}

func writePlainError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if herr, ok := err.(*httpError); ok {
		status = herr.status
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintln(w, err.Error())
}
