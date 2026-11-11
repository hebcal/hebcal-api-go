package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
)

// staticDir makes a document root whose ical/ holds omer.ics with brotli and
// gzip copies, and omer.csv with a gzip copy only, every file stamped mtime,
// as cmd/hebcal-static-ical leaves them. The "compressed" copies are marker
// text: what matters is which file is sent. A file at the root itself must
// not be reachable through /ical/.
func staticDir(t *testing.T, mtime time.Time) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ping"), []byte("root file"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "ical")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"omer.ics":    "BEGIN:VCALENDAR\r\n" + strings.Repeat("X-FILLER:padding the body past the compression threshold\r\n", 20) + "END:VCALENDAR\r\n",
		"omer.ics.br": "brotli copy",
		"omer.ics.gz": "gzip copy",
		"omer.csv":    "\xEF\xBB\xBFSubject,Start Date\r\n",
		"omer.csv.gz": "gzip csv copy",
		".hidden.ics": "secret",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestStaticICalPrecompressed(t *testing.T) {
	mtime := time.Date(2026, time.November, 8, 23, 57, 0, 0, time.UTC)
	app, srv := testServer(t)
	app.DocumentRoot = staticDir(t, mtime)
	const lastMod = "Sun, 08 Nov 2026 23:57:00 GMT"

	tests := []struct {
		path, accept      string
		wantEnc, wantBody string
		wantType          string
	}{
		{"/ical/omer.ics", "gzip, deflate, br", "br", "brotli copy", "text/calendar; charset=utf-8"},
		{"/ical/omer.ics", "zstd, br", "br", "brotli copy", "text/calendar; charset=utf-8"},
		{"/ical/omer.ics", "br;q=0, gzip", "gzip", "gzip copy", "text/calendar; charset=utf-8"},
		{"/ical/omer.ics", "gzip", "gzip", "gzip copy", "text/calendar; charset=utf-8"},
		{"/ical/omer.ics", "*", "br", "brotli copy", "text/calendar; charset=utf-8"},
		{"/ical/omer.csv", "br, gzip", "gzip", "gzip csv copy", "text/csv; charset=utf-8"},
		{"/ical/omer.csv", "identity", "", "\xEF\xBB\xBFSubject,Start Date\r\n", "text/csv; charset=utf-8"},
	}
	for _, tt := range tests {
		resp, body := get(t, srv, tt.path, "Accept-Encoding", tt.accept)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s [%s]: status %d", tt.path, tt.accept, resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Encoding"); got != tt.wantEnc {
			t.Errorf("%s [%s]: Content-Encoding %q, want %q", tt.path, tt.accept, got, tt.wantEnc)
		}
		if tt.wantEnc != "" && body != tt.wantBody {
			t.Errorf("%s [%s]: body %q, want %q", tt.path, tt.accept, body, tt.wantBody)
		}
		if got := resp.Header.Get("Content-Type"); got != tt.wantType {
			t.Errorf("%s: Content-Type %q, want %q", tt.path, got, tt.wantType)
		}
		if got := resp.Header.Get("Last-Modified"); got != lastMod {
			t.Errorf("%s: Last-Modified %q, want %q", tt.path, got, lastMod)
		}
		if got := resp.Header.Get("Cache-Control"); got != "public, max-age=604800, s-maxage=604800" {
			t.Errorf("%s: Cache-Control %q", tt.path, got)
		}
		if tt.wantEnc != "" && resp.Header.Get("Vary") != "Accept-Encoding" {
			t.Errorf("%s [%s]: Vary %q, want Accept-Encoding", tt.path, tt.accept, resp.Header.Get("Vary"))
		}
	}

	// A client that takes neither copy gets the file itself, compressed on
	// the fly when it asks for an encoding with no copy on disk.
	resp, body := get(t, srv, "/ical/omer.ics", "Accept-Encoding", "identity")
	if resp.Header.Get("Content-Encoding") != "" || !strings.HasPrefix(body, "BEGIN:VCALENDAR") {
		t.Errorf("identity: Content-Encoding %q, body %.20q", resp.Header.Get("Content-Encoding"), body)
	}
	resp, _ = get(t, srv, "/ical/omer.ics", "Accept-Encoding", "zstd")
	if resp.Header.Get("Content-Encoding") != "zstd" {
		t.Errorf("zstd only: Content-Encoding %q, want zstd on the fly", resp.Header.Get("Content-Encoding"))
	}
}

