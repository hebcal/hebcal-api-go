package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testServerWithDB is like testServer but also opens the sample geonames/zips
// databases in testdata/ so the /zmanim API can resolve locations.
func testServerWithDB(t *testing.T) *httptest.Server {
	t.Helper()
	app, _ := testServer(t)
	db, err := NewGeoDB("testdata/zips.sqlite3", "testdata/geonames.sqlite3")
	if err != nil {
		t.Fatalf("NewGeoDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app.db = db
	return httptest.NewServer(app.mux())
}

// decodeTimes pulls the times object out of a single-date zmanim response.
func decodeTimes(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var resp struct {
		Times map[string]interface{} `json:"times"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	return resp.Times
}

func TestZmanimGeoname(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/zmanim?cfg=json&geonameid=281184&date=2026-07-07")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	times := decodeTimes(t, body)
	// reference values from @hebcal/core Zmanim for Jerusalem on 2026-07-07
	want := map[string]string{
		"sunrise":          "2026-07-07T05:40:00+03:00",
		"sunset":           "2026-07-07T19:48:00+03:00",
		"chatzot":          "2026-07-07T12:44:00+03:00",
		"sofZmanShma":      "2026-07-07T09:12:00+03:00",
		"alotHaShachar":    "2026-07-07T04:13:00+03:00",
		"tzaisBaalHatanya": "2026-07-07T20:16:00+03:00",
		"minchaGedola":     "2026-07-07T13:19:00+03:00",
		"plagHaMincha":     "2026-07-07T18:20:00+03:00",
		"tzeit85deg":       "2026-07-07T20:30:00+03:00",
		"tzeit72min":       "2026-07-07T21:00:00+03:00",
	}
	for name, exp := range want {
		if got, _ := times[name].(string); got != exp {
			t.Errorf("times[%s] = %q, want %q", name, got, exp)
		}
	}
	// elevation is off by default, so seaLevel* times must be absent
	if _, ok := times["seaLevelSunrise"]; ok {
		t.Errorf("seaLevelSunrise present without ue=1")
	}
}

func TestZmanimVersionAndLocation(t *testing.T) {
	srv := testServerWithDB(t)
	_, body := get(t, srv, "/zmanim?cfg=json&geonameid=281184&date=2026-07-07")
	var resp struct {
		Date     string `json:"date"`
		Version  string `json:"version"`
		Location struct {
			Title     string  `json:"title"`
			City      string  `json:"city"`
			Tzid      string  `json:"tzid"`
			CC        string  `json:"cc"`
			Country   string  `json:"country"`
			Geonameid int     `json:"geonameid"`
			Geo       string  `json:"geo"`
			Latitude  float64 `json:"latitude"`
		} `json:"location"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Date != "2026-07-07" {
		t.Errorf("date = %q", resp.Date)
	}
	if resp.Version == "" {
		t.Errorf("version is empty")
	}
	if resp.Location.City != "Jerusalem" || resp.Location.CC != "IL" ||
		resp.Location.Country != "Israel" || resp.Location.Geonameid != 281184 ||
		resp.Location.Geo != "geoname" || resp.Location.Tzid != "Asia/Jerusalem" {
		t.Errorf("location = %+v", resp.Location)
	}
}

func TestZmanimElevation(t *testing.T) {
	srv := testServerWithDB(t)
	_, body := get(t, srv, "/zmanim?cfg=json&geonameid=281184&date=2026-07-07&ue=1")
	times := decodeTimes(t, body)
	if _, ok := times["seaLevelSunrise"].(string); !ok {
		t.Errorf("seaLevelSunrise missing with ue=1")
	}
	// elevation should be reported in the location object
	if !strings.Contains(body, `"elevation":786`) {
		t.Errorf("elevation not reported: %s", body)
	}
}

func TestZmanimZip(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/zmanim?cfg=json&zip=90210&date=2026-07-07")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"city":"Beverly Hills"`) ||
		!strings.Contains(body, `"tzid":"America/Los_Angeles"`) ||
		!strings.Contains(body, `"zip":"90210"`) {
		t.Errorf("unexpected zip location: %s", body)
	}
}

func TestZmanimLatLong(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv,
		"/zmanim?cfg=json&latitude=40.71427&longitude=-74.00597&tzid=America/New_York&date=2026-07-07")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"geo":"pos"`) {
		t.Errorf("expected geo=pos: %s", body)
	}
	// no cc/country/geonameid for a pos location
	if strings.Contains(body, `"cc":`) || strings.Contains(body, `"geonameid":`) {
		t.Errorf("pos location should not carry cc/geonameid: %s", body)
	}
}

func TestZmanimLegacyCity(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/zmanim?cfg=json&city=Jerusalem&date=2026-07-07")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"city":"Jerusalem"`) || !strings.Contains(body, `"geo":"geoname"`) {
		t.Errorf("unexpected legacy-city location: %s", body)
	}
}

func TestZmanimLatLongLegacy(t *testing.T) {
	srv := testServerWithDB(t)
	// legacy degree/minute/direction form: 40°42'N 74°0'W == 40.7, -74.0
	respLegacy, bodyLegacy := get(t, srv,
		"/zmanim?cfg=json&ladeg=40&lamin=42&ladir=n&lodeg=74&lomin=0&lodir=w&tzid=America/New_York&date=2026-07-07")
	if respLegacy.StatusCode != 200 {
		t.Fatalf("legacy status = %d body=%s", respLegacy.StatusCode, bodyLegacy)
	}
	var legacy struct {
		Location struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Geo       string  `json:"geo"`
		} `json:"location"`
		Times map[string]string `json:"times"`
	}
	if err := json.Unmarshal([]byte(bodyLegacy), &legacy); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// west longitude is expressed as a positive magnitude + direction, so it
	// must resolve to a negative decimal longitude (the "reversed" convention)
	if legacy.Location.Latitude != 40.7 || legacy.Location.Longitude != -74 ||
		legacy.Location.Geo != "pos" {
		t.Errorf("legacy location = %+v", legacy.Location)
	}
	// the decimal form with the same coordinates must produce identical times
	_, bodyDecimal := get(t, srv,
		"/zmanim?cfg=json&latitude=40.7&longitude=-74&tzid=America/New_York&date=2026-07-07")
	decimalTimes := decodeTimes(t, bodyDecimal)
	for name, v := range legacy.Times {
		if got, _ := decimalTimes[name].(string); got != v {
			t.Errorf("legacy vs decimal mismatch for %s: %q vs %q", name, v, got)
		}
	}
}

