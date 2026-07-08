package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

func testServer(t *testing.T) (*appServer, *httptest.Server) {
	t.Helper()
	logger := &accessLogger{out: io.Discard, hostname: "test", pid: 1}
	app := newAppServer(logger)
	// fixed "today" for tests that omit the date: 2026-07-05 in America/New_York
	app.now = func() gregDate {
		return gregDate{Year: 2026, Month: time.July, Day: 5}
	}
	srv := httptest.NewServer(app.mux())
	t.Cleanup(srv.Close)
	return app, srv
}

func get(t *testing.T, srv *httptest.Server, path string, headers ...string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	// prevent the default transport from silently adding Accept-Encoding: gzip
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "identity")
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func TestSingleG2H(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/converter?gd=5&gm=7&gy=2026&g2h=1&cfg=json")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	want := `{"gy":2026,"gm":7,"gd":5,"afterSunset":false,"hy":5786,"hm":"Tamuz","hd":20,` +
		`"hebrew":"כ׳ בְּתַמּוּז תשפ״ו","heDateParts":{"y":"תשפ״ו","m":"תמוז","d":"כ׳"},` +
		`"events":["Parashat Matot-Masei"]}`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != cacheControl1Year {
		t.Errorf("Cache-Control = %q", cc)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing CORS header")
	}
	if resp.Header.Get("Cross-Origin-Resource-Policy") != "cross-origin" {
		t.Error("missing CORP header")
	}
	etag := resp.Header.Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) {
		t.Errorf("ETag = %q, want weak etag", etag)
	}
}

// Documented example: https://www.hebcal.com/converter?cfg=json&date=2011-06-02&g2h=1&strict=1
func TestSingleG2HDateParam(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/converter?cfg=json&date=2011-06-02&g2h=1&strict=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	want := `{"gy":2011,"gm":6,"gd":2,"afterSunset":false,"hy":5771,"hm":"Iyyar","hd":29,` +
		`"hebrew":"כ״ט בְּאִיָיר תשע״א","heDateParts":{"y":"תשע״א","m":"אייר","d":"כ״ט"},` +
		`"events":["Parashat Nasso","44th day of the Omer"]}`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestSingleH2G(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/converter?cfg=json&hy=5749&hm=Kislev&hd=25&h2g=1&strict=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatal(err)
	}
	if obj["gy"].(float64) != 1988 || obj["gm"].(float64) != 12 || obj["gd"].(float64) != 4 {
		t.Errorf("wrong gregorian date: %s", body)
	}
}

func TestAfterSunset(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1&gs=on")
	var obj map[string]interface{}
	json.Unmarshal([]byte(body), &obj)
	if obj["afterSunset"] != true {
		t.Error("afterSunset should be true")
	}
	// Gregorian date unchanged, Hebrew date advanced
	if obj["gd"].(float64) != 5 || obj["hd"].(float64) != 21 {
		t.Errorf("gs=on: %s", body)
	}
}

func TestChanukahRename(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=json&gy=2025&gm=12&gd=15&g2h=1")
	if !strings.Contains(body, `"Chanukah day 1"`) {
		t.Errorf("expected Chanukah day 1: %s", body)
	}
	_, body = get(t, srv, "/converter?cfg=json&gy=2025&gm=12&gd=22&g2h=1")
	if !strings.Contains(body, `"Chanukah day 8"`) {
		t.Errorf("expected Chanukah day 8: %s", body)
	}
	// Hebrew locale uses the translated form
	_, body = get(t, srv, "/converter?cfg=json&gy=2025&gm=12&gd=15&g2h=1&lg=h")
	if !strings.Contains(body, "חֲנוּכָּה יוֹם א׳") {
		t.Errorf("expected Hebrew Chanukah day 1: %s", body)
	}
}

func TestLocales(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=json&gy=2025&gm=12&gd=15&g2h=1&lg=a")
	if !strings.Contains(body, `"Parshas Mikeitz"`) && !strings.Contains(body, `"Parshas Miketz"`) {
		t.Errorf("expected Ashkenazi parsha: %s", body)
	}
	_, body = get(t, srv, "/converter?cfg=json&date=2011-06-02&g2h=1&lg=h")
	if !strings.Contains(body, "פָּרָשַׁת נָשׂא") {
		t.Errorf("expected Hebrew parsha: %s", body)
	}
}

