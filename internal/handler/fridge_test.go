package handler

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

const fridgeNY = "/shabbat/fridge.cgi?geonameid=5128581"

func TestFridgeHTML(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, fridgeNY+"&year=5787")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=259200, s-maxage=259200" {
		t.Errorf("Cache-Control = %q", cc)
	}
	for _, want := range []string{
		`<html lang="en" dir="ltr">`,
		"<title>New York, USA - 5787 Shabbat Times - Hebcal</title>",
		`<meta name="robots" content="noindex, nofollow">`,
		`Candle lighting - New York`,
		`Hebrew Year 5787 (2026 - 2027)`,
		// the year opens with Erev Rosh Hashana, bold
		`<tr><td class="yomtov">Sep</td><td class="yomtov text-right">11</td><td class="yomtov">Rosh Hashana</td><td class="yomtov text-right">6:53</td><td class="yomtov text-right havdalah"></td>`,
		// a regular Shabbat, with its Havdalah in the hidden column
		`<td>Oct</td><td class="text-right">9</td><td>Bereshit</td><td class="text-right">6:06</td><td class="text-right havdalah">7:04</td>`,
		// the right-hand column is padded off the rule
		`<td class="leftpad">Apr</td><td class="text-right">2</td><td>Shmini</td>`,
		// the Friday of Shabbat Chol haMoed is named for the festival, not bold
		`<td class="leftpad">Apr</td><td class="text-right">23</td><td>Pesach</td>`,
		// a name with an apostrophe
		`<td>Ha’azinu</td>`,
		`data-base="/shabbat/fridge.cgi?geonameid=5128581&amp;ue=off&amp;b=18&amp;M=on&amp;lg=s"`,
		`title="Previous Hebrew year"` + "\n" + `  data-year="5786">`,
		`data-year="2027" data-yt="G">2027 &rarr;`,
		"Candle-lighting times are 18 min before sunset",
		"for 3 stars (sun 8.5° below the horizon)",
		"fonts.googleapis.com",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	// Chanukah candles are not Shabbat candles
	if strings.Contains(body, "Chanukah") {
		t.Error("body lists Chanukah")
	}
	// crawlers get no link to follow to another year
	if strings.Contains(body, "<a href") || regexp.MustCompile(`<a [^>]*href=`).MatchString(body) {
		t.Error("nav links carry an href")
	}
}

// The inline script runs only if the policy names the nonce it carries, and
// the nonce is new on every response.
func TestFridgeCSPNonce(t *testing.T) {
	srv := testServerWithDB(t)
	scriptNonce := regexp.MustCompile(`<script nonce="([A-Za-z0-9_-]{12})">`)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		resp, body := get(t, srv, fridgeNY+"&year=5787")
		m := scriptNonce.FindAllStringSubmatch(body, -1)
		if len(m) != 1 {
			t.Fatalf("found %d nonce script tags, want 1", len(m))
		}
		nonce := m[0][1]
		csp := resp.Header.Get("Content-Security-Policy")
		want := "script-src 'nonce-" + nonce + "' 'strict-dynamic' 'report-sample' https: 'unsafe-inline';" +
			" style-src 'self' https: data: 'unsafe-inline';" +
			" frame-ancestors https: data:;" +
			" frame-src https: data: blob:;" +
			" img-src 'self' https: data: blob:;" +
			" font-src 'self' data: https://fonts.gstatic.com/;" +
			" object-src 'none';" +
			" base-uri 'none';" +
			" report-uri https://y61ectdg.uriports.com/reports/report;" +
			" report-to default"
		if csp != want {
			t.Errorf("Content-Security-Policy = %q", csp)
		}
		if strings.Count(body, "<script") != 1 {
			t.Error("a script tag without the nonce")
		}
		if got := resp.Header.Get("X-XSS-Protection"); got != "0" {
			t.Errorf("X-XSS-Protection = %q", got)
		}
		if got := resp.Header.Get("Reporting-Endpoints"); got != `default="https://y61ectdg.uriports.com/reports"` {
			t.Errorf("Reporting-Endpoints = %q", got)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q", got)
		}
		seen[nonce] = true
	}
	if len(seen) != 2 {
		t.Error("the nonce repeated")
	}
}

