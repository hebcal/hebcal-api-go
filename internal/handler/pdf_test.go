package handler

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings/readingstest"
	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// The transport half of the PDF calendars: status codes, cache headers, the
// conditional request, and the two daily-learning failure modes. What each
// calendar draws is internal/service/pdf's business, not this file's.

// fontDir is where the Source Sans Pro and Adobe Hebrew families live: $FONT_DIR
// if it is set, otherwise the repo root's fonts/, which is a symlink to
// hebcal-web's copy. Tests that render skip when it is absent rather than
// failing on a fresh checkout.
var fontDir = func() string {
	if dir := os.Getenv("FONT_DIR"); dir != "" {
		return dir
	}
	return filepath.Join("..", "..", "fonts")
}()

// pdfServer returns a server whose PDF routes render for real, skipping when
// the fonts are absent.
func pdfServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	if _, err := os.Stat(fontDir); err != nil {
		t.Skipf("no %s directory; skipping tests that need real fonts", fontDir)
	}
	fonts, err := pdf.LoadFonts(fontDir)
	if err != nil {
		t.Fatalf("LoadFonts: %v", err)
	}
	app, srv := testServer(t)
	app.PDF.Renderer = pdf.NewRenderer(fonts)
	return app, srv
}

// pdfServerNoFonts returns a server that can answer everything a PDF request
// decides before rendering -- the 404s, the 410 and the parse errors -- without
// needing the font files.
func pdfServerNoFonts(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	app := New(logger.NewWriter(io.Discard, "test"))
	// A Renderer with no fonts is enough to pass the availability check;
	// nothing in these tests reaches the point of drawing with it, and if
	// something ever does, Render reports the missing fonts rather than
	// panicking.
	app.PDF.Renderer = &pdf.Renderer{}
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	return app, srv
}

