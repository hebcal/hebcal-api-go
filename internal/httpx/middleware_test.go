package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/reqlog"
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
// these logs too. "host" is the one deliberate addition: this binary answers
// both www.hebcal.com and download.hebcal.com, where hebcal-web's makeLogInfo
// only ever logs for a single vhost.
func TestAccessLogFormat(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v4/abc/hebcal_2026.pdf?x=1&y=2", nil)
	req.Host = "download.hebcal.com"
	req.Header.Set("User-Agent", "curl/8")
	req.Header.Set("X-Client-IP", "203.0.113.7")
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("hello"))
	}, req)

	for _, k := range []string{"level", "time", "pid", "hostname", "status", "length",
		"duration", "ip", "method", "host", "url", "ua"} {
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
	// This binary answers both www.hebcal.com and download.hebcal.com, so the
	// Host header distinguishes them in the shared log.
	if m["host"] != "download.hebcal.com" {
		t.Errorf("host = %v, want the request's Host header", m["host"])
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

// A 4xx/5xx response logs the error it rendered under "msg", so the reason (an
// OutOfRangeError's "No calendar for year 38", a bad location, etc.) is
// searchable in the access log without re-deriving it from the URL. The error
// value is recorded by the write helpers (here directly, via RecordError) onto
// the request's collector and rendered on the log line, not parsed back out of
// the response body.
func TestAccessLogErrorMessage(t *testing.T) {
	// A plain-text error, as the PDF routes write it: the error value is recorded
	// separately from the (here identical) body http.Error writes.
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		RecordError(w, errors.New("No calendar for year 38"))
		http.Error(w, "No calendar for year 38", http.StatusGone)
	}, httptest.NewRequest(http.MethodGet, "/v4/CAEYAUABYCY/hebcal_38.pdf", nil))
	if m["msg"] != "No calendar for year 38" {
		t.Errorf("msg = %v, want the recorded error text", m["msg"])
	}
	if m["status"] != float64(410) {
		t.Errorf("status = %v", m["status"])
	}

	// The JSON APIs go through WriteJSONError, which records the error itself, so
	// msg is the bare message even though the body is {"error": ...}.
	m = serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		WriteJSONError(w, model.BadRequest("bad geonameid"))
	}, httptest.NewRequest(http.MethodGet, "/zmanim?geonameid=x", nil))
	if m["msg"] != "bad geonameid" {
		t.Errorf("msg = %v, want the recorded error message", m["msg"])
	}
}

// A successful response carries no "msg" field.
func TestAccessLogNoMessageOnSuccess(t *testing.T) {
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}, httptest.NewRequest(http.MethodGet, "/x", nil))
	if _, ok := m["msg"]; ok {
		t.Errorf("msg present on a 200 response")
	}
}

// A backend sub-request made while serving the request is folded into the
// request's own log line as a nested "subreq" object, not logged separately.
func TestAccessLogFoldsSubrequest(t *testing.T) {
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		reqlog.FromContext(r.Context()).Add(reqlog.Call{
			Status:   200,
			URL:      "/leyning?end=2026-08-08&start=2026-08-08",
			Duration: 3500 * time.Microsecond,
			Length:   842,
		})
		w.Write([]byte("ok"))
	}, httptest.NewRequest(http.MethodGet, "/shabbat?geonameid=3448439", nil))

	rs, ok := m["subreq"].(map[string]any)
	if !ok {
		t.Fatalf("subreq = %v (%T), want a nested object", m["subreq"], m["subreq"])
	}
	// Duration is a float in milliseconds, so sub-millisecond calls keep their
	// resolution: 3500µs is logged as 3.5, not truncated to 3.
	if rs["status"] != float64(200) || rs["length"] != float64(842) ||
		rs["duration"] != float64(3.5) || rs["url"] != "/leyning?end=2026-08-08&start=2026-08-08" {
		t.Errorf("subreq = %#v", rs)
	}
}