func TestFridgeHebrew(t *testing.T) {
	srv := testServerWithDB(t)
	_, body := get(t, srv, "/shabbat/fridge.cgi?geonameid=281184&lg=h&year=5787")
	for _, want := range []string{
		`<html lang="he" dir="rtl">`,
		"הַדְלָקַת נֵרוֹת - Jerusalem",
		// dayjs's Hebrew month abbreviation, a 24-hour clock, Jerusalem's 40 minutes
		`<tr><td class="yomtov">ספט</td><td class="yomtov text-right">11</td><td class="yomtov text-right">רֹאשׁ הַשָּׁנָה</td><td class="yomtov text-right">18:10</td>`,
		`<td class="rightpad">אפר</td>`,
		"/i/adobehebrew-regular.woff2",
		"/i/adobehebrew-bold.woff2",
		"font-family:'Adobe Hebrew', sans-serif",
		"Candle-lighting times are 40 min before sunset",
		`&amp;b=40&amp;M=on&amp;lg=h"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "fonts.googleapis.com") {
		t.Error("Hebrew page loads the Latin web font")
	}
}

func TestFridgeLocalizedMonths(t *testing.T) {
	srv := testServerWithDB(t)
	tests := []struct{ lg, want string }{
		{"fr", `<td>janv.</td><td class="text-right">1</td>`},
		{"ru", `<td>янв.</td><td class="text-right">1</td><td>Шмот</td>`},
		{"es", `<td>ene</td><td class="text-right">1</td>`},
		{"de", `<td>Jan.</td><td class="text-right">1</td>`},
		{"a", `<td>Jan</td><td class="text-right">1</td><td>Shemos</td>`},
	}
	for _, tc := range tests {
		_, body := get(t, srv, fridgeNY+"&yt=G&year=2027&lg="+tc.lg)
		if !strings.Contains(body, tc.want) {
			t.Errorf("lg=%s: body lacks %q", tc.lg, tc.want)
		}
		if !strings.Contains(body, "2027 (5787 - 5788)") {
			t.Errorf("lg=%s: heading missing", tc.lg)
		}
	}
}

func TestFridgeHavdalahOptions(t *testing.T) {
	srv := testServerWithDB(t)
	_, body := get(t, srv, fridgeNY+"&year=5787&m=50&b=25")
	for _, want := range []string{
		"Candle-lighting times are 25 min before sunset",
		"Havdalah times are\n50 min after sunset",
		`&amp;b=25&amp;M=off&amp;m=50&amp;lg=s"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("m=50: body lacks %q", want)
		}
	}
	_, body = get(t, srv, fridgeNY+"&year=5787&td=7.083")
	if !strings.Contains(body, "(sun 7.083° below the horizon)") || !strings.Contains(body, "&amp;td=7.083&amp;") {
		t.Error("td=7.083 not reflected")
	}
	// no Havdalah at all: nothing to toggle
	_, body = get(t, srv, fridgeNY+"&year=5787&m=0")
	if strings.Contains(body, `id="toggle-havdalah"`) || strings.Contains(body, "Havdalah times are") {
		t.Error("m=0 still offers Havdalah times")
	}
}