// testGeoDB opens the trimmed geonames/zips databases the handler tests share.
func testGeoDB(t *testing.T) *geodb.DB {
	t.Helper()
	db, err := geodb.New("testdata/zips.sqlite3", "testdata/geonames.sqlite3")
	if err != nil {
		t.Skipf("opening geo databases: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// encode builds the base64 protobuf payload a /v4/ URL carries.
func encode(t *testing.T, msg *pb.Download) string {
	t.Helper()
	raw, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("+", "-", "/", "_", "=", "").Replace(
		base64.StdEncoding.EncodeToString(raw))
}

// inflateAll returns the document's raw bytes plus every stream it can
// decompress, which is where the link annotations' URIs end up.
func inflateAll(doc []byte) []byte {
	var out bytes.Buffer
	out.Write(doc)
	for _, m := range regexp.MustCompile(`stream\r?\n`).FindAllIndex(doc, -1) {
		end := bytes.Index(doc[m[1]:], []byte("endstream"))
		if end < 0 {
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader(doc[m[1] : m[1]+end]))
		if err != nil {
			continue
		}
		var dec bytes.Buffer
		if _, err := dec.ReadFrom(zr); err == nil {
			out.Write(dec.Bytes())
		}
		zr.Close()
	}
	return out.Bytes()
}

// The PDF response matches hebcal-web's cache headers: a 14-day Cache-Control,
// CORS and nosniff on a rendered calendar, and a weak ETag that answers a
// conditional request with 304 before the render runs. The out-of-range 410 is
// cacheable too (that year never comes into range); the unknown-location 404 is
// deliberately not (a location may be added later).
func TestPDFCacheHeaders(t *testing.T) {
	// 410 is decided while decoding the request, before any rendering, so this
	// needs no fonts.
	t.Run("410 out of range is cacheable", func(t *testing.T) {
		_, srv := pdfServerNoFonts(t)
		msg := &pb.Download{Year: 9999, Major: true}
		resp, _ := get(t, srv, "/v4/"+encode(t, msg)+"/x.pdf")
		if resp.StatusCode != http.StatusGone {
			t.Fatalf("status = %d, want 410", resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != httpx.CacheControl14Days {
			t.Errorf("Cache-Control = %q, want %q", got, httpx.CacheControl14Days)
		}
	})

	t.Run("404 unknown location is not cacheable", func(t *testing.T) {
		// A real geo lookup that misses is the only way to reach the 404 path;
		// with no database an unknown id falls through to a 400 instead.
		app, srv := pdfServerNoFonts(t)
		app.PDF.Geo = testGeoDB(t)
		msg := &pb.Download{Year: 2026, Major: true, Geonameid: 999999999}
		resp, _ := get(t, srv, "/v4/"+encode(t, msg)+"/x.pdf")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q, want none on a 404", got)
		}
	})

	t.Run("200 sets cache headers and honors If-None-Match", func(t *testing.T) {
		_, srv := pdfServer(t)
		msg := &pb.Download{Year: 2026, Major: true}
		path := "/v4/" + encode(t, msg) + "/x.pdf"

		resp, body := get(t, srv, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}
		if got := resp.Header.Get("Cache-Control"); got != httpx.CacheControl14Days {
			t.Errorf("Cache-Control = %q, want %q", got, httpx.CacheControl14Days)
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
			t.Errorf("Content-Type = %q", got)
		}
		// The PDF routes go through the shared middleware, so they carry the
		// response-time header (and the request counter and access log) every
		// other route does.
		if !strings.HasSuffix(resp.Header.Get("X-Response-Time"), "ms") {
			t.Errorf("X-Response-Time = %q, want a duration in ms", resp.Header.Get("X-Response-Time"))
		}
		if !strings.HasPrefix(body, "%PDF-") {
			t.Error("body is not a PDF")
		}
		etag := resp.Header.Get("ETag")
		if etag == "" {
			t.Fatal("missing ETag")
		}

		// A matching If-None-Match is answered 304 with no body.
		resp2, body2 := get(t, srv, path, "If-None-Match", etag)
		if resp2.StatusCode != http.StatusNotModified {
			t.Errorf("conditional request status = %d, want 304", resp2.StatusCode)
		}
		if len(body2) != 0 {
			t.Errorf("304 should have no body, got %d bytes", len(body2))
		}

		// A stale If-None-Match still renders, so 304 is not returned blindly.
		resp3, body3 := get(t, srv, path, "If-None-Match", `W/"stale"`)
		if resp3.StatusCode != http.StatusOK {
			t.Errorf("stale If-None-Match status = %d, want 200", resp3.StatusCode)
		}
		if len(body3) == 0 {
			t.Error("stale If-None-Match should return the rendered PDF")
		}
	})
}

// The two daily-learning failure modes are different and the status codes have
// to say so: a build with no readings-svc socket can never render these, while
// a configured readings-svc that does not answer is transient.
func TestFallbackStatusCodes(t *testing.T) {
	// A calendar naming one of the six, with nothing else that needs geo.
	msg := &pb.Download{Year: 2026, Month: 8, Major: true, DirshuAmudYomi: true}
	path := "/v4/" + encode(t, msg) + "/hebcal_2026.pdf"

	t.Run("no readings-svc configured is 501", func(t *testing.T) {
		_, srv := pdfServer(t)
		resp, _ := get(t, srv, path)
		if resp.StatusCode != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", resp.StatusCode)
		}
		if resp.Header.Get("X-Unsupported-Series") == "" {
			t.Error("X-Unsupported-Series should name what is missing")
		}
	})

	t.Run("readings-svc unreachable is 503", func(t *testing.T) {
		down := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "down", http.StatusBadGateway)
		}))
		app, srv := pdfServer(t)
		app.PDF.Learning = pdf.NewLearningFetcher(down)
		resp, _ := get(t, srv, path)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", resp.StatusCode)
		}
		if resp.Header.Get("Retry-After") == "" {
			t.Error("a 503 should say when to retry")
		}
	})

	t.Run("readings-svc answering renders", func(t *testing.T) {
		up := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":[{"title":"Yoma 49a","date":"2026-08-01",` +
				`"category":"dirshuAmudYomi"}]}`))
		}))
		app, srv := pdfServer(t)
		app.PDF.Learning = pdf.NewLearningFetcher(up)
		resp, body := get(t, srv, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
			t.Errorf("Content-Type = %q", ct)
		}
	})
}