func TestStaticICalConditional(t *testing.T) {
	mtime := time.Date(2026, time.November, 8, 23, 57, 0, 0, time.UTC)
	app, srv := testServer(t)
	app.DocumentRoot = staticDir(t, mtime)

	tests := []struct {
		headers []string
		want    int
	}{
		{[]string{"If-Modified-Since", "Sun, 08 Nov 2026 23:57:00 GMT"}, http.StatusNotModified},
		{[]string{"If-Modified-Since", "Mon, 09 Nov 2026 10:00:00 GMT"}, http.StatusNotModified},
		{[]string{"If-Modified-Since", "Sun, 08 Nov 2026 23:56:59 GMT"}, http.StatusOK},
		{[]string{"If-Modified-Since", "Sun, 08 Nov 2026 23:57:00 GMT", "Cache-Control", "no-cache"}, http.StatusOK},
		{[]string{"If-Modified-Since", "Sun, 08 Nov 2026 23:57:00 GMT", "If-None-Match", `W/"x"`}, http.StatusOK},
		{[]string{"If-Modified-Since", "garbage"}, http.StatusOK},
	}
	for _, tt := range tests {
		resp, _ := get(t, srv, "/ical/omer.ics", append([]string{"Accept-Encoding", "br"}, tt.headers...)...)
		if resp.StatusCode != tt.want {
			t.Errorf("%v: status %d, want %d", tt.headers, resp.StatusCode, tt.want)
		}
	}
}

func TestStaticICalNotFound(t *testing.T) {
	app, srv := testServer(t)
	app.DocumentRoot = staticDir(t, time.Now())
	for _, p := range []string{
		"/ical/", "/ical/missing.ics", "/ical/.hidden.ics", "/ical/sub",
		"/ical/sub/omer.ics", "/ical/..%2fomer.ics", "/ical/%2e%2e/etc/passwd",
		"/ical/..%2fping", "/ical/%2e%2e%2fping",
	} {
		resp, _ := get(t, srv, p)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, resp.StatusCode)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("%s: 404 carries Cache-Control %q", p, cc)
		}
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/ical/omer.ics", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: status %d, Allow %q", resp.StatusCode, resp.Header.Get("Allow"))
	}

	app.DocumentRoot = ""
	if resp, _ := get(t, srv, "/ical/omer.ics"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("no document root configured: status %d, want 404", resp.StatusCode)
	}
}

func TestAcceptsEncoding(t *testing.T) {
	tests := []struct {
		header, coding string
		want           bool
	}{
		{"gzip, deflate, br", "br", true},
		{"gzip, deflate", "br", false},
		{"br;q=0", "br", false},
		{"br; q=0.5", "br", true},
		{"BR", "br", true},
		{"*", "gzip", true},
		{"*;q=0", "gzip", false},
		{"*, br;q=0", "br", false},
		{"", "gzip", false},
		{"identity", "gzip", false},
	}
	for _, tt := range tests {
		if got := acceptsEncoding(tt.header, tt.coding); got != tt.want {
			t.Errorf("acceptsEncoding(%q, %q) = %v, want %v", tt.header, tt.coding, got, tt.want)
		}
	}
}

// serveStatic is not tied to /ical/: a route for a file at the document root
// itself (a favicon, say) passes the path and its own lifetime.
func TestServeStaticAtRoot(t *testing.T) {
	mtime := time.Date(2026, time.November, 8, 23, 57, 0, 0, time.UTC)
	app := &Server{DocumentRoot: staticDir(t, mtime)}
	rec := httptest.NewRecorder()
	app.serveStatic(rec, httptest.NewRequest(http.MethodGet, "/ping", nil), "ping", httpx.CacheControl1Day)
	if rec.Code != http.StatusOK || rec.Body.String() != "root file" {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != httpx.CacheControl1Day {
		t.Errorf("Cache-Control %q, want the caller's", got)
	}
}
