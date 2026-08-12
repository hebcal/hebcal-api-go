// Package config loads the service's runtime configuration from command-line
// flags and environment variables, and exposes the build metadata that the API
// responses and ETags are stamped with.
package config

import (
	"flag"
	"os"
	"strconv"

	"github.com/hebcal/hebcal-api-go/pkg/geoip"
)

// Defaults for the settings an operator is most likely to leave alone.
const (
	DefaultPort       = 8082
	DefaultPingFile   = "/var/www/html/ping"
	DefaultZipsDB     = "zips.sqlite3"
	DefaultGeonamesDB = "geonames.sqlite3"
	DefaultLeyningURL = "http://localhost:8080/leyning"
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
	// GeoIPSocket is the unix domain socket of the geoip2 service.
	GeoIPSocket string
	// LeyningURL is the hebcal-web /leyning endpoint supplying Torah readings.
	LeyningURL string
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
	flag.StringVar(&cfg.GeoIPSocket, "socket", geoip.DefaultSocket,
		"path to the GeoIP unix domain socket")
	flag.StringVar(&cfg.LeyningURL, "leyning-url", envOr("LEYNING_URL", DefaultLeyningURL),
		"URL of the hebcal-web /leyning endpoint (Torah readings for /shabbat)")
	flag.Parse()
	return cfg
}

// envOr returns the environment variable named by key, or def if it is unset
// or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