func TestShabbatMevarchim(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=json&gy=2022&gm=1&gd=1&g2h=1")
	if !strings.Contains(body, `"Shabbat Mevarchim Chodesh Sh’vat"`) {
		t.Errorf("expected Shabbat Mevarchim with smart apostrophe: %s", body)
	}
	if !strings.Contains(body, `"Molad Sh’vat`) {
		t.Errorf("expected Molad event: %s", body)
	}
	if !strings.Contains(body, `"Parashat Vaera"`) {
		t.Errorf("expected parsha: %s", body)
	}
}

func TestIsraelFlag(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=json&gy=2024&gm=10&gd=8&g2h=1&i=on")
	if !strings.Contains(body, `"il":true`) {
		t.Errorf("expected il:true: %s", body)
	}
	if !strings.Contains(body, `"Parashat Vezot Haberakhah"`) {
		t.Errorf("expected Vezot Haberakhah between YK and Sukkot: %s", body)
	}
	_, body = get(t, srv, "/converter?cfg=json&gy=2024&gm=10&gd=8&g2h=1")
	if strings.Contains(body, `"il"`) {
		t.Errorf("il should be absent without i param: %s", body)
	}
}

func TestPseudoParsha(t *testing.T) {
	_, srv := testServer(t)
	// 10 Nisan 5785: upcoming Saturday is Pesach I
	_, body := get(t, srv, "/converter?cfg=json&gy=2025&gm=4&gd=8&g2h=1")
	if !strings.Contains(body, `"Parashat Tzav"`) {
		t.Errorf("expected Parashat Tzav: %s", body)
	}
	// weekday Erev Pesach whose upcoming Saturday is a Pesach holiday reading
	_, body = get(t, srv, "/converter?cfg=json&gy=2026&gm=4&gd=1&g2h=1")
	if !strings.Contains(body, `"Parashat Pesach"`) {
		t.Errorf("expected pseudo-parsha Parashat Pesach: %s", body)
	}
	// chol hamoed day has a Torah reading, so no parsha event
	_, body = get(t, srv, "/converter?cfg=json&gy=2025&gm=4&gd=16&g2h=1")
	if strings.Contains(body, `"Parashat`) {
		t.Errorf("no parsha expected on chol hamoed: %s", body)
	}
}

