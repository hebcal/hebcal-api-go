package handler

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings/readingstest"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// The transport half of the iCalendar feeds. What each event says is
// internal/service/ical's business, and its golden files'.

// icsServer is a server whose feeds can reach a location database and a
// readings-svc that has no readings to add.
func icsServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	app, srv := testServer(t)
	app.ICS.Geo = testGeoDB(t)
	app.ICS.Readings = readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	app.ICS.Clock = func() time.Time { return time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC) }
	return app, srv
}

func TestICSResponse(t *testing.T) {
	_, srv := icsServer(t)
	msg := &pb.Download{Year: 2026, Month: 12, Major: true, Candlelighting: true, Geonameid: 5128581,
		HavdalahTzeit: true}
	path := "/v4/" + encode(t, msg) + "/hebcal_2026_new_york.ics"

	resp, body := get(t, srv, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	for header, want := range map[string]string{
		"Content-Type":           "text/calendar; charset=utf-8",
		"Content-Disposition":    `attachment; filename="hebcal_2026_new_york.ics"`,
		"Cache-Control":          httpx.CacheControl14Days,
		"X-Content-Type-Options": "nosniff",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n",
		"X-WR-CALNAME:Hebcal New York December 2026\r\n",
		"X-WR-TIMEZONE;VALUE=TEXT:America/New_York\r\n",
		"SUMMARY:Candle lighting\r\n",
		"LOCATION:New York\r\n",
		"GEO:40.71427;-74.00597\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	resp, _ = get(t, srv, path, "If-None-Match", etag)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("conditional request: status = %d, want 304", resp.StatusCode)
	}
}

// A subscription is shown rather than saved.
func TestICSSubscriptionIsInline(t *testing.T) {
	_, srv := icsServer(t)
	msg := &pb.Download{Year: 2026, Month: 12, Major: true, Subscribe: true}
	resp, body := get(t, srv, "/v4/"+encode(t, msg)+"/hebcal.ics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Disposition"); got != "" {
		t.Errorf("Content-Disposition = %q, want none for a subscription", got)
	}
	if !strings.Contains(body, "X-WR-CALNAME:Hebcal Diaspora\r\n") {
		t.Errorf("a subscription's title carries no dates:\n%.400s", body)
	}
}

func TestICSErrors(t *testing.T) {
	app, srv := testServer(t)
	app.ICS.Geo = testGeoDB(t)
	// no readings-svc is configured: a feed with readings in it is refused
	tests := []struct {
		name string
		msg  *pb.Download
		want int
	}{
		{"unknown location", &pb.Download{Year: 2026, Major: true, Geonameid: 1}, http.StatusNotFound},
		{"year out of range", &pb.Download{Year: 9999, Major: true}, http.StatusGone},
		// no readings-svc is configured
		{"unsupported series", &pb.Download{Year: 2026, Major: true, DirshuAmudYomi: true}, http.StatusNotImplemented},
		{"readings needed", &pb.Download{Year: 2026, Major: true}, http.StatusNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := get(t, srv, "/v4/"+encode(t, tc.msg)+"/hebcal.ics")
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d: %s", resp.StatusCode, tc.want, body)
			}
			if tc.want != http.StatusGone && resp.Header.Get("Cache-Control") != "" {
				t.Errorf("an error carries Cache-Control %q", resp.Header.Get("Cache-Control"))
			}
		})
	}
	resp, _ := get(t, srv, "/v4/!!!/hebcal.ics")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("undecodable payload: status = %d, want 404", resp.StatusCode)
	}
	resp, _ = get(t, srv, "/hebcal/index.cgi/hebcal.ics?year=2026&maj=on")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("no v=: status = %d, want 404", resp.StatusCode)
	}
}

// A readings-svc that does not answer is a 503 with Retry-After, so the proxy
// goes on serving the copy it has; it is never a feed missing its readings.
func TestICSReadingsDown(t *testing.T) {
	app, srv := testServer(t)
	app.ICS.Readings = readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // an older readings-svc has no /torahMemo
	}))
	resp, body := get(t, srv, "/hebcal/index.cgi/hebcal.ics?v=1&year=2026&month=3&maj=on")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
	if resp.Header.Get("ETag") != "" {
		t.Error("an error carries an ETag")
	}
}

