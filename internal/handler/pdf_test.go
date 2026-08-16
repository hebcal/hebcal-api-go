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
	"slices"
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

// The legacy /v2/h/<base64-querystring>/<name>.pdf URLs, which hebcal-web
// answers with a 301 to the /v4/ form. This service renders them directly, so
// the test that matters is that the two spellings of one request draw the same
// calendar; that they decode to the same protobuf as hebcal-web's redirect is
// internal/service/pdf/v2_test.go's business.
func TestPDFLegacyV2(t *testing.T) {
	// The shape of a real request from a download.hebcal.com access log, moved
	// onto a city the trimmed test database has, and the /v4/ payload
	// hebcal-web's 301 points at for it.
	const (
		legacy = "/v2/h/dj0xJmdlb25hbWVpZD00OTMwOTU2Jm09NTAmeWVhcj0yMDIxJmM9MSZzPTEm" +
			"bWFqPTEmbWluPTEmbW9kPTEmbWY9MSZzcz0xJm54PTE/hebcal_2021_boston.pdf"
		modern = "/v4/CAEQARgBIAEoATABUAFYjPusAmDlD3AyiAEB/hebcal_2021_boston.pdf"
	)
	app, srv := pdfServer(t)
	app.PDF.Geo = testGeoDB(t)

	resp, body := get(t, srv, legacy)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	// The legacy URL is a download like any other, so it carries the download
	// path's headers rather than a redirect's.
	if got := resp.Header.Get("Cache-Control"); got != httpx.CacheControl14Days {
		t.Errorf("Cache-Control = %q, want %q", got, httpx.CacheControl14Days)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type = %q", got)
	}
	if !strings.HasPrefix(body, "%PDF-") {
		t.Fatal("body is not a PDF")
	}

	// Same calendar as the /v4/ URL production redirects to. The documents are
	// not byte-identical -- each carries its own creation date -- so compare
	// the drawn content: every link, which encodes the events and their dates.
	_, v4body := get(t, srv, modern)
	links := func(doc string) []string {
		return regexp.MustCompile(`https://hebcal\.com/[^\s()>]*`).
			FindAllString(string(inflateAll([]byte(doc))), -1)
	}
	got, want := links(body), links(v4body)
	if len(want) == 0 {
		t.Fatal("the /v4/ calendar drew no links; the comparison proves nothing")
	}
	if !slices.Equal(got, want) {
		t.Errorf("the legacy URL drew %d links, the /v4/ one %d; first difference at %d",
			len(got), len(want), firstDiff(got, want))
	}
}

// firstDiff reports the index of the first element the two slices disagree on.
func firstDiff(a, b []string) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// Only /v2/h/<...>.pdf belongs to this service. Everything else Varnish might
// send here is refused with the status hebcal-web's download dispatcher gives
// it: a URL that is not a PDF download at all is 404, and one whose v= names
// another kind of calendar is 400.
func TestPDFLegacyV2Errors(t *testing.T) {
	enc := func(qs string) string {
		return strings.TrimRight(base64.StdEncoding.EncodeToString([]byte(qs)), "=")
	}
	tests := []struct {
		name string
		path string
		want int
	}{
		{"an ics feed", "/v2/h/" + enc("v=1&year=2026&maj=on") + "/hebcal.ics", http.StatusNotFound},
		{"a yahrzeit calendar", "/v2/y/" + enc("v=yahrzeit") + "/yahrzeit.pdf", http.StatusNotFound},
		{"no v at all", "/v2/h/" + enc("year=2026&maj=on") + "/x.pdf", http.StatusNotFound},
		{"not base64", "/v2/h/!!!!/x.pdf", http.StatusNotFound},
		{"a yahrzeit v=", "/v2/h/" + enc("v=yahrzeit&y1=1990") + "/x.pdf", http.StatusBadRequest},
		{"an out-of-range year", "/v2/h/" + enc("v=1&year=9999&maj=on") + "/x.pdf", http.StatusGone},
	}
	_, srv := pdfServerNoFonts(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _ := get(t, srv, tt.path)
			if resp.StatusCode != tt.want {
				t.Errorf("%s: status = %d, want %d", tt.path, resp.StatusCode, tt.want)
			}
		})
	}
}

