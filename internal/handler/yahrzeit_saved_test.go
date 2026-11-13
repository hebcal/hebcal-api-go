package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/repository/yahrzeitdb"
)

const (
	goldaID  = "01jthv2t5k88yermamssn96pzf"
	anOffID  = "01jthv2t5k88yermamssn9an0f"
	herzlID  = "01m73gh503ng2gfkfbyt3t0a38"
	chabadID = "01jmt1hef19p1hw6qn48k8p4z9"
)

func savedFixture(t *testing.T, id string) []byte {
	t.Helper()
	b, err := os.ReadFile("../service/yahrzeit/testdata/saved/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// savedServer is a test server whose database holds the saved calendars of
// hebcal-web's test suite plus the two real (public-figure) rows in testdata.
func savedServer(t *testing.T) (*Server, *httptest.Server, *yahrzeitdb.Memory) {
	t.Helper()
	app, srv := testServer(t)
	updated := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mem := &yahrzeitdb.Memory{Rows: map[string]*yahrzeitdb.Row{
		goldaID: {Contents: []byte(`{"x1": "1978-12-08", "n1": "Golda Meir", "t1": "y", "years": 20}`),
			Updated: updated, Downloaded: true},
		anOffID: {Contents: []byte(`{"an": "off", "n1": "Golda Meir", "t1": "y", "x1": "1978-12-08", "years": 20}`),
			Updated: updated, Downloaded: true},
		herzlID:  {Contents: savedFixture(t, herzlID), Updated: updated, Downloaded: false},
		chabadID: {Contents: savedFixture(t, chabadID), Updated: updated, Downloaded: true},
	}}
	app.Yahrzeit = mem
	return app, srv, mem
}

func unfold(s string) string { return strings.ReplaceAll(s, "\r\n ", "") }

func TestYahrzeitSaved(t *testing.T) {
	_, srv, mem := savedServer(t)
	path := "/v3/" + goldaID + "/golda_meir.ics"

	resp, body := get(t, srv, path)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/calendar; charset=utf-8" {
		t.Fatalf("status %d, %s: %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	for _, want := range []string{"BEGIN:VCALENDAR", "Golda Meir", "Golda Meir Yahrzeit reminder",
		"X-WR-RELCALID:hebcal-" + goldaID, "UID:yahrzeit-5787-" + goldaID + "-1\r\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.Contains(unfold(body), "https://www.hebcal.com/yahrzeit/edit/"+goldaID+"#row1") {
		t.Error("no edit link")
	}
	if resp.Header.Get("Cache-Control") != "" || resp.Header.Get("Content-Disposition") != "" {
		t.Errorf("Cache-Control %q, Content-Disposition %q",
			resp.Header.Get("Cache-Control"), resp.Header.Get("Content-Disposition"))
	}

	for _, q := range []string{"?yrem=0", "?yrem=off"} {
		_, body = get(t, srv, path+q)
		if !strings.Contains(body, "Golda Meir") || strings.Contains(body, "Yahrzeit reminder") {
			t.Errorf("%s: reminders not dropped", q)
		}
	}

	// A one-time download: an attachment with no edit links.
	resp, body = get(t, srv, path+"?dl=1")
	if strings.Contains(body, "yahrzeit/edit") {
		t.Error("dl=1 still links to the edit page")
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="golda_meir.ics"` {
		t.Errorf("Content-Disposition %q", cd)
	}

	resp, body = get(t, srv, "/v3/"+goldaID+"/golda_meir.csv?yrem=0")
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "csv") {
		t.Fatalf("csv: status %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, "Golda Meir") || strings.Contains(body, "Yahrzeit reminder") {
		t.Error("csv body")
	}

	resp, _ = get(t, srv, "/v3/"+goldaID+"/golda_meir.pdf")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("pdf: status %d", resp.StatusCode)
	}
	// The .pdf is refused before the lookup, so it is not a download.
	if n := len(mem.Touched()); n != 5 {
		t.Errorf("touched %d times, want 5", n)
	}
}

func TestYahrzeitSavedAnniversaryNumbers(t *testing.T) {
	_, srv, _ := savedServer(t)
	_, body := get(t, srv, "/v3/"+goldaID+"/golda_meir.ics?dl=1")
	if !regexp.MustCompile(`SUMMARY:🕯️ Golda Meir’s \d+(st|nd|rd|th) Yahrzeit`).MatchString(body) {
		t.Error("calendar with no an key lost its numbers")
	}
	_, body = get(t, srv, "/v3/"+anOffID+"/golda_meir.ics?dl=1")
	if !strings.Contains(body, "SUMMARY:🕯️ Golda Meir’s Yahrzeit") ||
		regexp.MustCompile(`Golda Meir’s \d+(st|nd|rd|th) Yahrzeit`).MatchString(body) {
		t.Error("an=off kept the numbers")
	}
	if !strings.Contains(unfold(body), "whose Yahrzeit occurs on") {
		t.Error("an=off memo")
	}
}

func TestYahrzeitSavedRealRows(t *testing.T) {
	_, srv, mem := savedServer(t)
	resp, body := get(t, srv, "/v3/"+herzlID+"/herzl.ics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(body, "example.org") {
		t.Error("the owner's email reached the feed")
	}
	for _, want := range []string{
		"X-WR-CALNAME:Hebrew Anniversaries: Theodor Herzl\\, Ze'ev Jabotinsky\\, Levi E...",
		"SEQUENCE:1", "Yizkor (Yom Kippur)", "Yitzhak Rabin Yahrzeit reminder",
	} {
		if !strings.Contains(unfold(body), want) {
			t.Errorf("missing %q", want)
		}
	}
	if !mem.Rows[herzlID].Downloaded {
		t.Error("first fetch did not mark the calendar downloaded")
	}

	_, body = get(t, srv, "/v3/"+chabadID+"/chabad.ics")
	body = unfold(body)
	for _, want := range []string{"SEQUENCE:6", "Baal Shem Tov", "Birthday of Tzemach Tzeddek", "R. Levi Yitzchak Schneerson"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestYahrzeitSavedErrors(t *testing.T) {
	app, srv, mem := savedServer(t)

	resp, body := get(t, srv, "/v3/01jthv2t5k88yermamssn00000/x.ics")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "Yahrzeit/Anniversary id not found: 01jthv2t5k88yermamssn00000") {
		t.Errorf("unknown id: %d %q", resp.StatusCode, body)
	}
	if resp.Header.Get("Cache-Control") != "" || resp.Header.Get("ETag") != "" {
		t.Error("404 carries caching headers")
	}
	resp, _ = get(t, srv, "/v3/not-an-id/x.ics")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("malformed id: %d", resp.StatusCode)
	}

	mem.Err = errors.New("connection refused")
	resp, body = get(t, srv, "/v3/"+goldaID+"/golda_meir.ics")
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Errorf("database down: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if strings.Contains(body, "refused") {
		t.Error("the cause leaked to the client")
	}

	app.Yahrzeit = nil
	resp, _ = get(t, srv, "/v3/"+goldaID+"/golda_meir.ics")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("no database: %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v3/"+goldaID+"/golda_meir.ics", nil)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", r.StatusCode)
	}
}

// Editing a calendar changes its ETag; a 304 still records the access.
func TestYahrzeitSavedETag(t *testing.T) {
	_, srv, mem := savedServer(t)
	path := "/v3/" + goldaID + "/golda_meir.ics"
	resp, _ := get(t, srv, path)
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	resp, _ = get(t, srv, path, "If-None-Match", etag)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("revalidation: %d", resp.StatusCode)
	}
	if n := len(mem.Touched()); n != 2 {
		t.Errorf("touched %d times, want 2", n)
	}
	mem.Rows[goldaID].Updated = mem.Rows[goldaID].Updated.Add(time.Minute)
	resp, _ = get(t, srv, path, "If-None-Match", etag)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") == etag {
		t.Errorf("after an edit: %d, same tag %v", resp.StatusCode, resp.Header.Get("ETag") == etag)
	}
}

// A saved calendar asked for in a format it has none of is a 404 naming the
// extension, answered before the row is looked up: nothing is read from the
// database or recorded as a download.
func TestYahrzeitSavedUnknownExtension(t *testing.T) {
	_, srv, mem := savedServer(t)
	mem.Err = errors.New("the database must not be asked")
	for path, body := range map[string]string{
		"/v3/" + goldaID + "/golda_meir.foo": "unknown download extension .foo\n",
		"/v3/" + goldaID + "/golda_meir":     "unknown download extension (none)\n",
		"/v3/" + goldaID:                     "unknown download extension (none)\n",
	} {
		resp, got := get(t, srv, path)
		if resp.StatusCode != http.StatusNotFound || got != body {
			t.Errorf("%s: %d %q, want 404 %q", path, resp.StatusCode, got, body)
		}
	}
	if n := len(mem.Touched()); n != 0 {
		t.Errorf("%d downloads recorded", n)
	}
}