// Several sub-requests in one request are logged as a JSON array under the same
// key.
func TestAccessLogFoldsMultipleSubrequests(t *testing.T) {
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		c := reqlog.FromContext(r.Context())
		c.Add(reqlog.Call{Status: 200, URL: "/leyning?x=1", Length: 10})
		c.Add(reqlog.Call{Status: 200, URL: "/learning?y=2", Length: 20})
		w.Write([]byte("ok"))
	}, httptest.NewRequest(http.MethodGet, "/x", nil))

	arr, ok := m["subreq"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("subreq = %v, want a 2-element array", m["subreq"])
	}
}

// A request that makes no sub-request has no such field.
func TestAccessLogOmitsSubreqWhenUnused(t *testing.T) {
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}, httptest.NewRequest(http.MethodGet, "/x", nil))
	if _, ok := m["subreq"]; ok {
		t.Errorf("subreq present on a request that made no call")
	}
}

// A /v4/ PDF request records the query string its opaque base64 protobuf decodes
// to, so the request is readable and reproducible under "qs". A request that
// records none has no such field.
func TestAccessLogQueryString(t *testing.T) {
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		reqlog.FromContext(r.Context()).SetQuery("v=1&M=on&yt=H&year=5787&lg=s&dksa=on&mm=0")
		w.Write([]byte("ok"))
	}, httptest.NewRequest(http.MethodGet, "/v4/QAFIAWCbLfgDAQ/hebcal_5787h.pdf", nil))
	if m["qs"] != "v=1&M=on&yt=H&year=5787&lg=s&dksa=on&mm=0" {
		t.Errorf("qs = %v, want the recorded query string", m["qs"])
	}

	m = serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}, httptest.NewRequest(http.MethodGet, "/x", nil))
	if _, ok := m["qs"]; ok {
		t.Errorf("qs present on a request that recorded none")
	}
}

// A POST /mcp request records its JSON-RPC body, which the log emits under
// "postBody" as a nested object (compacted from whatever whitespace the client
// sent), so the log shows which tool was called with what arguments. A request
// that records none has no such field.
func TestAccessLogPostBody(t *testing.T) {
	m := serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		reqlog.FromContext(r.Context()).SetPostBody([]byte(
			"{\n  \"jsonrpc\": \"2.0\",\n  \"id\": 1,\n  \"method\": \"tools/call\",\n" +
				"  \"params\": {\"name\": \"torah-portion\"}\n}"))
		w.Write([]byte("ok"))
	}, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	pb, ok := m["postBody"].(map[string]any)
	if !ok {
		t.Fatalf("postBody = %v (%T), want a nested object", m["postBody"], m["postBody"])
	}
	if pb["method"] != "tools/call" || pb["id"] != float64(1) {
		t.Errorf("postBody = %#v", pb)
	}

	m = serveAndReadLog(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}, httptest.NewRequest(http.MethodGet, "/x", nil))
	if _, ok := m["postBody"]; ok {
		t.Errorf("postBody present on a request that recorded none")
	}
}

// A non-JSON or over-cap body is logged as a bounded JSON string, so the log
// line stays valid JSON regardless of what the client posted.
func TestEncodePostBody(t *testing.T) {
	// Valid JSON is compacted to a nested value.
	if got := encodePostBody([]byte(`{"a": 1,  "b": [2, 3]}`)); string(got) != `{"a":1,"b":[2,3]}` {
		t.Errorf("valid JSON: got %s", got)
	}
	// Non-JSON becomes a quoted string, keeping the log line valid.
	if got := encodePostBody([]byte("not json")); string(got) != `"not json"` {
		t.Errorf("non-JSON: got %s", got)
	}
	// No body recorded: field omitted.
	if got := encodePostBody(nil); got != nil {
		t.Errorf("nil body: got %s, want nil", got)
	}
	// An over-cap body is truncated and emitted as a string (still valid JSON).
	big := make([]byte, maxLoggedPostBody+100)
	for i := range big {
		big[i] = 'x'
	}
	got := encodePostBody(big)
	if !json.Valid(got) {
		t.Errorf("over-cap body did not produce valid JSON: %s", got[:32])
	}
	var s string
	if err := json.Unmarshal(got, &s); err != nil || len(s) != maxLoggedPostBody {
		t.Errorf("over-cap body: len=%d err=%v, want a %d-char string", len(s), err, maxLoggedPostBody)
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
