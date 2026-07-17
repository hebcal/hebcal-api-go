package main

import (
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Expected bodies were captured from @hebcal/geo-sqlite GeoDb.autoComplete run
// against the testdata databases, with the country flag appended by the
// hebcal-web /complete handler, giving byte-for-byte parity with Node.

func TestCompleteGeoname(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/complete?q=Jerusa")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", ct, contentTypeJSON)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private, max-age=259200" {
		t.Errorf("Cache-Control = %q, want %q", cc, "private, max-age=259200")
	}
	// Without g=on: no latitude/longitude/timezone/population.
	want := `[{"id":281184,"value":"Jerusalem, Israel","admin1":"Jerusalem District","country":"Israel","cc":"IL","geo":"geoname","asciiname":"Jerusalem","flag":"🇮🇱"}]`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestCompleteGeonameLatLong(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/complete?q=Jerusa&g=on")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	want := `[{"id":281184,"value":"Jerusalem, Israel","admin1":"Jerusalem District","country":"Israel","cc":"IL","latitude":31.76904,"longitude":35.21633,"timezone":"Asia/Jerusalem","geo":"geoname","population":801000,"asciiname":"Jerusalem","flag":"🇮🇱"}]`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestCompleteZipText(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/complete?q=Bever")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	want := `[{"id":"90210","value":"Beverly Hills, CA 90210","admin1":"CA","asciiname":"Beverly Hills","country":"United States","cc":"US","geo":"zip","flag":"🇺🇸"}]`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestCompleteZipPrefix(t *testing.T) {
	srv := testServerWithDB(t)
	// Numeric prefix keeps latitude/longitude/timezone even without g=on,
	// but the handler still strips population.
	resp, body := get(t, srv, "/complete?q=902")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	want := `[{"id":"90210","value":"Beverly Hills, CA 90210","admin1":"CA","asciiname":"Beverly Hills","country":"United States","cc":"US","latitude":34.103131,"longitude":-118.416253,"timezone":"America/Los_Angeles","geo":"zip","flag":"🇺🇸"}]`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestCompleteZipExact(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/complete?q=90210")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	want := `[{"id":"90210","value":"Beverly Hills, CA 90210","admin1":"CA","asciiname":"Beverly Hills","country":"United States","cc":"US","latitude":34.103131,"longitude":-118.416253,"timezone":"America/Los_Angeles","geo":"zip","flag":"🇺🇸"}]`
	if body != want {
		t.Errorf("body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestCompletePhpAlias(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/complete.php?q=Jerusa")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"id":281184`) {
		t.Errorf("expected Jerusalem result from /complete.php, got %s", body)
	}
}

func TestCompleteNoResults(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/complete?q=zzzznotacity")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
	if body != `{"error":"Not Found"}` {
		t.Errorf("body = %s, want Not Found error", body)
	}
	// hebcal-web drops the ETag on the no-results 404 but keeps Cache-Control.
	if etag := resp.Header.Get("ETag"); etag != "" {
		t.Errorf("expected no ETag on no-results 404, got %q", etag)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private, max-age=259200" {
		t.Errorf("Cache-Control = %q, want %q", cc, "private, max-age=259200")
	}
}

func TestCompleteEmptyQuery(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/complete")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
	if body != `{"error":"Not Found"}` {
		t.Errorf("body = %s, want Not Found error", body)
	}
	// The empty-query 404 is returned before any Cache-Control is set.
	if cc := resp.Header.Get("Cache-Control"); cc != "" {
		t.Errorf("expected no Cache-Control on empty-query 404, got %q", cc)
	}
}

func TestCompleteETag304(t *testing.T) {
	srv := testServerWithDB(t)
	resp, _ := get(t, srv, "/complete?q=Jerusa")
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("expected an ETag on the /complete response")
	}
	resp2, body2 := get(t, srv, "/complete?q=Jerusa", "If-None-Match", etag)
	if resp2.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304; body=%s", resp2.StatusCode, body2)
	}
	if body2 != "" {
		t.Errorf("expected empty 304 body, got %q", body2)
	}
}

func TestCompleteOptions(t *testing.T) {
	srv := testServerWithDB(t)
	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/complete", nil)
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

func TestCompleteDBUnavailable(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/complete?q=Jerusa")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", resp.StatusCode, body)
	}
}

func TestAutocompleteSortNearMountainView(t *testing.T) {
	items := []acItem{
		{value: "Santiago, Chile", population: 4837295, latitude: -33.45694, longitude: -70.64827},
		{value: "San Jose, CA", population: 945942, latitude: 37.33939, longitude: -121.89496},
		{value: "San Francisco, CA", population: 873965, latitude: 37.77493, longitude: -122.41942},
		{value: "Santa Clara, CA", population: 127647, latitude: 37.35411, longitude: -121.95524},
	}
	sortAutocomplete(items, &geoIPPoint{Latitude: 37.3861, Longitude: -122.0839})
	nearby := map[string]bool{"San Jose, CA": true, "San Francisco, CA": true, "Santa Clara, CA": true}
	for i := 0; i < 3; i++ {
		if !nearby[items[i].value] {
			t.Fatalf("rank %d = %q, want a Bay Area city; all=%v", i, items[i].value, []string{items[0].value, items[1].value, items[2].value, items[3].value})
		}
	}
	if items[3].value != "Santiago, Chile" {
		t.Fatalf("last = %q, want distant Santiago below Bay Area matches", items[3].value)
	}
}

func TestAutocompleteSortFallsBackToPopulation(t *testing.T) {
	items := []acItem{
		{value: "San Jose, CA", population: 945942, latitude: 37.33939, longitude: -121.89496},
		{value: "Santiago, Chile", population: 4837295, latitude: -33.45694, longitude: -70.64827},
	}
	sortAutocomplete(items, nil)
	if items[0].value != "Santiago, Chile" {
		t.Fatalf("first = %q, want Santiago population fallback", items[0].value)
	}
}

func TestGeoIPClientReusesUnixSocketConnection(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "geoip2.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	var newConns int
	srv := &http.Server{ConnState: func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns++
		}
	}}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"location":{"latitude":37.3861,"longitude":-122.0839}}`)
	})
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client := newGeoIPClient(socketPath)
	for i := 0; i < 2; i++ {
		if _, err := client.lookupPoint(t.Context(), "8.8.8.8"); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	if newConns != 1 {
		t.Fatalf("new unix socket connections = %d, want 1", newConns)
	}
}

func TestLookupGeoIPPointUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "geoip2.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 56\r\n\r\n{\"location\":{\"latitude\":37.3861,\"longitude\":-122.0839}}"))
	}()
	pt, err := newGeoIPClient(socketPath).lookupPoint(t.Context(), "8.8.8.8")
	if err != nil {
		t.Fatalf("lookupGeoIPPoint: %v", err)
	}
	if pt.Latitude != 37.3861 || pt.Longitude != -122.0839 {
		t.Fatalf("point = %#v, want Mountain View coordinates", pt)
	}
}
