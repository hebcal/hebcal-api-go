package main

import (
	"net/http"
	"strings"
	"testing"
)

// The expected bodies below were captured from hebcal-web's /geo route
// (getLocationFromQuery serialized by Koa) using the same testdata databases,
// so they double as a byte-for-byte parity check with the Node implementation.

func TestGeoGeoname(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?geonameid=281184")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", ct, contentTypeJSON)
	}
	want := `{"latitude":31.76904,"longitude":35.21633,"locationName":"Jerusalem, Israel","timeZoneId":"Asia/Jerusalem","elevation":786,"il":true,"cc":"IL","geoid":281184,"admin1":"Jerusalem District","geo":"geoname","population":801000,"asciiname":"Jerusalem","geonameid":281184}`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestGeoGeonameUSA(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?geonameid=5128581")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	want := `{"latitude":40.71427,"longitude":-74.00597,"locationName":"New York, USA","timeZoneId":"America/New_York","elevation":57,"il":false,"cc":"US","geoid":5128581,"admin1":"New York","geo":"geoname","population":8175133,"asciiname":"New York","geonameid":5128581}`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestGeoZip(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?zip=90210")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	// geoid is the ZIP string (not a number), and "state" is emitted last.
	want := `{"latitude":34.103131,"longitude":-118.416253,"locationName":"Beverly Hills, CA 90210","timeZoneId":"America/Los_Angeles","elevation":719,"il":false,"cc":"US","geoid":"90210","admin1":"CA","stateName":"California","geo":"zip","zip":"90210","population":21134,"state":"CA"}`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestGeoLegacyCity(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?city=IL-Jerusalem")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	// A legacy city resolves through the GeoNames id, so it matches geonameid=281184.
	want := `{"latitude":31.76904,"longitude":35.21633,"locationName":"Jerusalem, Israel","timeZoneId":"Asia/Jerusalem","elevation":786,"il":true,"cc":"IL","geoid":281184,"admin1":"Jerusalem District","geo":"geoname","population":801000,"asciiname":"Jerusalem","geonameid":281184}`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestGeoPos(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?latitude=37.5&longitude=-122.3&tzid=America/Los_Angeles")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	// geo=pos: no cc/geoid, elevation defaults to 0 but is still present.
	want := `{"latitude":37.5,"longitude":-122.3,"locationName":"37°30′N 122°17′W America/Los_Angeles","timeZoneId":"America/Los_Angeles","elevation":0,"il":false,"geo":"pos"}`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestGeoPosIsrael(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?latitude=31.5&longitude=34.9&tzid=Asia/Jerusalem")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	// Asia/Jerusalem forces il=true even without i=on.
	if !strings.Contains(body, `"il":true`) {
		t.Errorf("expected il=true for Asia/Jerusalem, got %s", body)
	}
}

func TestGeoNoParams(t *testing.T) {
	srv := testServerWithDB(t)
	// No location parameters: hebcal-web assigns ctx.body = null => 204.
	resp, body := get(t, srv, "/geo")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", resp.StatusCode, body)
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestGeoUnknownGeoname(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?geonameid=999999999")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"error"`) {
		t.Errorf("expected JSON error, got %s", body)
	}
}

func TestGeoInvalidZip(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?zip=abc")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, body)
	}
}

func TestGeoUnknownZip(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/geo?zip=00000")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
}

func TestGeoRejectsPost(t *testing.T) {
	srv := testServerWithDB(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/geo?geonameid=281184", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", allow, "GET, HEAD")
	}
}

func TestGeoOptions(t *testing.T) {
	srv := testServerWithDB(t)
	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/geo", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if origin := resp.Header.Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", origin)
	}
	if m := resp.Header.Get("Access-Control-Allow-Methods"); m != "GET" {
		t.Errorf("Access-Control-Allow-Methods = %q, want GET", m)
	}
}

func TestGeoDBUnavailable(t *testing.T) {
	// No app.db set: /geo should report 503.
	_, srv := testServer(t)
	resp, body := get(t, srv, "/geo?geonameid=281184")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", resp.StatusCode, body)
	}
}
