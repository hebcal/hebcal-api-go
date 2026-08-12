package handler

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/service/converter"
)

// converter implements the /converter JSON and XML APIs.
func (s *Server) converter(w http.ResponseWriter, r *http.Request) {
	// The "/converter/" subtree pattern also catches bogus paths such as
	// /converter/csv0"XOR(...) — typically SQL-injection or vulnerability
	// probes that miss the exact /converter/csv route. Only the bare
	// endpoint and its trailing-slash form are valid here; reject anything
	// else with 400 rather than falling through to the cfg check below,
	// which would otherwise answer 501.
	if r.URL.Path != "/converter" && r.URL.Path != "/converter/" {
		httpx.WritePlainError(w, model.BadRequest("Not a valid converter request: %s", r.URL.Path))
		return
	}
	q := r.URL.Query()
	cfg := q.Get("cfg")
	if cfg != "" {
		httpx.SetCORS(w)
	}
	switch r.Method {
	case http.MethodOptions:
		httpx.CORSPreflight(w, "GET, POST")
		return
	case http.MethodGet, http.MethodPost, http.MethodHead:
		// POST is accepted but any request body is ignored;
		// conversion parameters come from the URL only
	default:
		httpx.MethodNotAllowed(w, r.Method, "GET, POST, HEAD, OPTIONS")
		return
	}
	if cfg != "json" && cfg != "xml" {
		w.Header().Set("Content-Type", httpx.ContentTypeJSON)
		w.WriteHeader(http.StatusNotImplemented)
		w.Write(jsutil.Marshal(map[string]string{
			"error": "Only cfg={json,xml} is supported by this endpoint",
		}))
		return
	}
	now := s.Now()
	p, err := converter.ParseQuery(q, now)
	// Ported from hebcal-web src/converter.js: a GET request that omits
	// every date parameter (bare /converter, or h2g=1/g2h=1 with no
	// hy/hm/hd or gy/gm/gd) resolves against "today" and so must not be
	// cached under a stable URL. Redirect to an equivalent request that
	// pins the date explicitly, with a short private Cache-Control.
	//
	// HEAD redirects alongside GET: RFC 9110 §9.3.2 makes HEAD identical to
	// GET but for the content, so answering 200 here while GET answers 302
	// would misreport the resource to any client that probes with HEAD.
	// hebcal-web tests only for GET, so this deliberately diverges from it;
	// POST keeps the JS behavior and renders "today" directly, since a POST
	// is not the cacheable, followable request the redirect exists to fix.
	if err == nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) && p.NoCache {
		redirectConverterNoCache(w, q, cfg, now)
		return
	}
	lg := q.Get("lg")
	if lg == "" {
		lg = "s"
	}
	if err != nil {
		writeConverterError(w, cfg, err)
		return
	}
	if p.IsRange && cfg != "json" {
		writeConverterError(w, cfg, model.BadRequest(converter.RangeRequiresCfgJSON))
		return
	}
	if !p.NoCache {
		etag := httpx.MakeETag(r, "")
		w.Header().Set("ETag", etag)
		// RFC 7232 §4.1: a 304 SHOULD carry the same Cache-Control
		// it would have sent on a 200
		w.Header().Set("Cache-Control", httpx.CacheControl1Year)
		if httpx.CheckFresh(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	if cfg == "xml" {
		w.Header().Set("Content-Type", httpx.ContentTypeXML)
		w.Write(converter.RenderXML(p, q, lg))
		return
	}
	var body []byte
	if p.IsRange {
		body = converter.RenderRangeJSON(p, q, lg)
	} else {
		body = converter.RenderSingleJSON(p, q, lg)
	}
	w.Header().Set("Content-Type", httpx.ContentTypeJSON)
	if cb := converter.StripCallback(q.Get("callback")); cb != "" {
		w.Write([]byte(cb + "("))
		w.Write(body)
		w.Write([]byte(")\n"))
		return
	}
	w.Write(body)
}

// writeConverterError emits an error response in the format matching cfg.
func writeConverterError(w http.ResponseWriter, cfg string, err error) {
	if cfg == "xml" {
		w.Header().Set("Content-Type", httpx.ContentTypeXML)
		w.WriteHeader(model.StatusOf(err))
		fmt.Fprintf(w, "<error message=\"%s\" />\n", jsutil.XMLEscape(err.Error()))
		return
	}
	httpx.WriteJSONError(w, err)
}

// redirectConverterNoCache 302s a date-less /converter GET to an equivalent
// URL with gd/gm/gy pinned to today. Ported from the noCache/message check
// in hebcal-web src/converter.js, with one deliberate fix: the JS version
// only re-appends &cfg=json (never &cfg=xml) to the redirect target, which
// silently drops cfg=xml from the round trip. The caller only reaches this
// function once cfg has already passed the {json,xml} gate, so cfg is
// always one of those two values here and we preserve it either way.
// Unlike hebcal-web, this microservice does not resolve a per-request
// location (there is no IP geolocation for /converter, only the fixed
// New-York "today" also used by Server.Now), so the redirect never appends
// &gs=on or &i=on: those require knowing the caller's location to
// determine after-sunset/Israel status.
func redirectConverterNoCache(w http.ResponseWriter, q url.Values, cfg string, gd model.GregDate) {
	lg := ""
	if v := q.Get("lg"); v != "" {
		lg = "&lg=" + url.QueryEscape(v)
	}
	location := fmt.Sprintf("https://www.hebcal.com/converter?gd=%d&gm=%d&gy=%d&g2h=1&cfg=%s%s",
		gd.Day, int(gd.Month), gd.Year, cfg, lg)
	w.Header().Set("Cache-Control", "private, max-age=1200")
	w.Header().Set("Location", location)
	w.Header().Set("Content-Type", httpx.ContentTypeText)
	w.WriteHeader(http.StatusFound)
	fmt.Fprintf(w, "Redirecting to %s\n", location)
}

// converterCSV implements the /converter/csv download.
func (s *Server) converterCSV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
		return
	}
	q := r.URL.Query()
	if q.Get("cfg") != "" {
		httpx.SetCORS(w)
	}
	p, err := converter.ParseQuery(q, s.Now())
	if err != nil {
		httpx.WritePlainError(w, err)
		return
	}
	if p.IsRange {
		httpx.WritePlainError(w,
			model.BadRequest("Date range conversion is not supported for CSV download"))
		return
	}
	if !p.NoCache && r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", httpx.CacheControl7Days)
	}
	etag := httpx.MakeETag(r, "")
	w.Header().Set("ETag", etag)
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", converter.CSVFilename(p.HD)))
	w.Header().Set("Content-Type", httpx.ContentTypeCSV)
	w.Write(converter.RenderCSV(p.HD))
}
