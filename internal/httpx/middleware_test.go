package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/logger"
)

// serveAndReadLog runs one request through the middleware and returns the
// single log line it wrote, decoded.
func serveAndReadLog(t *testing.T, h http.HandlerFunc, req *http.Request) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api.log")
	lg, err := logger.New(path)
	if err != nil {
		t.Fatal(err)
	}
	mw := &Middleware{Logger: lg}
	mw.Serve(h)(httptest.NewRecorder(), req)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &m); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, data)
	}
	return m
}

// The log format has to match hebcal-web's makeLogInfo(), field for field, so
// one pipeline reads both and hebcal-web's tools/perf analysis works against
// these logs too.
func TestAccessLogFormat(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v4/abc/hebcal_2026.pdf?x=1&y=2", nil)
	req.Header.Set("User-Agent", "curl/8")
	req.Header.Set("X-Client-IP", "203.0.113.7")
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("hello"))
	}, req)

	for _, k := range []string{"level", "time", "pid", "hostname", "status", "length",
		"duration", "ip", "method", "url", "ua"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing field %q; hebcal-web's log tooling expects it", k)
		}
	}
	if m["status"] != float64(200) {
		t.Errorf("status = %v", m["status"])
	}
	if m["ip"] != "203.0.113.7" {
		t.Errorf("ip = %v, want the X-Client-IP value Varnish sets", m["ip"])
	}
	// The query string is part of the logged URL, and an ampersand must not be
	// escaped to &amp; or the tooling cannot match URLs.
	if url, _ := m["url"].(string); url != "/v4/abc/hebcal_2026.pdf?x=1&y=2" {
		t.Errorf("url = %q", url)
	}
}

// 404s are scanner noise; other errors are worth a warning level.
func TestAccessLogLevels(t *testing.T) {
	tests := []struct {
		status int
		want   float64
	}{
		{200, logger.LevelInfo},
		{404, logger.LevelInfo},
		{400, logger.LevelWarn},
		{503, logger.LevelWarn},
	}
	for _, tt := range tests {
		m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.status)
		}, httptest.NewRequest(http.MethodGet, "/x", nil))
		if m["level"] != tt.want {
			t.Errorf("status %d logged at level %v, want %v", tt.status, m["level"], tt.want)
		}
	}
}

// A PDF request answered with the daily-learning fallback header records which
// series were involved, so those requests can be found in the log.
func TestAccessLogRecordsUnsupportedSeries(t *testing.T) {
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Unsupported-Series", "dirshuAmudYomi")
		w.WriteHeader(http.StatusServiceUnavailable)
	}, httptest.NewRequest(http.MethodGet, "/x", nil))
	if m["unsupported"] != "dirshuAmudYomi" {
		t.Errorf("unsupported = %v", m["unsupported"])
	}
}