func TestZmanimLatLongLegacySouthWest(t *testing.T) {
	srv := testServerWithDB(t)
	// ladir=s and lodir=w must negate both coordinates
	_, body := get(t, srv,
		"/zmanim?cfg=json&ladeg=33&lamin=52&ladir=s&lodeg=151&lomin=12&lodir=e&tzid=Australia/Sydney&date=2026-07-07")
	var out struct {
		Location struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"location"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if out.Location.Latitude >= 0 || out.Location.Longitude <= 0 {
		t.Errorf("expected negative lat / positive long for S/E, got %+v", out.Location)
	}
}

func TestZmanimLatLongLegacyTzDst(t *testing.T) {
	srv := testServerWithDB(t)
	// legacy tz=2&dst=israel resolves to Asia/Jerusalem (and Israel schedule)
	_, body := get(t, srv,
		"/zmanim?cfg=json&ladeg=31&lamin=46&ladir=n&lodeg=35&lomin=13&lodir=e&tz=2&dst=israel&date=2026-07-07")
	if !strings.Contains(body, `"tzid":"Asia/Jerusalem"`) {
		t.Errorf("expected tzid Asia/Jerusalem: %s", body)
	}
}

func TestLegacyTzToTzid(t *testing.T) {
	cases := []struct{ tz, dst, want string }{
		{"2", "israel", "Asia/Jerusalem"},
		{"0", "none", "UTC"},
		{"-5", "none", "Etc/GMT-5"}, // reversed sign convention (UTC+5)
		{"3", "none", "Etc/GMT+3"},
		{"0", "eu", "Europe/London"},
		{"1", "eu", "Europe/Paris"},
		{"-5", "usa", "America/New_York"}, // tz*-1 => 5
		{"-8", "usa", "America/Los_Angeles"},
		{"99", "bogus", ""},
	}
	for _, tc := range cases {
		if got := legacyTzToTzid(tc.tz, tc.dst); got != tc.want {
			t.Errorf("legacyTzToTzid(%q,%q) = %q, want %q", tc.tz, tc.dst, got, tc.want)
		}
	}
}

func TestZmanimRange(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv,
		"/zmanim?cfg=json&geonameid=281184&start=2026-07-07&end=2026-07-09")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var out struct {
		Date struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"date"`
		Times map[string]map[string]string `json:"times"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if out.Date.Start != "2026-07-07" || out.Date.End != "2026-07-09" {
		t.Errorf("date range = %+v", out.Date)
	}
	if got := out.Times["sunrise"]["2026-07-08"]; got == "" {
		t.Errorf("missing sunrise for 2026-07-08: %s", body)
	}
	if len(out.Times["sunrise"]) != 3 {
		t.Errorf("expected 3 days of sunrise, got %d", len(out.Times["sunrise"]))
	}
	if cc := resp.Header.Get("Cache-Control"); cc != cacheControl30Days {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestZmanimMelacha(t *testing.T) {
	srv := testServerWithDB(t)
	// 2026-07-11 is a Saturday (Shabbat); mid-afternoon is assur bemlacha
	_, body := get(t, srv,
		"/zmanim?cfg=json&geonameid=281184&im=1&dt=2026-07-11T15:00:00")
	var out struct {
		Status struct {
			LocalTime       string `json:"localTime"`
			IsAssurBemlacha bool   `json:"isAssurBemlacha"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if !out.Status.IsAssurBemlacha {
		t.Errorf("Saturday afternoon should be assur bemlacha: %s", body)
	}
	if !strings.HasPrefix(out.Status.LocalTime, "2026-07-11T15:00:00") {
		t.Errorf("localTime = %q", out.Status.LocalTime)
	}
	// boundary cases cross-checked against @hebcal/core isAssurBemlacha:
	//   Wed 15:00 = false, Fri 20:00 (after shkiah) = true,
	//   Sat 21:30 (after tzeit) = false.
	for _, tc := range []struct {
		dt   string
		want bool
	}{
		{"2026-07-08T15:00:00", false},
		{"2026-07-10T20:00:00", true},
		{"2026-07-11T21:30:00", false},
	} {
		_, b := get(t, srv, "/zmanim?cfg=json&geonameid=281184&im=1&dt="+tc.dt)
		has := strings.Contains(b, `"isAssurBemlacha":true`)
		if has != tc.want {
			t.Errorf("dt=%s isAssurBemlacha=%v, want %v: %s", tc.dt, has, tc.want, b)
		}
	}
}

func TestZmanimErrors(t *testing.T) {
	srv := testServerWithDB(t)
	cases := []struct {
		path string
		want int
	}{
		{"/zmanim?geonameid=281184", 400},                          // cfg missing
		{"/zmanim?cfg=json", 400},                                  // location missing
		{"/zmanim?cfg=json&geonameid=99999999", 404},               // unknown geonameid
		{"/zmanim?cfg=json&zip=00000", 404},                        // unknown zip
		{"/zmanim?cfg=json&latitude=99&longitude=0&tzid=UTC", 400}, // bad latitude
		{"/zmanim?cfg=json&latitude=40&longitude=-74", 400},        // tzid required
	}
	for _, tc := range cases {
		resp, body := get(t, srv, tc.path)
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status = %d, want %d (%s)", tc.path, resp.StatusCode, tc.want, body)
		}
		if !strings.Contains(body, `"error"`) {
			t.Errorf("%s: expected error body, got %s", tc.path, body)
		}
	}
}

func TestZmanimNoDB(t *testing.T) {
	// without a db, /zmanim reports 503 but the rest of the service still works
	_, srv := testServer(t)
	resp, _ := get(t, srv, "/zmanim?cfg=json&geonameid=281184")
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestZmanimCORS(t *testing.T) {
	srv := testServerWithDB(t)
	// OPTIONS preflight returns 204 with CORS headers and no MethodNotAllowed
	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/zmanim", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Errorf("OPTIONS status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got != "GET" {
		t.Errorf("Access-Control-Allow-Methods = %q, want GET", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "cross-origin" {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want cross-origin", got)
	}
	// unsupported methods are still rejected
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/zmanim", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 405 {
		t.Errorf("DELETE status = %d, want 405", resp2.StatusCode)
	}
}

func TestZmanimOrderedOutput(t *testing.T) {
	srv := testServerWithDB(t)
	_, body := get(t, srv, "/zmanim?cfg=json&geonameid=281184&date=2026-07-07")
	// the top-level keys must appear in this order
	for _, pair := range [][2]string{
		{`"date"`, `"version"`}, {`"version"`, `"location"`}, {`"location"`, `"times"`},
	} {
		if strings.Index(body, pair[0]) >= strings.Index(body, pair[1]) {
			t.Errorf("expected %s before %s", pair[0], pair[1])
		}
	}
	// within times, chatzotNight comes before sunrise before sunset
	ti := strings.Index(body, `"times"`)
	sub := body[ti:]
	if !(strings.Index(sub, `"chatzotNight"`) < strings.Index(sub, `"sunrise"`) &&
		strings.Index(sub, `"sunrise"`) < strings.Index(sub, `"sunset"`)) {
		t.Errorf("times not in expected order: %s", sub)
	}
}