func TestFridgeCSV(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, fridgeNY+"&year=5787&cfg=csv")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="candles-new-york-5787.csv"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if resp.Header.Get("Content-Security-Policy") != "" {
		t.Error("CSV carries a Content-Security-Policy")
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CSV lacks CORS")
	}
	wantStart := "\xEF\xBB\xBFDate,Parsha,Candles,Havdalah\r\n" +
		"2026-09-11,Rosh Hashana,6:53,\r\n" +
		"2026-09-12,Rosh Hashana,7:51,7:49\r\n" +
		"2026-09-18,Ha’azinu,6:41,7:39\r\n" +
		"2026-09-20,Yom Kippur,6:38,7:35\r\n"
	if !strings.HasPrefix(body, wantStart) {
		t.Errorf("CSV starts %q", body[:min(len(body), len(wantStart))])
	}
	// and closes with the next Erev Rosh Hashana
	if !strings.HasSuffix(body, "\r\n2027-10-01,Rosh Hashana,6:20,\r\n") {
		t.Errorf("CSV ends %q", body[max(0, len(body)-60):])
	}
	resp, _ = get(t, srv, "/shabbat/fridge.cgi?zip=90210&yt=G&year=2027&cfg=csv")
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="candles-90210-2027.csv"` {
		t.Errorf("zip Content-Disposition = %q", cd)
	}
}

// testServer pins today to 2026-07-05: Gregorian 2026, Hebrew 5786.
func TestFridgeYearWindow(t *testing.T) {
	srv := testServerWithDB(t)
	tests := []struct {
		query  string
		status int
	}{
		{"&yt=G&year=2022", 200},
		{"&yt=G&year=2021", 410},
		{"&yt=G&year=2046", 200},
		{"&yt=G&year=2047", 410},
		{"&year=5782", 200},
		{"&year=5781", 410},
		{"&year=5806", 200},
		{"&year=5807", 410},
		{"&year=abc", 400},
	}
	for _, tc := range tests {
		resp, body := get(t, srv, fridgeNY+tc.query)
		if resp.StatusCode != tc.status {
			t.Errorf("%s: status = %d, want %d", tc.query, resp.StatusCode, tc.status)
		}
		if tc.status != 200 {
			if resp.Header.Get("Cache-Control") != "" || resp.Header.Get("ETag") != "" {
				t.Errorf("%s: error is cacheable", tc.query)
			}
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
				t.Errorf("%s: Content-Type = %q", tc.query, resp.Header.Get("Content-Type"))
			}
			_ = body
		}
	}
	// links that would lead outside the window are not offered
	_, first := get(t, srv, fridgeNY+"&yt=G&year=2022")
	if strings.Contains(first, `title="Previous Gregorian year"`) || !strings.Contains(first, `title="Next Gregorian year"`) {
		t.Error("first year: wrong nav links")
	}
	_, last := get(t, srv, fridgeNY+"&yt=G&year=2046")
	if !strings.Contains(last, `title="Previous Gregorian year"`) || strings.Contains(last, `title="Next Gregorian year"`) {
		t.Error("last year: wrong nav links")
	}
}

func TestFridgeDefaultYear(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, fridgeNY)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "Hebrew Year 5786 (2025 - 2026)") {
		t.Error("default year is not 5786")
	}
	// the answer changes with the date, so it names no lifetime
	if cc := resp.Header.Get("Cache-Control"); cc != "" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestFridgeErrors(t *testing.T) {
	srv := testServerWithDB(t)
	tests := []struct {
		path   string
		status int
		body   string
	}{
		{"/shabbat/fridge.cgi?year=5787", 400, "Location required: geonameid, zip, city\n"},
		{"/shabbat/fridge.cgi?geonameid=99999999&year=5787", 404, "Sorry, can't find geonameid: 99999999\n"},
		{fridgeNY + "&year=5787&lg=xx", 400, "Locale 'xx' not found\n"},
	}
	for _, tc := range tests {
		resp, body := get(t, srv, tc.path)
		if resp.StatusCode != tc.status || body != tc.body {
			t.Errorf("%s: %d %q", tc.path, resp.StatusCode, body)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+fridgeNY, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: status = %d", resp.StatusCode)
	}
}

// Whatever a request puts in its query comes back escaped.
func TestFridgeEscapesQuery(t *testing.T) {
	srv := testServerWithDB(t)
	_, body := get(t, srv, "/shabbat/fridge.cgi?latitude=40.7&longitude=-74&tzid=America/New_York&year=5787"+
		"&city-typeahead=%22%3E%3Cscript%3Ealert(1)%3C/script%3E")
	if strings.Contains(body, "<script>alert") || strings.Contains(body, `"><script`) {
		t.Error("query text reached the page unescaped")
	}
	if strings.Count(body, "<script") != 1 {
		t.Error("extra script tag")
	}
}

func TestFridgeConditional(t *testing.T) {
	srv := testServerWithDB(t)
	resp, _ := get(t, srv, fridgeNY+"&year=5787")
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	resp, body := get(t, srv, fridgeNY+"&year=5787", "If-None-Match", etag)
	if resp.StatusCode != http.StatusNotModified || body != "" {
		t.Errorf("conditional: %d, %d bytes", resp.StatusCode, len(body))
	}
}

// /shabbat/fridge.cgi does not fall through to the JSON /shabbat route.
func TestFridgeRouteIsNotShabbatJSON(t *testing.T) {
	srv := testServerWithDB(t)
	resp, _ := get(t, srv, fridgeNY+"&cfg=json&year=5787")
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}