// The two location forms downloadHref2() has no branch for, so hebcal-web's
// 301 loses them. They are resolved here (see applyV2Location), which is what
// these URLs drew before redirV2 was added, and the check that matters at this
// level is that the location actually reaches the rendered document: a
// calendar with no location is titled "Hebcal Diaspora" and carries no times.
func TestPDFLegacyV2LocationForms(t *testing.T) {
	enc := func(qs string) string {
		return "/v2/h/" + strings.TrimRight(base64.StdEncoding.EncodeToString([]byte(qs)), "=") +
			"/x.pdf"
	}
	app, srv := pdfServer(t)
	app.PDF.Geo = testGeoDB(t)

	t.Run("a legacy city identifier", func(t *testing.T) {
		resp, body := get(t, srv, enc("v=1&city=AU-Melbourne&year=2026&c=on&maj=on&s=on"))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}
		// The title comes from the resolved location, not from the lookup key:
		// "AU-Melbourne" names a row, getCalendarTitle names a city.
		if want := "Hebcal Melbourne 2026"; !strings.Contains(pdfTitle(t, body), want) {
			t.Errorf("title = %q, want %q", pdfTitle(t, body), want)
		}
	})

	t.Run("degrees, minutes and a direction", func(t *testing.T) {
		resp, body := get(t, srv, enc("v=1&ladeg=40&lamin=42&ladir=n&lodeg=74&lomin=0&"+
			"lodir=w&tzid=America/New_York&year=2026&c=on&maj=on&s=on"))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
		}
		// With no city-typeahead the location names itself by its coordinates.
		if title := pdfTitle(t, body); !strings.Contains(title, "40") ||
			strings.Contains(title, "Diaspora") {
			t.Errorf("title = %q, want the coordinates rather than Diaspora", title)
		}
		// The campaign is that name run through makeAnchor, so the punctuation
		// is hyphenated rather than surviving to be percent-encoded.
		all := string(inflateAll([]byte(body)))
		if want := "uc=pdf-40-42-n-74-0-w-america-new_york-2026"; !strings.Contains(all, want) {
			t.Errorf("no link carries %s", want)
		}
	})

	t.Run("an unresolvable city is 404 and an impossible degree 400", func(t *testing.T) {
		resp, _ := get(t, srv, enc("v=1&city=Nowhereville&year=2026&c=on&maj=on"))
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("unknown city status = %d, want 404", resp.StatusCode)
		}
		resp, _ = get(t, srv, enc("v=1&ladeg=99&lamin=42&ladir=n&lodeg=74&lomin=0&"+
			"lodir=w&tzid=America/New_York&year=2026&c=on&maj=on"))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("ladeg=99 status = %d, want 400", resp.StatusCode)
		}
	})
}

