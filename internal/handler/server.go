// Package handler is the transport layer: it maps HTTP routes onto the
// service packages, and owns everything that is about the protocol rather than
// the calendar — method and cfg gating, CORS, cache validators, and the shape
// of each endpoint's error response.
package handler

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hebcal/hebcal-api-go/internal/config"
	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/repository/yahrzeitdb"
	csvsvc "github.com/hebcal/hebcal-api-go/internal/service/csv"
	"github.com/hebcal/hebcal-api-go/internal/service/ical"
	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
	"github.com/hebcal/hebcal-api-go/pkg/geoip"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// Server holds the shared state for the HTTP handlers. Its dependencies are
// exported so main can wire them and tests can substitute their own.
type Server struct {
	Logger *logger.AccessLogger
	// Now supplies "today" for the routes that have no location of their own;
	// tests replace it to pin a date.
	Now func() model.GregDate
	// Clock supplies the current instant for the routes that work out "today"
	// in a location's own time zone (the zmanim feeds); nil means time.Now.
	Clock func() time.Time
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
	// CSV renders the Outlook CSV calendars served from the same /v4/ URLs.
	CSV *csvsvc.Service
	// ICS renders the holiday calendars as iCalendar feeds, from the same
	// /v4/ URLs.
	ICS *ical.Service
	// DocumentRoot is the directory static files are served from; /ical/
	// maps to its ical/ subdirectory. Empty answers every static path 404.
	DocumentRoot string
	// Zones supplies the VTIMEZONE definitions the iCalendar feeds embed. Nil
	// serves the feeds without them.
	Zones *icalendar.ZoneInfo
	// Yahrzeit holds the saved Yahrzeit + Anniversary calendars served under
	// /v3/. Nil (no database configured) answers those 503.
	Yahrzeit yahrzeitdb.Store
	// DownloadLimiter caps how many PDF and CSV downloads run at once, shedding the
	// overflow with 503 so a flood cannot grow the heap into swap. Nil (the
	// zero value) leaves the routes unlimited; main sets it from the config.
	DownloadLimiter *httpx.Limiter
	// MCP serves the /mcp Model Context Protocol endpoint (www.hebcal.com/mcp).
	// main sets it from internal/service/mcp; when nil the route answers 404.
	MCP http.Handler
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
		CSV:      &csvsvc.Service{},
		ICS:      &ical.Service{},
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
	mux.HandleFunc("/robots.txt", mw.Serve(s.robots))
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/converter/csv", mw.Serve(s.converterCSV))
	mux.HandleFunc("/converter", mw.Serve(s.converter))
	mux.HandleFunc("/converter/", mw.Serve(s.converter))
	// /zmanim with cfg=json is the JSON API; without it, and the two other
	// paths, are download.hebcal.com's zmanim iCalendar feeds. The `.ics`
	// spellings are aliases of the same feeds; no trailing slash is served.
	mux.HandleFunc("/zmanim", mw.Serve(s.zmanim))
	mux.HandleFunc("/zmanim2", mw.Serve(s.DownloadLimiter.Wrap(s.zmanimFeed)))
	mux.HandleFunc("/sunrs", mw.Serve(s.DownloadLimiter.Wrap(s.zmanimFeed)))
	mux.HandleFunc("/zmanim.ics", mw.Serve(s.DownloadLimiter.Wrap(s.zmanimFeed)))
	mux.HandleFunc("/zmanim2.ics", mw.Serve(s.DownloadLimiter.Wrap(s.zmanimFeed)))
	mux.HandleFunc("/sunrs.ics", mw.Serve(s.DownloadLimiter.Wrap(s.zmanimFeed)))
	mux.HandleFunc("/shabbat", mw.Serve(s.shabbat))
	mux.HandleFunc("/shabbat/", mw.Serve(s.shabbat))
	mux.HandleFunc("/geo", mw.Serve(s.geo))
	mux.HandleFunc("/geo/", mw.Serve(s.geo))
	mux.HandleFunc("/complete", mw.Serve(s.complete))
	mux.HandleFunc("/complete/", mw.Serve(s.complete))
	mux.HandleFunc("/complete.php", mw.Serve(s.complete))
	// The Model Context Protocol server. Stateless streamable-HTTP: the SDK
	// answers non-POST with 405 itself.
	mux.HandleFunc("/mcp", mw.Serve(s.mcp))
	// www.hebcal.com's short URLs, which the PDF, iCalendar and email links
	// use: /h/ for holidays, /s/ for Torah portions, /o/ for the Omer.
	mux.HandleFunc("/h/", mw.Serve(s.shortURL))
	mux.HandleFunc("/s/", mw.Serve(s.shortURL))
	mux.HandleFunc("/o/", mw.Serve(s.shortURL))
	// The PDF and CSV calendars: download.hebcal.com's downloads and
	// www.hebcal.com's /holidays/ holiday calendars, both routed here by
	// Varnish. Nothing else under /holidays/ belongs to this service -- the
	// HTML pages there are served elsewhere, and pdfHoliday answers them 404.
	//
	// A download comes in three URL shapes, each serving .pdf and .csv:
	// /v4/<protobuf>/<name>, the legacy /v2/h/<base64 query>/<name> (which
	// production answers with a 301 to the /v4/ form; this service renders it
	// instead), and the classic /hebcal/index.cgi/<name>?<query>, older than
	// both and still crawled. The holiday .ics form of each shape is handled
	// elsewhere and answered 404 here; a personal Yahrzeit + Anniversary
	// calendar under any of them (v=yahrzeit) is served by yahrzeitDownload.
	//
	// Every download route -- both formats, all three URL shapes, and the
	// holiday PDFs -- shares one concurrency limiter: the cap is on total
	// simultaneous work, not per route or per format. A CSV costs roughly half
	// a PDF in CPU, so it holds a slot like any other request. Wrapped inside
	// mw.Serve so a shed 503 is still logged and counted like any other
	// response.
	limited := func(h http.HandlerFunc) http.HandlerFunc {
		return mw.Serve(s.DownloadLimiter.Wrap(h))
	}
	calendar := limited(s.download)
	mux.HandleFunc("/v2/", calendar)
	mux.HandleFunc("/v4/", calendar)
	mux.HandleFunc("/hebcal/index.cgi/", calendar)
	// /export/x.csv and /export/<dir>/x.csv are the same classic query-string
	// download under an older name; historical URLs nest the file in one or
	// more directories.
	mux.HandleFunc("/export/", calendar)
	// The personal Yahrzeit + Anniversary calendars, as iCalendar feeds. They
	// arrive under /v2/y/ and /export/ too, which the download handler hands
	// over when the query names one (v=yahrzeit); this is the original CGI
	// path, which serves nothing else here.
	mux.HandleFunc("/yahrzeit/yahrzeit.cgi/", calendar)
	// Saved Yahrzeit + Anniversary calendars, /v3/<id>/<name>.ics (or .csv),
	// whose parameters live in the MySQL database rather than the URL.
	mux.HandleFunc("/v3/", limited(s.yahrzeitSaved))
	mux.HandleFunc("/holidays/", limited(s.pdfHoliday))
	// The static calendars cmd/hebcal-static-ical writes weekly into
	// DocumentRoot/ical, sent precompressed when a .br or .gz copy is on
	// disk. Reading a file is cheap, so these hold no download slot.
	mux.HandleFunc("/ical/", mw.Serve(s.staticICal))
	mux.HandleFunc("/", mw.Serve(s.notFound))
	return s.withBackend(mux)
}

// ping serves the contents of the ping file. The file is read on every request
// so operators can create or remove it to move the host in or out of
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

// robotsTxt is the body of /robots.txt: every crawler may fetch every path.
const robotsTxt = "User-agent: *\nAllow: /\n"

// robots serves the static robots.txt for GET and HEAD, one day cacheable.
func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
		return
	}
	w.Header().Set("Content-Type", httpx.ContentTypeText)
	w.Header().Set("Cache-Control", httpx.CacheControl1Day)
	io.WriteString(w, robotsTxt)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	httpx.WriteNotFoundText(w)
}

// dbUnavailable is the error the location-dependent routes report when the
// geonames/zips databases could not be opened at startup.
func dbUnavailable() error {
	return model.Unavailable("Location database is not available")
}
