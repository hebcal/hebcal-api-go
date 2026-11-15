package handler

import (
	"net/http"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/service/fridge"
	"github.com/hebcal/hebcal-api-go/internal/service/location"
	"github.com/hebcal/hebcal-api-go/internal/service/shabbat"
)

// cacheControl3Days is the lifetime of a fridge page that names its year.
const cacheControl3Days = "public, max-age=259200, s-maxage=259200"

// htmlCSP builds the strict Content-Security-Policy www.hebcal.com sends
// with its HTML pages: scripts run only when they carry the response's nonce.
func htmlCSP(nonce string) string {
	return "script-src 'nonce-" + nonce + "' 'strict-dynamic' 'report-sample' https: 'unsafe-inline';" +
		" style-src 'self' https: data: 'unsafe-inline';" +
		" frame-ancestors https: data:;" +
		" frame-src https: data: blob:;" +
		" img-src 'self' https: data: blob:;" +
		" font-src 'self' data: https://fonts.gstatic.com/;" +
		" object-src 'none';" +
		" base-uri 'none';" +
		" report-uri https://y61ectdg.uriports.com/reports/report;" +
		" report-to default"
}

// setHTMLSecurityHeaders sets the Content-Security-Policy for an HTML page
// whose inline scripts carry nonce, and the reporting headers that go with it.
func setHTMLSecurityHeaders(w http.ResponseWriter, nonce string) {
	h := w.Header()
	h.Set("Content-Security-Policy", htmlCSP(nonce))
	h.Set("X-XSS-Protection", "0")
	h.Set("Reporting-Endpoints", `default="https://y61ectdg.uriports.com/reports"`)
}

// fridgeShabbat implements GET /shabbat/fridge.cgi: a location's
// candle-lighting times for a whole year, as a page made for printing or,
// with cfg=csv, as a CSV download. Errors are plain text.
//
// A request that names a year is cacheable for three days. One that does not
// gets the coming Hebrew year, which changes with the date, so it carries no
// lifetime and its ETag includes the year it resolved to.
func (s *Server) fridgeShabbat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
		return
	}
	q := r.URL.Query()
	jsutil.TrimTrailingWhitespace(q)
	isCSV := q.Get("cfg") == "csv"
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	if isCSV {
		httpx.SetCORS(w)
	}
	if s.DB == nil {
		httpx.WritePlainError(w, dbUnavailable())
		return
	}
	today := s.Now()
	// Before the location lookup: an out-of-range year is the commonest
	// request a crawler sends, and the cheapest to refuse.
	year, err := fridge.ParseYear(q, today)
	if err != nil {
		httpx.WritePlainError(w, err)
		return
	}
	lg := shabbat.QueryLang(q)
	if !model.LocaleSupported(lg) {
		httpx.WritePlainError(w, model.BadRequest("Locale '%s' not found", lg))
		return
	}
	loc, err := location.FromQuery(s.DB, q)
	if err != nil {
		httpx.WritePlainError(w, err)
		return
	}
	if loc == nil {
		httpx.WritePlainError(w, model.BadRequest("Location required: geonameid, zip, city"))
		return
	}

	etagExtra := ""
	if year.Default {
		etagExtra = "year=" + model.GregFromRD(year.Start).String()
	} else {
		h.Set("Cache-Control", cacheControl3Days)
	}
	etag := httpx.MakeETag(r, etagExtra)
	h.Set("ETag", etag)
	if httpx.CheckFresh(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	cal, err := fridge.New(loc, year, lg, q)
	if err != nil {
		httpx.WritePlainError(w, err)
		return
	}
	if isCSV {
		h.Set("Content-Type", "text/csv; charset=utf-8")
		h.Set("Content-Disposition", attachment(cal.CSVFilename()))
		w.Write(cal.CSV())
		return
	}
	nonce := fridge.NewNonce()
	body, err := cal.HTML(q, today, nonce)
	if err != nil {
		httpx.WritePlainError(w, model.Internal("%s", err.Error()))
		return
	}
	setHTMLSecurityHeaders(w, nonce)
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}
