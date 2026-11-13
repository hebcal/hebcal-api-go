package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/logger"
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
		"/ical/", "/ical/missing.ics", "/ical/sub", "/ical/sub/omer.ics",
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

// A path that climbs out of the document root or names a dot file is a 403
// on every route, answered before the mux can redirect a ".." path to its
// cleaned form, and never cached.
func TestDotSegmentsForbidden(t *testing.T) {
	app, srv := testServer(t)
	app.DocumentRoot = staticDir(t, time.Now())
	for _, p := range []string{
		"/../../etc/passwd", "/..", "/%2e%2e/%2e%2e/etc/passwd", "/..%2f..%2fetc/passwd",
		"/..\\..\\etc\\passwd", "/.env", "/.git/config", "/.well-known/security.txt",
		"/ical/.hidden.ics", "/ical/..%2fomer.ics", "/ical/%2e%2e/etc/passwd",
		"/ical/..%2fping", "/ical/%2e%2e%2fping", "/ical/../ping", "/v4/x/../../.env",
		"/static/./favicon.ico",
	} {
		resp, body := get(t, srv, p)
		if resp.StatusCode != http.StatusForbidden || body != "Forbidden\n" {
			t.Errorf("%s: status %d body %q, want 403", p, resp.StatusCode, body)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("%s: 403 carries Cache-Control %q", p, cc)
		}
	}
	// A dot inside a segment is fine.
	for _, p := range []string{"/favicon.ico", "/ical/omer.ics"} {
		if resp, _ := get(t, srv, p); resp.StatusCode == http.StatusForbidden {
			t.Errorf("%s: 403", p)
		}
	}
}

// serveStatic opens files through an os.Root, so neither a path that climbs
// out nor a symlink pointing outside DocumentRoot is served, even when a
// caller passes it unchecked.
func TestServeStaticContained(t *testing.T) {
	app, _ := testServer(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.DocumentRoot = staticDir(t, time.Now())
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(app.DocumentRoot, "link")); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(app.DocumentRoot, filepath.Join(outside, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"link", filepath.ToSlash(rel), "../secret", "/etc/passwd"} {
		rec := httptest.NewRecorder()
		app.serveStatic(rec, httptest.NewRequest(http.MethodGet, "/x", nil), p, httpx.CacheControl1Day)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "root:") {
			t.Errorf("%s: status %d body %q, want 404", p, rec.Code, rec.Body.String())
		}
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

// /favicon.ico is the file at the document root, cached for a year as
// immutable, and conditional on If-Modified-Since. Without the file it is a
// 404 that carries no Cache-Control.
func TestFavicon(t *testing.T) {
	mtime := time.Date(2026, time.November, 8, 23, 57, 0, 0, time.UTC)
	root := t.TempDir()
	icon, err := os.ReadFile(filepath.Join("..", "..", "static", "favicon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "favicon.ico")
	if err := os.WriteFile(p, icon, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(faviconApp(root).Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != string(icon) {
		t.Fatalf("status %d, %d bytes, want 200 and %d", resp.StatusCode, len(body), len(icon))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "icon") {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != httpx.CacheControl1Year+", immutable" {
		t.Errorf("Cache-Control %q", cc)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/favicon.ico", nil)
	req.Header.Set("If-Modified-Since", mtime.Format(http.TimeFormat))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("conditional: status %d, want 304", resp.StatusCode)
	}

	empty := httptest.NewServer(faviconApp(t.TempDir()).Routes())
	defer empty.Close()
	resp, err = http.Get(empty.URL + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 || resp.Header.Get("Cache-Control") != "" {
		t.Errorf("missing: status %d, Cache-Control %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
}

func faviconApp(root string) *Server {
	app := New(logger.NewWriter(io.Discard, "test"))
	app.DocumentRoot = root
	return app
}
