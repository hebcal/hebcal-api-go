package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// feedClock is the instant the expected lines below were captured from
// hebcal-web's download server: 2026-11-10 in New York and Jerusalem, so
// every feed starts on 2026-11-09.
var feedClock = time.Date(2026, time.November, 10, 21, 0, 0, 0, time.UTC)

func getFeed(t *testing.T, path string, headers ...string) (*http.Response, string) {
	t.Helper()
	app, _ := testServer(t)
	db, err := geodb.New("testdata/zips.sqlite3", "testdata/geonames.sqlite3")
	if err != nil {
		t.Fatalf("geodb.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app.DB = db
	app.Clock = func() time.Time { return feedClock }
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	return get(t, srv, path, headers...)
}

func requireLines(t *testing.T, body string, lines ...string) {
	t.Helper()
	body = unfold(body)
	for _, l := range lines {
		if !strings.Contains(body, "\r\n"+l+"\r\n") {
			t.Errorf("missing line %q", l)
		}
	}
}

func TestZmanimFeed(t *testing.T) {
	resp, body := getFeed(t, "/zmanim?zip=90210")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	h := resp.Header
	if got := h.Get("Content-Type"); got != "text/calendar; charset=utf-8" {
		t.Errorf("Content-Type %q", got)
	}
	if got := h.Get("Cache-Control"); got != "public, max-age=604800, s-maxage=604800" {
		t.Errorf("Cache-Control %q", got)
	}
	if h.Get("ETag") == "" || h.Get("Content-Disposition") != "" || h.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("headers %v", h)
	}
	requireLines(t, body,
		"PRODID:-//hebcal.com/NONSGML Hebcal Calendar v17.0.6//EN",
		"X-WR-CALNAME:Hebcal Zmanim Beverly Hills",
		"X-WR-TIMEZONE;VALUE=TEXT:America/Los_Angeles",
		// The first event: Chatzot HaLailah on the eve of yesterday, a
		// personal (no CLASS:PUBLIC), alarm-less timed event.
		"BEGIN:VEVENT\r\nDTSTAMP:20261110T210000Z\r\nCATEGORIES:chatzotNight\r\n"+
			"SUMMARY:Chatzot HaLailah\r\n"+
			"DTSTART;TZID=America/Los_Angeles:20261109T233800\r\n"+
			"DTEND;TZID=America/Los_Angeles:20261109T233800\r\n"+
			"UID:hebcal-20261109-438950d3-90210\r\n"+
			"TRANSP:TRANSPARENT\r\nX-MICROSOFT-CDO-BUSYSTATUS:FREE\r\n"+
			"DESCRIPTION:Sunset plus 6 halachic hours\r\n"+
			"LOCATION:Beverly Hills\r\nGEO:34.103131;-118.416253\r\nEND:VEVENT",
	)
	if strings.Contains(body, "CLASS:PUBLIC") || strings.Contains(body, "VALARM") {
		t.Error("zmanim events are private and have no alarm")
	}

	// A refresh the same day is answered from the ETag.
	resp2, _ := getFeed(t, "/zmanim?zip=90210", "If-None-Match", h.Get("ETag"))
	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("conditional status %d", resp2.StatusCode)
	}
}

func TestZmanimFeedKiddushLevanaMovedToNight(t *testing.T) {
	_, body := getFeed(t, "/zmanim/hebcal.ics?geonameid=281184")
	requireLines(t, body,
		"X-WR-CALDESC:Halachic times for Jerusalem\\, Israel",
		"CATEGORIES:tchilasZmanKidushLevana3Days\r\n"+
			"SUMMARY:Earliest Kiddush Levana (3 days)\r\n"+
			"DTSTART;TZID=Asia/Jerusalem:20261112T220700\r\n"+
			"DTEND;TZID=Asia/Jerusalem:20261112T220700\r\n"+
			"UID:hebcal-20261112-dd00c66b-281184",
	)
}

func TestZmanimFeedHebrew(t *testing.T) {
	_, body := getFeed(t, "/zmanim?geonameid=281184&lg=he")
	requireLines(t, body,
		"PRODID:-//hebcal.com/NONSGML Hebcal Calendar v17.0.6//HE",
		// hebcal-web's spelling, which differs from @hebcal/core's
		"SUMMARY:חֲצוֹת הַלַיְלָה",
		"SUMMARY:מִשֶּׁיַּכִּיר מַחמִיר",
		// in the locales package's catalog only
		"SUMMARY:Alot HaShachar (Baal Hatanya)",
	)
	_, body = getFeed(t, "/zmanim?geonameid=281184&lg=he-x-NoNikud")
	requireLines(t, body, "SUMMARY:עלות השחר", "SUMMARY:תחילת זמן קידוש לבנה (3 ימים)")
}

