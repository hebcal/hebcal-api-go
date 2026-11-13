package handler

import (
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// getICS fetches a feed and requires a served calendar: 200, not a redirect.
func getICS(t *testing.T, path string, headers ...string) (*http.Response, string) {
	t.Helper()
	_, srv := testServer(t)
	resp, body := get(t, srv, path, headers...)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d for %s: %s", resp.StatusCode, path, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/calendar; charset=utf-8" {
		t.Errorf("Content-Type %q", ct)
	}
	if resp.Header.Get("Location") != "" {
		t.Error("unexpected redirect")
	}
	if !strings.HasPrefix(body, "BEGIN:VCALENDAR\r\n") || !strings.HasSuffix(body, "END:VCALENDAR\r\n") {
		t.Errorf("not a calendar: %.80q", body)
	}
	return resp, body
}

func v2y(qs string) string {
	return "/v2/y/" + base64.RawStdEncoding.EncodeToString([]byte(qs)) + "/yahrzeit_20200429194651.ics"
}

// The legacy subscription URLs calendar clients still poll, a decade on, in
// each of their shapes.
func TestYahrzeitLegacyURLs(t *testing.T) {
	t.Run("export with semicolons", func(t *testing.T) {
		_, body := getICS(t, "/export/74/9cf0af2d92f0843bb84e1eb7956854.ics"+
			"?subscribe=1;t1=Yahrzeit;d1=23;m1=12;y1=2003;n1=Ploni%20Almoni;hebdate=on;years=20;v=yahrzeit")
		if !strings.Contains(body, "Ploni Almoni") {
			t.Error("name missing")
		}
	})
	t.Run("cgi with redir=1 and ampersands", func(t *testing.T) {
		resp, body := getICS(t, "/yahrzeit/yahrzeit.cgi/anniversary_20150924153350.ics"+
			"?redir=1&dl=1"+
			"&t1=Birthday&n1=Ploni%20Almoni&d1=22&m1=12&y1=1983&s1=off"+
			"&t2=Birthday&n2=Plonit%20Almonit&d2=1&m2=10&y2=1984&s2=on"+
			"&t3=Birthday&n3=Moshe%20Cohen&d3=6&m3=3&y3=1989&s3=on&v=yahrzeit")
		for _, name := range []string{"Ploni Almoni", "Plonit Almonit", "Moshe Cohen"} {
			if !strings.Contains(body, name) {
				t.Errorf("%s missing", name)
			}
		}
		if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="anniversary_20150924153350.ics"` {
			t.Errorf("Content-Disposition %q", cd)
		}
	})
	t.Run("cgi with semicolons", func(t *testing.T) {
		_, body := getICS(t, "/yahrzeit/yahrzeit.cgi/yahrzeit_20160110183144.ics"+
			"?dl=1;t1=Yahrzeit;n1=Ploni%20Almoni;d1=23;m1=12;y1=2015;s1=off"+
			";t2=Yahrzeit;n2=Plonit%20Almonit;d2=13;m2=9;y2=2013;s2=off"+
			";t3=Yahrzeit;n3=Moshe%20Cohen;d3=31;m3=10;y3=1997;s3=off;v=yahrzeit")
		for _, name := range []string{"Ploni Almoni", "Plonit Almonit", "Moshe Cohen"} {
			if !strings.Contains(body, name) {
				t.Errorf("%s missing", name)
			}
		}
	})
	t.Run("v2 keeps the anniversary numbers", func(t *testing.T) {
		resp, body := getICS(t, v2y("d1=27&hebdate=on&m1=4&n1=Person1&s1=off&t1=Yahrzeit&v=yahrzeit&y1=2020&years=20&yizkor=on"))
		if !regexp.MustCompile(`SUMMARY:🕯️ Person1’s \d+(st|nd|rd|th) Yahrzeit`).MatchString(body) {
			t.Error("no numbered Yahrzeit")
		}
		if !strings.Contains(body, "SUMMARY:🕯️ Person1 Yahrzeit reminder\r\n") || !strings.Contains(body, "SUMMARY:🕯️ Yizkor (Yom Kippur)\r\n") {
			t.Error("reminder or Yizkor missing")
		}
		if resp.Header.Get("Content-Disposition") != "" {
			t.Error("a subscription is not an attachment")
		}
	})
	t.Run("v2 hebrew name", func(t *testing.T) {
		_, body := getICS(t, v2y("d1=29&hebdate=on&m1=1&n1=%D7%92%D7%95%D7%9C%D7%93%D7%94+%D7%9E%D7%90%D7%99%D7%A8&t1=Yahrzeit&v=yahrzeit&y1=2013"))
		body = strings.ReplaceAll(body, "\r\n ", "")
		if !regexp.MustCompile(`SUMMARY:🕯️ יארצייט ה-\d+ של גולדה מאיר \(י״ח שבט\)\r\n`).MatchString(body) {
			t.Errorf("Hebrew title missing:\n%.900s", body)
		}
		if !strings.Contains(body, "SUMMARY:🕯️ גולדה מאיר יארצייט תזכורת\r\n") {
			t.Error("Hebrew reminder missing")
		}
		if !strings.Contains(body, "X-WR-CALNAME:Yahrzeit: גולדה מאיר\r\n") {
			t.Error("calendar name")
		}
	})
	t.Run("double-encoded query", func(t *testing.T) {
		_, body := getICS(t, "/export/x.ics?subscribe=1%3Bt1%3DYahrzeit%3Bd1%3D23%3Bm1%3D12%3By1%3D2003%3Bn1%3DPloni%2520Almoni%3Bv%3Dyahrzeit")
		if !strings.Contains(body, "X-WR-CALNAME:Yahrzeit: Ploni Almoni\r\n") {
			t.Error("name not decoded")
		}
	})
	t.Run("query encoded into the path", func(t *testing.T) {
		_, body := getICS(t, "/export/74/x.ics%3Fsubscribe%3D1%3Bt1%3DYahrzeit%3Bd1%3D23%3Bm1%3D12%3By1%3D2003%3Bn1%3DPloni%2520Almoni%3Bv%3Dyahrzeit")
		if !strings.Contains(body, "X-WR-CALNAME:Yahrzeit: Ploni Almoni\r\n") {
			t.Error("name not decoded")
		}
	})
	t.Run("no v but a first entry", func(t *testing.T) {
		_, body := getICS(t, "/export/x.ics?y1=2003&m1=12&d1=23")
		if !strings.Contains(body, "Person1’s") {
			t.Error("unnamed entry should be Person1")
		}
	})
}

func TestYahrzeitHeaders(t *testing.T) {
	const url = "/export/x.ics?v=yahrzeit&t1=Yahrzeit&n1=Theodor+Herzl&y1=1904&m1=7&d1=3"
	resp, body := getICS(t, url)
	h := resp.Header
	if h.Get("ETag") == "" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers: %v", h)
	}
	// A personal calendar is not for a shared cache to keep.
	if cc := h.Get("Cache-Control"); cc != "" {
		t.Errorf("Cache-Control %q", cc)
	}
	// Google's importer gets a different body, and the response is large
	// enough to be compressed.
	if vary := strings.Join(h.Values("Vary"), ", "); vary != "User-Agent, Accept-Encoding" {
		t.Errorf("Vary %q", vary)
	}

	_, srv := testServer(t)
	resp2, body2 := get(t, srv, url, "If-None-Match", h.Get("ETag"))
	if resp2.StatusCode != http.StatusNotModified || body2 != "" {
		t.Errorf("conditional request: %d", resp2.StatusCode)
	}
	if resp2.Header.Get("ETag") != h.Get("ETag") {
		t.Error("304 should repeat the ETag")
	}

	gresp, gbody := getICS(t, url+"&tzo=300", "User-Agent", "Google-Calendar-Importer")
	if !strings.Contains(gbody, "X-WR-TIMEZONE;VALUE=TEXT:America/New_York\r\n") ||
		!regexp.MustCompile(`DTSTART;TZID=America/New_York:\d{8}T\d{6}\r\n`).MatchString(gbody) ||
		!strings.Contains(gbody, "GEO:0;0\r\n") {
		t.Errorf("Google's copy should pin the reminders to a zone:\n%.700s", gbody)
	}
	if strings.Contains(body, "TZID") {
		t.Error("everyone else gets floating times")
	}
	presp, plain := getICS(t, url+"&tzo=300")
	if g, p := gresp.Header.Get("ETag"), presp.Header.Get("ETag"); g == p || plain == gbody {
		t.Error("the two renderings need different validators")
	}

	resp3, _ := get(t, srv, url, "Accept-Encoding", "br")
	if resp3.Header.Get("Content-Encoding") != "br" {
		t.Error("feed should be compressed")
	}
	if resp3.Header.Get("ETag") == h.Get("ETag") {
		t.Error("ETag should vary with the encoding")
	}
}

// A subscription's body changes when the week does, so its validator must.
func TestYahrzeitETagFollowsTheWeek(t *testing.T) {
	app, srv := testServer(t)
	etag := func(path string) string {
		resp, _ := get(t, srv, path)
		return resp.Header.Get("ETag")
	}
	const sub = "/export/x.ics?v=yahrzeit&n1=Theodor+Herzl&y1=1904&m1=7&d1=3"
	sub1, dl1 := etag(sub), etag(sub+"&dl=1")
	app.Now = func() model.GregDate { return model.GregDate{Year: 2026, Month: time.July, Day: 11} }
	if etag(sub) != sub1 {
		t.Error("changed within the week")
	}
	app.Now = func() model.GregDate { return model.GregDate{Year: 2026, Month: time.July, Day: 12} }
	if etag(sub) == sub1 {
		t.Error("unchanged across a Sunday")
	}
	if etag(sub+"&dl=1") != dl1 {
		t.Error("a download does not depend on the week")
	}
}

func TestYahrzeitVTimezone(t *testing.T) {
	app, srv := testServer(t)
	app.Zones = icalendar.NewZoneInfo("../service/yahrzeit/testdata/zoneinfo")
	_, body := get(t, srv, "/export/x.ics?v=yahrzeit&t1=Yahrzeit&n1=Theodor+Herzl&y1=1904&m1=7&d1=3&tzo=600",
		"User-Agent", "Google-Calendar-Importer")
	if !strings.Contains(body, "X-WR-TIMEZONE;VALUE=TEXT:Pacific/Honolulu\r\nBEGIN:VTIMEZONE\r\nTZID:Pacific/Honolulu\r\n") ||
		!strings.Contains(body, "END:VTIMEZONE\r\nBEGIN:VEVENT\r\n") {
		t.Errorf("VTIMEZONE missing:\n%.600s", body)
	}
}

func TestYahrzeitEmptyCalendar(t *testing.T) {
	resp, body := getICS(t, "/export/x.ics?v=yahrzeit")
	if !strings.Contains(body, "SUMMARY:Calendar contains no events\r\n") {
		t.Errorf("no placeholder: %s", body)
	}
	if resp.Header.Get("ETag") != "" || resp.Header.Get("Cache-Control") != "max-age=86400" {
		t.Errorf("headers: %v", resp.Header)
	}
}

func TestYahrzeitErrors(t *testing.T) {
	_, srv := testServer(t)
	tests := []struct {
		name, path string
		status     int
		body       string
	}{
		{"invalid date", "/export/x.ics?v=yahrzeit&d1=31&m1=2&y1=2003", 400, "Gregorian day 31 out of valid range for 2/2003\n"},
		{"other format", "/export/x.txt?v=yahrzeit&d1=3&m1=2&y1=2003", 404, "unknown download extension .txt\n"},
		{"other format, /v2/y/", "/v2/y/dj15YWhyemVpdCZkMT0zJm0xPTImeTE9MjAwMw/x.foo", 404, "unknown download extension .foo\n"},
		{"a four-letter extension", "/export/x.json?v=yahrzeit&d1=3&m1=2&y1=2003", 404, "unknown download extension .json\n"},
		{"invalid date, csv", "/yahrzeit/yahrzeit.cgi/x.csv?v=yahrzeit&d1=31&m1=2&y1=2003", 400, "Gregorian day 31 out of valid range for 2/2003\n"},
		{"cgi path without a calendar", "/yahrzeit/yahrzeit.cgi/x.ics?d1=3&m1=2&y1=2003", 404, "Invalid download URL: v=undefined\n"},
		{"cgi path, holiday calendar", "/yahrzeit/yahrzeit.cgi/x.ics?v=1&year=2026", 404, "Invalid download URL: v=undefined\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := get(t, srv, tt.path)
			if resp.StatusCode != tt.status || (tt.body != "" && body != tt.body) {
				t.Errorf("got %d %q", resp.StatusCode, body)
			}
			for _, k := range []string{"ETag", "Cache-Control", "Vary"} {
				if v := resp.Header.Get(k); v != "" {
					t.Errorf("error response carries %s: %s", k, v)
				}
			}
		})
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/export/x.ics?v=yahrzeit&d1=3&m1=2&y1=2003", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: %d, Allow %q", resp.StatusCode, resp.Header.Get("Allow"))
	}
}

func TestYahrzeitHead(t *testing.T) {
	_, srv := testServer(t)
	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/export/x.ics?v=yahrzeit&n1=Theodor+Herzl&y1=1904&m1=7&d1=3", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength <= 0 || resp.Header.Get("ETag") == "" {
		t.Errorf("HEAD: %d, length %d", resp.StatusCode, resp.ContentLength)
	}
}

// The .csv export: every URL shape, and the feed's headers minus what depends
// on the client.
func TestYahrzeitCSV(t *testing.T) {
	_, srv := testServer(t)
	const qs = "v=yahrzeit&n1=Theodor+Herzl&y1=1904&m1=7&d1=3&years=3"
	paths := []string{
		strings.TrimSuffix(v2y(qs), ".ics") + ".csv",
		"/yahrzeit/yahrzeit.cgi/x.csv?" + qs,
		"/export/x.csv?" + qs,
		"/hebcal/index.cgi/x.csv?" + strings.ReplaceAll(qs, "&", ";"),
	}
	for _, p := range paths {
		resp, body := get(t, srv, p)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/x-csv; charset=utf-8" {
			t.Fatalf("%s: %d %q %.80q", p, resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}
		if !strings.HasPrefix(body, "\xEF\xBB\xBFSubject,") || strings.Count(body, "\r\n") != 4 ||
			!strings.Contains(body, "Theodor Herzl’s") || strings.Contains(body, "reminder") {
			t.Errorf("%s: body %q", p, body)
		}
		for _, k := range []string{"Cache-Control", "Content-Disposition"} {
			if v := resp.Header.Get(k); v != "" {
				t.Errorf("%s: unexpected %s: %s", p, k, v)
			}
		}
		if v := resp.Header.Values("Vary"); strings.Contains(strings.Join(v, ","), "User-Agent") {
			t.Errorf("%s: a CSV does not vary by client: %v", p, v)
		}
		etag := resp.Header.Get("ETag")
		if etag == "" {
			t.Errorf("%s: no ETag", p)
		}
		resp, _ = get(t, srv, p, "If-None-Match", etag)
		if resp.StatusCode != http.StatusNotModified {
			t.Errorf("%s: conditional request got %d", p, resp.StatusCode)
		}
	}
	resp, _ := get(t, srv, "/export/yahrzeit.csv?dl=1&"+qs)
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="yahrzeit.csv"` {
		t.Errorf("dl=1: Content-Disposition %q", cd)
	}
}

// The PDF and CSV calendars share these URL shapes and must be unaffected.
func TestYahrzeitLeavesOtherDownloadsAlone(t *testing.T) {
	_, srv := testServer(t)
	resp, body := get(t, srv, "/export/x.csv?v=1&year=2026&maj=on")
	if resp.StatusCode != 200 || !strings.Contains(body, "Subject") {
		t.Errorf("holiday CSV: %d %.80q", resp.StatusCode, body)
	}
}
