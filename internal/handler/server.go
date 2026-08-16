// Package handler is the transport layer: it maps HTTP routes onto the
// service packages, and owns everything that is about the protocol rather than
// the calendar — method and cfg gating, CORS, cache validators, and the shape
// of each endpoint's error response.
package handler

import (
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hebcal/hebcal-api-go/internal/config"
	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
	"github.com/hebcal/hebcal-api-go/pkg/geoip"
)

// Server holds the shared state for the HTTP handlers. Its dependencies are
// exported so main can wire them and tests can substitute their own.
type Server struct {
	Logger *logger.AccessLogger
	// Now supplies "today" for the routes that have no location of their own;
	// tests replace it to pin a date.
	Now func() model.GregDate
	// PingFile is served by /ping.
	PingFile string
	// Hostname is advertised in the X-Backend response header.
	Hostname string
	// DB is the geonames/zips database. It may be nil, in which case the
	// location-dependent routes answer 503 and the rest keep working.
	DB *geodb.DB
	// GeoIP resolves the caller's approximate location for /complete. It may
	// be nil or unreachable; the route then ranks results without that hint.
	GeoIP *geoip.Client
	// Readings is the client for the readings-svc sidecar, which supplies
	// Torah readings for /shabbat and the daily-learning series the PDF
	// calendars cannot generate in-process.
	Readings *readings.Client
	// PDF renders the calendar PDFs. Its Renderer is nil when the fonts could
	// not be loaded, in which case the PDF routes answer 503 and the rest of
	// the API keeps working.
	PDF *pdf.Service
	// PDFLimiter caps how many PDF calendars render at once, shedding the
	// overflow with 503 so a flood cannot grow the heap into swap. Nil (the
	// zero value) leaves the routes unlimited; main sets it from the config.
	PDFLimiter *httpx.Limiter
}

// New returns a Server with the defaults main and the tests share.
func New(lg *logger.AccessLogger) *Server {
	hostname, _ := os.Hostname()
	return &Server{
		Logger:   lg,
		Now:      model.TodayNewYork,
		PingFile: config.DefaultPingFile,
		Hostname: hostname,
		Readings: readings.New(readings.DefaultSocket),
		PDF:      &pdf.Service{},
	}
}

// withBackend advertises the hostname that served the request in the
// X-Backend response header. It wraps the whole mux, so the header appears on
// every response - success, error, and the routes that bypass the middleware -
// as long as the hostname is known. Hostname is read per request rather than
// captured, so it stays settable after Routes has been called.
func (s *Server) withBackend(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Hostname != "" {
			w.Header().Set("X-Backend", s.Hostname)
		}
		h.ServeHTTP(w, r)
	})
}

// Routes builds the HTTP routing table.
func (s *Server) Routes() http.Handler {
	mw := &httpx.Middleware{Logger: s.Logger}
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", mw.Serve(s.ping))
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/converter/csv", mw.Serve(s.converterCSV))
	mux.HandleFunc("/converter", mw.Serve(s.converter))
	mux.HandleFunc("/converter/", mw.Serve(s.converter))
	mux.HandleFunc("/zmanim", mw.Serve(s.zmanim))
	mux.HandleFunc("/zmanim/", mw.Serve(s.zmanim))
	mux.HandleFunc("/shabbat", mw.Serve(s.shabbat))
	mux.HandleFunc("/shabbat/", mw.Serve(s.shabbat))
	mux.HandleFunc("/geo", mw.Serve(s.geo))
	mux.HandleFunc("/geo/", mw.Serve(s.geo))
	mux.HandleFunc("/complete", mw.Serve(s.complete))
	mux.HandleFunc("/complete/", mw.Serve(s.complete))
	mux.HandleFunc("/complete.php", mw.Serve(s.complete))
	// The PDF calendars: download.hebcal.com's /v4/ downloads and
	// www.hebcal.com's /holidays/ holiday calendars, both routed here by
	// Varnish. Nothing else under /holidays/ belongs to this service -- the
	// HTML pages there are hebcal-web's, and pdfHoliday answers them 404.
	//
	// /v2/ is the legacy download URL, which hebcal-web answers with a 301 to
	// the /v4/ form; this service renders it instead. Only /v2/h/<...>.pdf is
	// ours -- the other /v2/ families are .ics feeds and yahrzeit calendars,
	// and pdfDownload answers them 404.
	// The three PDF routes share one renderer and one memory pool, so they
	// share one concurrency limiter: the cap is on total simultaneous renders,
	// not per route. Wrapped inside mw.Serve so a shed 503 is still logged and
	// counted like any other response.
	mux.HandleFunc("/v2/", mw.Serve(s.PDFLimiter.Wrap(s.pdfDownload)))
	mux.HandleFunc("/v4/", mw.Serve(s.PDFLimiter.Wrap(s.pdfDownload)))
	// The classic /hebcal/index.cgi/<name>.pdf?<query> download URL, older than
	// both /v2/ and /v4/ and still crawled. Only its .pdf is ours (the .ics and
	// yahrzeit calendars this path also serves are hebcal-web's), so pdfDownload
	// answers everything else 404.
	mux.HandleFunc("/hebcal/index.cgi/", mw.Serve(s.PDFLimiter.Wrap(s.pdfDownload)))
	mux.HandleFunc("/holidays/", mw.Serve(s.PDFLimiter.Wrap(s.pdfHoliday)))
	mux.HandleFunc("/", mw.Serve(s.notFound))
	return s.withBackend(mux)
}

// ping serves the contents of the ping file (like hebcal-web, which serves
// /var/www/html/ping via koa-send). The file is read on every request so
// operators can create or remove it to move the host in or out of
// load-balancer rotation; a missing file yields a 404.
func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	body, err := os.ReadFile(s.PingFile)
	if err != nil {
		httpx.WriteNotFoundText(w)
		return
	}
	w.Header().Set("Content-Type", httpx.ContentTypeText)
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	httpx.WriteNotFoundText(w)
}

// dbUnavailable is the error the location-dependent routes report when the
// geonames/zips databases could not be opened at startup.
func dbUnavailable() error {
	return model.Unavailable("Location database is not available")
}