func TestZmanimFeedAllDay(t *testing.T) {
	_, body := getFeed(t, "/zmanim2?geonameid=281184&lg=he")
	requireLines(t, body,
		"SUMMARY:כ״ט חֶשְׁוָן\r\nDTSTART;VALUE=DATE:20261109\r\nDTEND;VALUE=DATE:20261110\r\n"+
			"UID:hebcal-zmanim2-20261109-281184\r\n"+
			"TRANSP:TRANSPARENT\r\nX-MICROSOFT-CDO-BUSYSTATUS:FREE\r\n"+
			"X-MICROSOFT-CDO-ALLDAYEVENT:TRUE\r\nCLASS:PUBLIC",
		"SUMMARY:א׳ כִּסְלֵו תשפ״ז",
	)
	if !strings.Contains(unfold(body), "DESCRIPTION:23:23 חֲצוֹת הַלַיְלָה\\n04:44 Alot HaShachar (Baal Hatanya)\\n04:48 עֲלוֹת הַשַּׁחַר\\n") {
		t.Error("24-hour Jerusalem times, untranslated where the catalog has nothing")
	}

	_, body = getFeed(t, "/zmanim2?geonameid=5128581")
	body = unfold(body)
	if !strings.Contains(body, "SUMMARY:29th of Cheshvan") || !strings.Contains(body, "DESCRIPTION:11:40pm Chatzot HaLailah\\n 5:08am Alot HaShachar (Baal Hatanya)\\n") {
		t.Error("12-hour times right-aligned on the colon")
	}
	if !strings.Contains(body, "Tzeit 72 min (Rabbeinu Tam)\\n\\nNew York\\, USA\r\nLOCATION:New York\r\nEND:VEVENT") {
		t.Error("description ends with the location's full name")
	}
}

func TestSunriseSunsetFeed(t *testing.T) {
	_, body := getFeed(t, "/sunrs?geonameid=5128581")
	requireLines(t, body,
		"X-WR-CALNAME:Sunrise and Sunset New York",
		"X-WR-CALDESC:Times for New York\\, USA",
		"CATEGORIES:sunrise\r\nSUMMARY:Sunrise\r\n"+
			"DTSTART;TZID=America/New_York:20261109T063600\r\n"+
			"DTEND;TZID=America/New_York:20261109T063600\r\n"+
			"UID:hebcal-20261109-b1ad7542-5128581",
	)
	unfolded := unfold(body)
	if n := strings.Count(unfolded, "BEGIN:VEVENT"); n != 2*367 {
		t.Errorf("%d events, want sunrise and sunset for 367 days", n)
	}
	if strings.Contains(unfolded, "CATEGORIES:dawn") {
		t.Error("sunrise and sunset only")
	}
}

func TestZmanimFeedLatLong(t *testing.T) {
	_, body := getFeed(t, "/zmanim?geo=pos&latitude=40.7&longitude=-74&tzid=America/New_York")
	requireLines(t, body,
		"UID:hebcal-20261109-438950d3-40-42-n-74-0-w-america-new_york",
		"LOCATION:40°42′N 74°0′W America/New_York",
		"GEO:40.7;-74",
	)
}

func TestZmanimFeedErrors(t *testing.T) {
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/zmanim", 400, "Location is required\n"},
		{"/sunrs?geonameid=99999999", 404, "Sorry, can't find geonameid: 99999999\n"},
		{"/zmanim?geo=pos&latitude=90&longitude=0&tzid=UTC", 400, "No zmanim available for 90°0′N 0°0′E UTC\n"},
	} {
		resp, body := getFeed(t, tc.path)
		if resp.StatusCode != tc.status || body != tc.body {
			t.Errorf("%s: %d %q", tc.path, resp.StatusCode, body)
		}
		if resp.Header.Get("Cache-Control") != "" || resp.Header.Get("ETag") != "" {
			t.Errorf("%s: an error is not cached", tc.path)
		}
	}
}

// cfg=json is still the JSON API, and at the pole a time that does not occur
// is null rather than a time in year 1.
func TestZmanimJSONPolarNull(t *testing.T) {
	resp, body := getFeed(t, "/zmanim?cfg=json&geo=pos&latitude=90&longitude=0&tzid=UTC&date=2026-11-12")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("%d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if strings.Contains(body, "0001-01-01") || !strings.Contains(body, `"minchaGedolaMGA":null`) {
		t.Errorf("body %s", body)
	}
}