// The classic /hebcal/index.cgi/<name>.pdf?<query> download URL, older than
// both /v2/ and /v4/. Like the /v2/ test, what matters here is that it draws
// the same calendar as the /v4/ URL it corresponds to, and that the two legacy
// query encodings (plain, and the doubly-encoded form fixup2 unescapes) reach
// the same result. The protobuf-level agreement is cgi_test.go's business.
func TestPDFLegacyCGI(t *testing.T) {
	app, srv := pdfServer(t)
	app.PDF.Geo = testGeoDB(t)

	// The same request as TestPDFLegacyV2 (Boston 2021), carried in the CGI
	// query string rather than a base64 path, and the /v4/ payload it matches.
	const (
		query  = "v=1&geonameid=4930956&m=50&year=2021&c=1&s=1&maj=1&min=1&mod=1&mf=1&ss=1&nx=1"
		cgi    = "/hebcal/index.cgi/hebcal_2021_boston.pdf?" + query
		modern = "/v4/CAEQARgBIAEoATABUAFYjPusAmDlD3AyiAEB/hebcal_2021_boston.pdf"
	)
	links := func(doc string) []string {
		return regexp.MustCompile(`https://hebcal\.com/[^\s()>]*`).
			FindAllString(string(inflateAll([]byte(doc))), -1)
	}

	resp, body := get(t, srv, cgi)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	// A CGI download carries the download path's headers, not a redirect's.
	if got := resp.Header.Get("Cache-Control"); got != httpx.CacheControl14Days {
		t.Errorf("Cache-Control = %q, want %q", got, httpx.CacheControl14Days)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type = %q", got)
	}

	_, v4body := get(t, srv, modern)
	got, want := links(body), links(v4body)
	if len(want) == 0 {
		t.Fatal("the /v4/ calendar drew no links; the comparison proves nothing")
	}
	if !slices.Equal(got, want) {
		t.Errorf("the CGI URL drew %d links, the /v4/ one %d; first difference at %d",
			len(got), len(want), firstDiff(got, want))
	}

	// The doubly-encoded spelling of the same request (its separators as %3B and
	// its '=' as %3D, the form fixup2 detects by the dl=1%3B prefix) must render
	// the identical calendar.
	semi := "dl=1;" + strings.ReplaceAll(query, "&", ";")
	i := strings.Index(semi, "=")
	doubled := semi[:i+1] + strings.NewReplacer(";", "%3B", "=", "%3D").Replace(semi[i+1:])
	resp, dblbody := get(t, srv, "/hebcal/index.cgi/hebcal_2021_boston.pdf?"+doubled)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("doubly-encoded status = %d, want 200: %s", resp.StatusCode, dblbody)
	}
	if !slices.Equal(links(dblbody), got) {
		t.Errorf("the doubly-encoded URL drew a different calendar than the plain one")
	}
}

// A CGI URL that is not a PDF download, or that names no download version, is
// refused with the status hebcal-web's router gives it: 404 for a request that
// is not a download at all (v=undefined, or a non-.pdf), 400 for a v= naming
// another kind of calendar.
func TestPDFLegacyCGIErrors(t *testing.T) {
	_, srv := pdfServerNoFonts(t)
	tests := []struct {
		name, path string
		want       int
	}{
		{"no query at all", "/hebcal/index.cgi/hebcal_2028_may.pdf", http.StatusNotFound},
		{"no v", "/hebcal/index.cgi/x.pdf?year=2026&maj=on", http.StatusNotFound},
		{"an ics feed", "/hebcal/index.cgi/hebcal.ics?v=1&year=2026&maj=on", http.StatusNotFound},
		{"a yahrzeit v=", "/hebcal/index.cgi/x.pdf?v=yahrzeit&y1=1990", http.StatusBadRequest},
		{"an out-of-range year", "/hebcal/index.cgi/x.pdf?v=1&year=9999&maj=on", http.StatusGone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _ := get(t, srv, tt.path)
			if resp.StatusCode != tt.want {
				t.Errorf("%s: status = %d, want %d", tt.path, resp.StatusCode, tt.want)
			}
		})
	}
}

// pdfTitle reads the document's /Title string, which the renderer writes as
// UTF-16BE when it is not pure ASCII.
func pdfTitle(t *testing.T, doc string) string {
	t.Helper()
	m := regexp.MustCompile(`/Title\s*\(([^)]*)\)`).FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("no /Title in the document")
	}
	raw := m[1]
	if !strings.HasPrefix(raw, "\xfe\xff") {
		return raw
	}
	var b strings.Builder
	for i := 2; i+1 < len(raw); i += 2 {
		b.WriteRune(rune(raw[i])<<8 | rune(raw[i+1]))
	}
	return b.String()
}