// A calendar with nothing in it is a 200 with one placeholder event, good for
// a day, and with no ETag.
func TestICSEmpty(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/hebcal/index.cgi/hebcal.ics?v=1&year=2026&subscribe=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Cache-Control"); got != "max-age=86400" {
		t.Errorf("Cache-Control = %q, want max-age=86400", got)
	}
	if got := resp.Header.Get("ETag"); got != "" {
		t.Errorf("ETag = %q, want none", got)
	}
	if !strings.Contains(body, "SUMMARY:Calendar contains no events") {
		t.Errorf("no placeholder:\n%s", body)
	}
}

// The three URL shapes of one request are one feed.
func TestICSLegacyURLs(t *testing.T) {
	_, srv := icsServer(t)
	const qs = "v=1&geonameid=5128581&year=2026&month=12&c=on&maj=on&M=on&subscribe=1"
	_, want := get(t, srv, "/v4/"+encode(t, &pb.Download{Year: 2026, Month: 12, Major: true,
		Candlelighting: true, Geonameid: 5128581, HavdalahTzeit: true, Subscribe: true})+"/hebcal.ics")
	_, got := get(t, srv, "/hebcal/index.cgi/hebcal.ics?"+qs)
	if want != got {
		t.Error("the classic query-string URL renders differently from its /v4/ form")
	}
	b64 := strings.NewReplacer("+", "-", "/", "_", "=", "").Replace(base64.StdEncoding.EncodeToString([]byte(qs)))
	_, got = get(t, srv, "/v2/h/"+b64+"/hebcal.ics")
	if want != got {
		t.Error("the /v2/h/ URL renders differently from its /v4/ form")
	}
}

// A subscription too long for calendar applications is cut, which makes it
// depend on the week: the tag it carries differs from the uncut one and moves
// with the Sunday.
func TestICSLimitedETagMovesWithTheWeek(t *testing.T) {
	app, srv := icsServer(t)
	const path = "/hebcal/index.cgi/hebcal.ics?v=1&year=2026&ny=10&maj=on&min=on&nx=on&mf=on&ss=on&mod=on&s=on&o=on&d=on&c=on&geonameid=5128581&b=18&M=on&subscribe=1"
	resp, body := get(t, srv, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %.200s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Hebcal calendar feed truncated") {
		t.Fatal("the subscription was not cut")
	}
	etag := resp.Header.Get("ETag")
	resp, _ = get(t, srv, path, "If-None-Match", etag)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("same week: status = %d, want 304", resp.StatusCode)
	}
	app.ICS.Clock = func() time.Time { return time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC) }
	resp, _ = get(t, srv, path, "If-None-Match", etag)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("next week: status = %d, want 200 (the cut moves with the week)", resp.StatusCode)
	}
}

func TestICSLearningFallback(t *testing.T) {
	app, srv := testServer(t)
	up := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"title":"Yoma 49a","date":"2026-08-01",` +
			`"category":"dirshuAmudYomi","memo":"first line\nsecond, line"}]}`))
	}))
	app.ICS.Learning = download.NewLearningFetcher(up)
	msg := &pb.Download{Year: 2026, Month: 8, DirshuAmudYomi: true}
	resp, body := get(t, srv, "/v4/"+encode(t, msg)+"/hebcal.ics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		"SUMMARY:Yoma 49a\r\n",
		"DTSTART;VALUE=DATE:20260801\r\n",
		"CATEGORIES:Dirshu Amud HaYomi\r\n",
		"UID:hebcal-20260801-",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
}

// A calendar with a location names its zone, and carries the zone's definition
// when there is one.
func TestICSVTimezone(t *testing.T) {
	app, srv := icsServer(t)
	app.Zones = icalendar.NewZoneInfo("../../pkg/icalendar/testdata/zoneinfo")
	msg := &pb.Download{Year: 2026, Month: 12, Major: true, Candlelighting: true, GeoPos: true,
		LatOneof: &pb.Download_Latitude{Latitude: 34.05}, LongOneof: &pb.Download_Longitude{Longitude: -118.24},
		Tzid: "America/Los_Angeles", CityName: "Los Angeles, CA", HavdalahTzeit: true}
	resp, body := get(t, srv, "/v4/"+encode(t, msg)+"/hebcal.ics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		"X-WR-TIMEZONE;VALUE=TEXT:America/Los_Angeles\r\n",
		"BEGIN:VTIMEZONE\r\n",
		"DTSTART;TZID=America/Los_Angeles:",
		"LOCATION:Los Angeles\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}