// The holiday calendars are cached for 60 days rather than the download path's
// 14, carry nosniff but no CORS header (www.hebcal.com sets that only on the
// cfg= API responses), and answer a conditional request before rendering. The
// refusals are not cached: holidayPdf.js throws before it sets Cache-Control.
func TestHolidayPDFResponse(t *testing.T) {
	_, srv := pdfServer(t)
	const path = "/holidays/hebcal-2026.pdf"

	resp, body := get(t, srv, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, wanted 200: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Cache-Control"); got != httpx.CacheControl60Days {
		t.Errorf("Cache-Control = %q, want %q", got, httpx.CacheControl60Days)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want none on www", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type = %q", got)
	}
	if !strings.HasPrefix(body, "%PDF-") {
		t.Error("body is not a PDF")
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	resp2, body2 := get(t, srv, path, "If-None-Match", etag)
	if resp2.StatusCode != http.StatusNotModified || len(body2) != 0 {
		t.Errorf("conditional request = %d with %d bytes, want 304 and none",
			resp2.StatusCode, len(body2))
	}

	// A different Israel setting is a different calendar, so it must not answer
	// 304 to the first one's tag.
	respIL, _ := get(t, srv, path+"?i=on", "If-None-Match", etag)
	if respIL.StatusCode != http.StatusOK {
		t.Errorf("i=on with the diaspora ETag = %d, want 200", respIL.StatusCode)
	}
}

func TestHolidayPDFErrors(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{"/holidays/hebcal-3000.pdf", http.StatusGone},
		{"/holidays/hebcal-0.pdf", http.StatusBadRequest},
		{"/holidays/hebcal-foo.pdf", http.StatusNotFound},
		{"/holidays/2026", http.StatusNotFound}, // an HTML page, still served by Node
		{"/holidays/", http.StatusNotFound},
	}
	_, srv := pdfServerNoFonts(t)
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp, _ := get(t, srv, tt.path)
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
			if got := resp.Header.Get("Cache-Control"); got != "" {
				t.Errorf("Cache-Control = %q, want none on a refusal", got)
			}
		})
	}
}

// Links on a holiday calendar are tagged with the event's own Hebrew year,
// because holidayPdf.js sets no utmCampaign for renderPdfEvent to use. A
// Gregorian year spans two Hebrew years, so both appear.
func TestHolidayPDFPerEventCampaign(t *testing.T) {
	_, srv := pdfServer(t)
	_, body := get(t, srv, "/holidays/hebcal-2026.pdf")
	all := string(inflateAll([]byte(body)))
	for _, want := range []string{"uc=pdf-5786", "uc=pdf-5787"} {
		if !strings.Contains(all, want) {
			t.Errorf("no link carries %s", want)
		}
	}
	if strings.Contains(all, "uc=pdf-diaspora") {
		t.Error("links should not be tagged with the document title's campaign")
	}
}

// Without the fonts the PDF routes report 503 and the rest of the API is
// unaffected, which is why a missing font directory is not fatal at startup.
func TestPDFUnavailableWithoutFonts(t *testing.T) {
	_, srv := testServer(t)
	for _, path := range []string{"/v4/abc/x.pdf", "/holidays/hebcal-2026.pdf"} {
		resp, _ := get(t, srv, path)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", path, resp.StatusCode)
		}
	}
	// ... while a JSON route still answers
	resp, _ := get(t, srv, "/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/converter status = %d, want 200 with the PDF routes disabled", resp.StatusCode)
	}
}
