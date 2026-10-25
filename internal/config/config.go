// Package config loads the service's runtime configuration from command-line
// flags and environment variables, and exposes the build metadata that the API
// responses and ETags are stamped with.
package config

import (
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/pkg/geoip"
)

// Defaults for the settings an operator is most likely to leave alone.
const (
	DefaultPort       = 8082
	DefaultPingFile   = "/var/www/html/ping"
	DefaultZipsDB     = "zips.sqlite3"
	DefaultGeonamesDB = "geonames.sqlite3"
	DefaultFontDir    = "fonts"
	// DefaultGeonamesPoll is how often the geonames database file is checked
	// for replacement. A check is one stat(2); the file is rebuilt weekly and
	// nothing needs the new rows within minutes.
	DefaultGeonamesPoll = 10 * time.Minute
	// DefaultDownloadMaxConcurrency caps how many PDF and CSV downloads run at once. An
	// unbounded flood grows the heap past physical RAM and the box swaps, which
	// is what turned 50ms requests into the 10-25s durations seen in production.
	//
	// Sized from measurement, not churn: one concurrent render holds ~25-37 MiB
	// of *live* heap (peak HeapInuse sampled under sustained load), so peak
	// working set is roughly N * 30 MiB. On a ~1 GiB box with GOMEMLIMIT=768MiB
	// (see etc/hebcal-api.service) the GC starts thrashing to defend that ceiling
	// at about N=32 (~740 MiB); 16 (~440 MiB peak) keeps a ~2x margin below that
	// while draining a flood at ~320 renders/s. Raise it together with the
	// GOMEMLIMIT / MemoryMax numbers on a larger box; the rule of thumb is to
	// keep N * 30 MiB under about half of GOMEMLIMIT.
	DefaultDownloadMaxConcurrency = 8
	// DefaultDownloadQueueTimeout is how long a request waits for a free render slot
	// before it is shed with 503. A render is ~50ms, so a short wait absorbs
	// bursts without letting a flood pile up in memory.
	DefaultDownloadQueueTimeout = 5 * time.Second
)

// Config is the resolved runtime configuration.
type Config struct {
	// Port is the TCP port to listen on.
	Port int
	// LogFile is the access log path; empty or "-" means stdout.
	LogFile string
	// PingFile is served by /ping; a missing file makes /ping return 404.
	PingFile string
	// ZipsDB and GeonamesDB are the SQLite databases behind the location APIs.
	ZipsDB     string
	GeonamesDB string
	// GeonamesPoll is how often GeonamesDB is checked for an atomic
	// replacement and reopened if one is found. Zero disables polling; a
	// SIGHUP or SIGUSR1 still triggers a check.
	GeonamesPoll time.Duration
	// GeoIPSocket is the unix domain socket of the geoip2 service.
	GeoIPSocket string
	// ReadingsSocket is the unix domain socket of the readings-svc sidecar,
	// which supplies Torah readings for /shabbat and the daily-learning series
	// no Go schedule generates. Empty disables both: /shabbat answers 503 and
	// a PDF asking for one of those six series is refused with 501.
	ReadingsSocket string
	// FontDir holds the Source_Sans_Pro/ and Adobe_Hebrew/ faces the PDF
	// calendars are drawn with. Without it the PDF routes report 503 and the
	// rest of the API is unaffected.
	FontDir string
	// DownloadMaxConcurrency caps how many PDF and CSV downloads run simultaneously; the
	// overflow is shed with 503 rather than queued into swap. Zero disables the
	// cap. See DefaultDownloadMaxConcurrency.
	DownloadMaxConcurrency int
	// DownloadQueueTimeout is how long a download request waits for a free render slot
	// before it is shed with 503 + Retry-After.
	DownloadQueueTimeout time.Duration
}

// Load parses flags (with environment-variable fallbacks) into a Config. It
// calls flag.Parse, so it must be called once, from main.
func Load() *Config {
	cfg := &Config{}
	defaultPort := DefaultPort
	if s := os.Getenv("PORT"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			defaultPort = n
		}
	}
	flag.IntVar(&cfg.Port, "port", defaultPort, "port to listen on")
	flag.StringVar(&cfg.LogFile, "logfile", "",
		"access log file path (empty or \"-\" for stdout)")
	flag.StringVar(&cfg.PingFile, "pingfile", DefaultPingFile,
		"file served by /ping; a missing file makes /ping return 404")
	flag.StringVar(&cfg.ZipsDB, "zips-db", envOr("ZIPS_DB", DefaultZipsDB),
		"path to the zips SQLite database (for the /zmanim API)")
	flag.StringVar(&cfg.GeonamesDB, "geonames-db", envOr("GEONAMES_DB", DefaultGeonamesDB),
		"path to the geonames SQLite database (for the /zmanim API)")
	flag.DurationVar(&cfg.GeonamesPoll, "geonames-poll", DefaultGeonamesPoll,
		"how often to check the geonames database for replacement and reopen it (0 disables; SIGHUP also checks)")
	flag.StringVar(&cfg.GeoIPSocket, "socket", geoip.DefaultSocket,
		"path to the GeoIP unix domain socket")
	flag.StringVar(&cfg.ReadingsSocket, "readings-socket",
		envOr("READINGS_SOCKET", readings.DefaultSocket),
		"path to the readings-svc unix domain socket (Torah readings for "+
			"/shabbat and the daily-learning series with no Go schedule)")
	flag.StringVar(&cfg.FontDir, "fonts", envOr("FONT_DIR", DefaultFontDir),
		"directory holding Source_Sans_Pro/ and Adobe_Hebrew/ (for the PDF calendars)")
	flag.IntVar(&cfg.DownloadMaxConcurrency, "download-max-concurrency",
		envIntOr("DOWNLOAD_MAX_CONCURRENCY", DefaultDownloadMaxConcurrency),
		"max PDF and CSV downloads served at once; the overflow is shed with 503 (0 disables the cap)")
	flag.DurationVar(&cfg.DownloadQueueTimeout, "download-queue-timeout", DefaultDownloadQueueTimeout,
		"how long a download request waits for a free render slot before it is shed with 503")
	flag.Parse()
	return cfg
}

// envIntOr returns the environment variable named by key parsed as an int, or
// def if it is unset, empty, or not a number.
func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// envOr returns the environment variable named by key, or def if it is unset
// or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