func TestHebrewYear1(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=json&h2g=1&hy=1&hm=Tishrei&hd=1")
	want := `{"gy":-3760,"gm":9,"gd":7,"afterSunset":false,"hy":1,"hm":"Tishrei","hd":1,` +
		`"hebrew":"א׳ בְּתִשְׁרֵי א׳","heDateParts":{"y":"א׳","m":"תשרי","d":"א׳"}}`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestAdarLeapYear(t *testing.T) {
	_, srv := testServer(t)
	// 5784 is a leap year: Adar1 -> "Adar I"
	_, body := get(t, srv, "/converter?cfg=json&h2g=1&hy=5784&hm=Adar1&hd=15")
	if !strings.Contains(body, `"hm":"Adar I"`) {
		t.Errorf("expected Adar I: %s", body)
	}
	if !strings.Contains(body, "בַּאֲדָר א׳") {
		t.Errorf("expected Hebrew Adar I: %s", body)
	}
	// Adar2 in a non-leap year (5785) is treated as plain Adar
	_, body = get(t, srv, "/converter?cfg=json&h2g=1&hy=5785&hm=Adar2&hd=15")
	if !strings.Contains(body, `"hm":"Adar"`) || strings.Contains(body, "Adar I") {
		t.Errorf("expected plain Adar: %s", body)
	}
}

func TestMonthNameLeniency(t *testing.T) {
	_, srv := testServer(t)
	for _, hm := range []string{"Shvat", "Sh%27vat", "sh%27vat", "sh"} {
		_, body := get(t, srv, "/converter?cfg=json&h2g=1&hy=5786&hm="+hm+"&hd=1")
		if !strings.Contains(body, `"hm":"Sh'vat"`) {
			t.Errorf("hm=%s: expected Sh'vat: %s", hm, body)
		}
	}
	// Hebrew month name
	_, body := get(t, srv, "/converter?cfg=json&h2g=1&hy=5786&hm=%D7%98%D7%91%D7%AA&hd=1")
	if !strings.Contains(body, `"hm":"Tevet"`) {
		t.Errorf("expected Tevet from Hebrew name: %s", body)
	}
}

func TestDefaultsToToday(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/converter?cfg=json")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	wantLocation := "https://www.hebcal.com/converter?gd=5&gm=7&gy=2026&g2h=1&cfg=json"
	if got := resp.Header.Get("Location"); got != wantLocation {
		t.Errorf("Location = %q, want %q", got, wantLocation)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, max-age=1200" {
		t.Errorf("unexpected Cache-Control: %q", got)
	}
	if resp.Header.Get("ETag") != "" {
		t.Errorf("unexpected ETag: %q", resp.Header.Get("ETag"))
	}
	wantBody := "Redirecting to " + wantLocation + "\n"
	if body != wantBody {
		t.Errorf("redirect body = %q, want %q", body, wantBody)
	}
	// h2g with no params also redirects to the same pinned-date g2h URL
	resp2, _ := get(t, srv, "/converter?cfg=json&h2g=1")
	if resp2.StatusCode != http.StatusFound {
		t.Fatalf("h2g fallback: status = %d", resp2.StatusCode)
	}
	if got := resp2.Header.Get("Location"); got != wantLocation {
		t.Errorf("h2g fallback Location = %q, want %q", got, wantLocation)
	}
}

func TestMissingCfg(t *testing.T) {
	_, srv := testServer(t)
	for _, path := range []string{
		"/converter?gd=5&gm=7&gy=2026&g2h=1",
		"/converter?cfg=html&gd=5&gm=7&gy=2026&g2h=1",
		"/converter",
	} {
		resp, _ := get(t, srv, path)
		if resp.StatusCode != http.StatusNotImplemented {
			t.Errorf("%s: status = %d, want 501", path, resp.StatusCode)
		}
	}
}

// TestBareConverterRedirects covers the edge case where /converter is
// requested with a valid cfg but no date parameters at all: since the
// response would depend on "now", it must 302 to a URL with the date pinned
// rather than caching it. Requests that omit cfg entirely, or pass an
// unsupported cfg, still 501 regardless of the date params (TestMissingCfg).
func TestBareConverterRedirects(t *testing.T) {
	_, srv := testServer(t)
	cases := []struct{ path, wantLocation string }{
		{"/converter?cfg=xml", "https://www.hebcal.com/converter?gd=5&gm=7&gy=2026&g2h=1&cfg=xml"},
		{"/converter?cfg=json&lg=h", "https://www.hebcal.com/converter?gd=5&gm=7&gy=2026&g2h=1&cfg=json&lg=h"},
	}
	for _, c := range cases {
		resp, _ := get(t, srv, c.path)
		if resp.StatusCode != http.StatusFound {
			t.Errorf("%s: status = %d, want 302", c.path, resp.StatusCode)
			continue
		}
		if got := resp.Header.Get("Location"); got != c.wantLocation {
			t.Errorf("%s: Location = %q, want %q", c.path, got, c.wantLocation)
		}
		if got := resp.Header.Get("Cache-Control"); got != "private, max-age=1200" {
			t.Errorf("%s: Cache-Control = %q", c.path, got)
		}
	}
}

func TestErrorMessages(t *testing.T) {
	_, srv := testServer(t)
	cases := []struct {
		path, wantErr string
	}{
		{"/converter?cfg=json&hy=5785&hm=&hd=24&h2g=1&strict=1",
			"Missing parameter 'hm' for conversion from Hebrew to Gregorian"},
		{"/converter?cfg=json&h2g=1&strict=1&hm=Av&hd=1",
			"Missing parameter 'hy' for conversion from Hebrew to Gregorian"},
		{"/converter?cfg=json&g2h=1&strict=1&gm=7&gd=5",
			"Missing parameter 'gy' for conversion from Gregorian to Hebrew"},
		{"/converter?cfg=json&hd=12&hy=5801&h2g=1", "Hebrew month is required"},
		{"/converter?cfg=json&h2g=1&hy=99999&hm=Av&hd=1", "Hebrew year is too large: 99999"},
		{"/converter?cfg=json&h2g=1&hy=0&hm=Av&hd=1", "Hebrew year must be year 1 or later: 0"},
		{"/converter?cfg=json&h2g=1&hy=abc&hm=Av&hd=1", "Hebrew year must be numeric: abc"},
		{"/converter?cfg=json&h2g=1&hy=5786&hm=Av&hd=xyz", "Hebrew day must be numeric: xyz"},
		{"/converter?cfg=json&h2g=1&hy=5786&hm=Foo&hd=1", "bad monthName: Foo"},
		{"/converter?cfg=json&h2g=1&hy=5786&hm=7&hd=1", "bad monthName: 7"},
		{"/converter?cfg=json&h2g=1&hy=5786&hm=Av&hd=31",
			"Hebrew day out of valid range 1-30 for Av 5786"},
		{"/converter?cfg=json&h2g=1&hy=5785&hm=Adar2&hd=30",
			"Hebrew day out of valid range 1-29 for Adar 5785"},
		{"/converter?cfg=json&g2h=1&gy=2026&gm=99&gd=5",
			"Gregorian month out of valid range 1-12: 99"},
		{"/converter?cfg=json&g2h=1&gy=2026&gm=2&gd=30",
			"Gregorian day 30 out of valid range for 2/2026"},
		{"/converter?cfg=json&g2h=1&gy=99999&gm=2&gd=3",
			"Gregorian year cannot be greater than 9999: 99999"},
		{"/converter?cfg=json&g2h=1&gy=abc&gm=2&gd=3", "Gregorian year must be numeric: abc"},
		{"/converter?cfg=json&g2h=1&gy=-3800&gm=1&gd=1",
			"Gregorian date before Hebrew year 1: -003800-01-01"},
		{"/converter?cfg=json&g2h=1&gy=-3760&gm=9&gd=7",
			"Gregorian date before Hebrew year 1: -003760-09-07"},
		{"/converter?cfg=json&date=junk&g2h=1", "Date does not match format YYYY-MM-DD: junk"},
		{"/converter?cfg=json&h2g=1&ndays=abc&hy=5785&hm=Av&hd=1", "Invalid value for ndays: abc"},
		{"/converter?cfg=json&h2g=1&ndays=0&hy=5785&hm=Av&hd=1", "Invalid value for ndays: 0"},
		{"/converter?cfg=json&h2g=1&ndays=3&hy=abc&hm=Av&hd=1", "Hebrew year must be numeric: abc"},
		{"/converter?cfg=json&h2g=1&gd=1&hy=5786&hm=Av", "Hebrew day must be numeric: undefined"},
	}
	for _, c := range cases {
		resp, body := get(t, srv, c.path)
		if resp.StatusCode != 400 {
			t.Errorf("%s: status = %d, want 400 (%s)", c.path, resp.StatusCode, body)
			continue
		}
		var obj map[string]string
		if err := json.Unmarshal([]byte(body), &obj); err != nil {
			t.Errorf("%s: bad json: %s", c.path, body)
			continue
		}
		if obj["error"] != c.wantErr {
			t.Errorf("%s:\n got: %s\nwant: %s", c.path, obj["error"], c.wantErr)
		}
	}
}

func TestEpochBoundary(t *testing.T) {
	_, srv := testServer(t)
	// 2 Tishrei 1 is the first Gregorian date accepted via gy/gm/gd
	resp, body := get(t, srv, "/converter?cfg=json&g2h=1&gy=-3760&gm=9&gd=8")
	if resp.StatusCode != 200 || !strings.Contains(body, `"hy":1`) {
		t.Errorf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestDateRollover(t *testing.T) {
	_, srv := testServer(t)
	// like JavaScript new Date(), out-of-range month/day roll over
	_, body := get(t, srv, "/converter?cfg=json&date=2011-13-45&g2h=1")
	if !strings.Contains(body, `"gy":2012,"gm":2,"gd":14`) {
		t.Errorf("expected rollover to 2012-02-14: %s", body)
	}
}

func TestXML(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/converter/?cfg=xml&gy=2026&gm=7&gd=5&g2h=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/xml; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<hebcal>
<gregorian year="2026" month="7" day="5" />
<hebrew year="5786" month="Tamuz" day="20" str="כ׳ בְּתַמּוּז תשפ״ו" />
<hebdate year="תשפ״ו" month="תמוז" day="כ׳" />
<events>
 <event name="Parashat Matot-Masei" diaspora="1" israel="1" href="https://www.hebcal.com/sedrot/matot-masei-20260711" />
</events>
</hebcal>
`
	if body != want {
		t.Errorf("body mismatch\n got: %q\nwant: %q", body, want)
	}
}

func TestXMLSunset(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=xml&gy=2026&gm=7&gd=5&g2h=1&gs=on")
	if !strings.Contains(body, `<gregorian year="2026" month="7" day="5" sunset="1" />`) {
		t.Errorf("expected sunset attribute: %s", body)
	}
	if !strings.Contains(body, `<hebrew year="5786" month="Tamuz" day="21"`) {
		t.Errorf("expected hebrew day 21: %s", body)
	}
}

func TestXMLDiasporaIsrael(t *testing.T) {
	_, srv := testServer(t)
	// Pesach VIII observed in the Diaspora only
	_, body := get(t, srv, "/converter?cfg=xml&gy=2026&gm=4&gd=9&g2h=1")
	if !strings.Contains(body, `<event name="Pesach VIII" diaspora="1" israel="0" href="https://www.hebcal.com/holidays/pesach-2026" />`) {
		t.Errorf("expected diaspora-only Pesach VIII: %s", body)
	}
	if !strings.Contains(body, `<event name="7th day of the Omer" diaspora="1" israel="1" href="https://www.hebcal.com/omer/5786/7" />`) {
		t.Errorf("expected omer with URL: %s", body)
	}
}

func TestXMLError(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/converter?cfg=xml&gy=2026&gm=99&gd=5&g2h=1")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body != "<error message=\"Gregorian month out of valid range 1-12: 99\" />\n" {
		t.Errorf("body = %q", body)
	}
	// apostrophes escaped as &#39; (numeric entity), same as XML attributes
	_, body = get(t, srv, "/converter?cfg=xml&start=2024-01-01&end=2024-01-05")
	if body != "<error message=\"Date range conversion using &#39;start&#39; and &#39;end&#39; requires cfg=json\" />\n" {
		t.Errorf("body = %q", body)
	}
}

func TestRange(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/converter?cfg=json&start=2021-12-29&end=2022-01-04&g2h=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var obj struct {
		Start  string                     `json:"start"`
		End    string                     `json:"end"`
		Hdates map[string]json.RawMessage `json:"hdates"`
	}
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatal(err)
	}
	if obj.Start != "2021-12-29" || obj.End != "2022-01-04" {
		t.Errorf("start/end: %s %s", obj.Start, obj.End)
	}
	if len(obj.Hdates) != 7 {
		t.Errorf("len(hdates) = %d, want 7", len(obj.Hdates))
	}
	if !strings.Contains(string(obj.Hdates["2022-01-03"]), `"Rosh Chodesh Sh’vat"`) {
		t.Errorf("2022-01-03: %s", obj.Hdates["2022-01-03"])
	}
	// keys must be in chronological order
	idx1 := strings.Index(body, `"2021-12-29"`)
	idx2 := strings.Index(body, `"2022-01-04"`)
	if idx1 == -1 || idx2 == -1 || idx1 > idx2 {
		t.Error("hdates keys out of order")
	}
}

func TestRangeEdgeCases(t *testing.T) {
	_, srv := testServer(t)
	// truncated to 399 days
	_, body := get(t, srv, "/converter?cfg=json&start=2025-01-01&end=2026-12-31")
	var obj struct {
		End    string                     `json:"end"`
		Hdates map[string]json.RawMessage `json:"hdates"`
	}
	json.Unmarshal([]byte(body), &obj)
	if obj.End != "2026-02-04" || len(obj.Hdates) != 400 {
		t.Errorf("end=%s len=%d, want 2026-02-04/400", obj.End, len(obj.Hdates))
	}
	// end before start collapses to a single conversion
	_, body = get(t, srv, "/converter?cfg=json&start=2025-01-01&end=2024-01-05")
	if !strings.Contains(body, `"gy":2025,"gm":1,"gd":1`) {
		t.Errorf("expected single date: %s", body)
	}
	// start == end collapses to a single conversion
	_, body = get(t, srv, "/converter?cfg=json&start=2025-03-03&end=2025-03-03")
	if !strings.Contains(body, `"gy":2025,"gm":3,"gd":3`) {
		t.Errorf("expected single date: %s", body)
	}
	// range requires cfg=json
	resp, body := get(t, srv, "/converter?cfg=xml&start=2024-01-01&end=2024-01-05")
	if resp.StatusCode != 400 {
		t.Errorf("xml range status = %d (%s)", resp.StatusCode, body)
	}
}

func TestNdays(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=json&h2g=1&ndays=3&hy=5785&hm=Av&hd=1")
	var obj struct {
		Hdates map[string]json.RawMessage `json:"hdates"`
	}
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatal(err)
	}
	if len(obj.Hdates) != 3 {
		t.Errorf("len(hdates) = %d, want 3", len(obj.Hdates))
	}
	// ndays are capped at 399 days total
	_, body = get(t, srv, "/converter?cfg=json&h2g=1&ndays=500&hy=5785&hm=Av&hd=1")
	json.Unmarshal([]byte(body), &obj)
	if len(obj.Hdates) != 399 {
		t.Errorf("len(hdates) = %d, want 399", len(obj.Hdates))
	}
}

func TestCallback(t *testing.T) {
	_, srv := testServer(t)
	_, body := get(t, srv, "/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1&callback=foo.bar%3Cx%3E")
	if !strings.HasPrefix(body, "foo.barx({") || !strings.HasSuffix(body, "})\n") {
		t.Errorf("jsonp body = %q", body)
	}
}

func TestMethods(t *testing.T) {
	_, srv := testServer(t)
	// POST accepted, body ignored
	resp, err := http.Post(srv.URL+"/converter?cfg=json&hy=5786&hm=Cheshvan&hd=14&h2g=1&strict=1&gs=off",
		"application/json", strings.NewReader(`{"gy": 1999}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"hy":5786`) {
		t.Errorf("POST: %d %s", resp.StatusCode, body)
	}
	// HEAD returns headers but no body
	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1", nil)
	resp2, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != 200 || len(b2) != 0 {
		t.Errorf("HEAD: %d body=%q", resp2.StatusCode, b2)
	}
	if resp2.Header.Get("Content-Length") == "" || resp2.Header.Get("Content-Length") == "0" {
		t.Errorf("HEAD Content-Length = %q", resp2.Header.Get("Content-Length"))
	}
	// OPTIONS preflight
	req, _ = http.NewRequest(http.MethodOptions, srv.URL+"/converter?cfg=json", nil)
	resp3, _ := http.DefaultClient.Do(req)
	resp3.Body.Close()
	if resp3.StatusCode != 204 || resp3.Header.Get("Access-Control-Allow-Methods") != "GET, POST" {
		t.Errorf("OPTIONS: %d %q", resp3.StatusCode, resp3.Header.Get("Access-Control-Allow-Methods"))
	}
	// other methods rejected
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/converter?cfg=json", nil)
	resp4, _ := http.DefaultClient.Do(req)
	resp4.Body.Close()
	if resp4.StatusCode != 405 {
		t.Errorf("DELETE: %d", resp4.StatusCode)
	}
}

func TestETag304(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := get(t, srv, "/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1")
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	resp2, body := get(t, srv, "/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1",
		"If-None-Match", etag)
	if resp2.StatusCode != 304 {
		t.Fatalf("status = %d", resp2.StatusCode)
	}
	if body != "" {
		t.Errorf("304 body = %q", body)
	}
	// RFC 7232: the 304 carries the same Cache-Control as a 200 would
	if resp2.Header.Get("Cache-Control") != cacheControl1Year {
		t.Errorf("304 Cache-Control = %q", resp2.Header.Get("Cache-Control"))
	}
	// a different Accept-Encoding class yields a different validator
	resp3, _ := get(t, srv, "/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1",
		"Accept-Encoding", "gzip")
	if resp3.Header.Get("ETag") == etag {
		t.Error("ETag should vary with Accept-Encoding")
	}
}

func TestCompression(t *testing.T) {
	_, srv := testServer(t)
	// large batch response is gzipped
	resp, body := get(t, srv, "/converter?cfg=json&start=2025-01-01&end=2025-06-30",
		"Accept-Encoding", "gzip")
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", resp.Header.Get("Content-Encoding"))
	}
	if !strings.Contains(resp.Header.Get("Vary"), "Accept-Encoding") {
		t.Error("missing Vary")
	}
	zr, err := gzip.NewReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), `"2025-06-30"`) {
		t.Error("bad gzip payload")
	}
	// small single-date response is not compressed and has no Vary
	resp2, _ := get(t, srv, "/converter?cfg=json&gy=2025&gm=12&gd=24&g2h=1",
		"Accept-Encoding", "gzip")
	if resp2.Header.Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %q", resp2.Header.Get("Content-Encoding"))
	}
	if resp2.Header.Get("Vary") != "" {
		t.Errorf("Vary = %q", resp2.Header.Get("Vary"))
	}
	// no gzip without Accept-Encoding, but Vary still set for non-JSON
	resp3, _ := get(t, srv, "/converter/csv?hd=20&hm=Tamuz&hy=5786&h2g=1")
	if resp3.Header.Get("Content-Encoding") != "" {
		t.Errorf("csv Content-Encoding = %q", resp3.Header.Get("Content-Encoding"))
	}
	if !strings.Contains(resp3.Header.Get("Vary"), "Accept-Encoding") {
		t.Error("csv missing Vary")
	}
}

func TestBrotli(t *testing.T) {
	_, srv := testServer(t)
	// brotli preferred when the client offers both
	resp, body := get(t, srv, "/converter?cfg=json&start=2025-01-01&end=2025-01-31",
		"Accept-Encoding", "gzip, deflate, br")
	if resp.Header.Get("Content-Encoding") != "br" {
		t.Fatalf("Content-Encoding = %q, want br", resp.Header.Get("Content-Encoding"))
	}
	if !strings.Contains(resp.Header.Get("Vary"), "Accept-Encoding") {
		t.Error("missing Vary")
	}
	plain, err := io.ReadAll(brotli.NewReader(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), `"2025-01-31"`) {
		t.Error("bad brotli payload")
	}
	// gzip-only clients still get gzip
	resp2, _ := get(t, srv, "/converter?cfg=json&start=2025-01-01&end=2025-01-31",
		"Accept-Encoding", "gzip")
	if resp2.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", resp2.Header.Get("Content-Encoding"))
	}
	// brotli requests carry a different ETag class than gzip requests
	if resp.Header.Get("ETag") == resp2.Header.Get("ETag") {
		t.Error("ETag should differ between br and gzip clients")
	}
}

func TestCompressThreshold(t *testing.T) {
	_, srv := testServer(t)
	// a 3-day batch (~640 bytes) sits above the 512-byte threshold
	resp, _ := get(t, srv, "/converter?cfg=json&start=2025-01-01&end=2025-01-03",
		"Accept-Encoding", "br")
	if resp.Header.Get("Content-Encoding") != "br" {
		t.Errorf("3-day range Content-Encoding = %q, want br", resp.Header.Get("Content-Encoding"))
	}
	// a single-date response (~220 bytes) stays uncompressed
	resp2, _ := get(t, srv, "/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1",
		"Accept-Encoding", "br")
	if resp2.Header.Get("Content-Encoding") != "" {
		t.Errorf("single Content-Encoding = %q, want none", resp2.Header.Get("Content-Encoding"))
	}
}

func TestCSV(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/converter/csv?hd=20&hm=Tamuz&hy=5786&h2g=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/x-csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="hdate-20-tamuz.csv"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != cacheControl7Days {
		t.Errorf("Cache-Control = %q", cc)
	}
	lines := strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n")
	if lines[0] != "Gregorian Date,Hebrew Date" {
		t.Errorf("header = %q", lines[0])
	}
	// -5 .. +75 years inclusive
	if len(lines) != 82 {
		t.Errorf("len(lines) = %d, want 82", len(lines))
	}
	if lines[1] != "2021-06-30,20 Tamuz 5781" {
		t.Errorf("first row = %q", lines[1])
	}
	if lines[6] != "2026-07-05,20 Tamuz 5786" {
		t.Errorf("row = %q", lines[6])
	}
}

func TestCSVEdgeCases(t *testing.T) {
	_, srv := testServer(t)
	// 30 Cheshvan rolls over to 1 Kislev in years with a short Cheshvan
	_, body := get(t, srv, "/converter/csv?hd=30&hm=Cheshvan&hy=5785&h2g=1")
	if !strings.Contains(body, "1 Kislev 5784") {
		t.Errorf("expected 1 Kislev rollover: %s", body)
	}
	// POST not allowed
	resp, err := http.Post(srv.URL+"/converter/csv", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("POST status = %d, want 405", resp.StatusCode)
	}
	// date range not supported
	resp2, _ := get(t, srv, "/converter/csv?start=2024-01-01&end=2024-01-05")
	if resp2.StatusCode != 400 {
		t.Errorf("range status = %d, want 400", resp2.StatusCode)
	}
	resp3, _ := get(t, srv, "/converter/csv?h2g=1&ndays=2")
	if resp3.StatusCode != 400 {
		t.Errorf("ndays status = %d, want 400", resp3.StatusCode)
	}
	// ETag/304 support
	resp4, _ := get(t, srv, "/converter/csv?hd=20&hm=Tamuz&hy=5786&h2g=1")
	etag := resp4.Header.Get("ETag")
	resp5, _ := get(t, srv, "/converter/csv?hd=20&hm=Tamuz&hy=5786&h2g=1", "If-None-Match", etag)
	if resp5.StatusCode != 304 {
		t.Errorf("304 status = %d", resp5.StatusCode)
	}
}

func TestPingAndMetrics(t *testing.T) {
	app, srv := testServer(t)
	// /ping serves the ping file when it exists
	pingFile := t.TempDir() + "/ping"
	if err := os.WriteFile(pingFile, []byte("pong\n"), 0644); err != nil {
		t.Fatal(err)
	}
	app.pingFile = pingFile
	resp, body := get(t, srv, "/ping")
	if resp.StatusCode != 200 || body != "pong\n" {
		t.Errorf("ping: %d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("ping Content-Type = %q", ct)
	}
	// ... and returns 404 when it does not
	os.Remove(pingFile)
	resp404, _ := get(t, srv, "/ping")
	if resp404.StatusCode != 404 {
		t.Errorf("ping without file: %d, want 404", resp404.StatusCode)
	}
	// generate at least one counted request first
	get(t, srv, "/converter?cfg=json&gy=2026&gm=7&gd=5&g2h=1")
	resp2, body2 := get(t, srv, "/metrics")
	if resp2.StatusCode != 200 {
		t.Fatalf("metrics status = %d", resp2.StatusCode)
	}
	if !strings.Contains(body2, "http_requests_total") {
		t.Error("metrics missing http_requests_total")
	}
}

func TestNotFound(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := get(t, srv, "/nosuchpage")
	if resp.StatusCode != 404 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}
