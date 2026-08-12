package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/repository/leyning"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// stubLeyning stands in for the hebcal-web /leyning endpoint, answering from
// testdata/leyning/readings.json, which holds one day of readings per
// "YYYY-MM-DD|<0 or 1 for Israel>" key, captured with &events=on.
type stubLeyning struct {
	*httptest.Server
	days     map[string][]json.RawMessage
	requests atomic.Int32
	status   int // when non-zero, every request fails with this status
}

func newStubLeyning(t *testing.T) *stubLeyning {
	t.Helper()
	raw, err := os.ReadFile("testdata/leyning/readings.json")
	if err != nil {
		t.Fatalf("readings.json: %v", err)
	}
	stub := &stubLeyning{}
	if err := json.Unmarshal(raw, &stub.days); err != nil {
		t.Fatalf("readings.json: %v", err)
	}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.requests.Add(1)
		if stub.status != 0 {
			http.Error(w, "nope", stub.status)
			return
		}
		q := r.URL.Query()
		il := "0"
		if q.Get("i") == "on" {
			il = "1"
		}
		if q.Get("events") != "on" {
			t.Errorf("/leyning called without events=on: %s", r.URL.RawQuery)
		}
		start, err1 := time.Parse("2006-01-02", q.Get("start"))
		end, err2 := time.Parse("2006-01-02", q.Get("end"))
		if err1 != nil || err2 != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		items := []json.RawMessage{}
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			date := d.Format("2006-01-02")
			day, ok := stub.days[date+"|"+il]
			if !ok {
				t.Errorf("no captured readings for %s|%s (add it to readings.json)", date, il)
			}
			items = append(items, day...)
		}
		w.Header().Set("Content-Type", httpx.ContentTypeJSON)
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	t.Cleanup(stub.Close)
	return stub
}

// testServerWithLeyning is testServerWithDB plus a stub /leyning upstream.
func testServerWithLeyning(t *testing.T) (*httptest.Server, *stubLeyning) {
	t.Helper()
	app, _ := testServer(t)
	db, err := geodb.New("testdata/zips.sqlite3", "testdata/geonames.sqlite3")
	if err != nil {
		t.Fatalf("geodb.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	app.DB = db
	stub := newStubLeyning(t)
	app.Leyning = leyning.New(stub.URL)
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	return srv, stub
}

// itemKey identifies an item across the two implementations.
func itemKey(item map[string]any) string {
	title, _ := item["title"].(string)
	if orig, ok := item["title_orig"].(string); ok {
		title = orig
	}
	date, _ := item["date"].(string)
	cat, _ := item["category"].(string)
	return fmt.Sprintf("%s|%s|%s", date, title, cat)
}

// leyningByItem indexes the "leyning" JSON of every item that has one, with
// whitespace removed but key order preserved: the classic API is specific
// about that order, so comparing the bytes checks it.
func leyningByItem(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var parsed struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	out := make(map[string]string)
	for _, raw := range parsed.Items {
		ley, ok := raw["leyning"]
		if !ok {
			continue
		}
		item := make(map[string]any, len(raw))
		for k, v := range raw {
			var val any
			json.Unmarshal(v, &val)
			item[k] = val
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, ley); err != nil {
			t.Fatalf("compact: %v", err)
		}
		out[itemKey(item)] = compact.String()
	}
	return out
}

// leyningGoldens are hebcal-web /shabbat responses captured from a live
// server. Only the per-item "leyning" objects are compared: the rest of the
// item (titles, times, links) is the older, separately-tested code path.
var leyningGoldens = []struct {
	name string
	path string
}{
	// ordinary Shabbat: 7 aliyot, torah, haftarah, maftir, triennial
	{"ny-reeh", "/shabbat?cfg=json&geonameid=5128581&dt=2026-08-08"},
	// Shabbat Shekalim: a special-Shabbat reading of its own (maftir +
	// two Haftarot) alongside a parsha whose maftir and Haftarah carry
	// "| Shabbat Shekalim" reasons
	{"ny-shekalim", "/shabbat?cfg=json&geonameid=5128581&dt=2026-02-14"},
	// Shabbat Rosh Chodesh Chanukah: replaces the 7th aliyah as well
	{"ny-chanukah-rc", "/shabbat?cfg=json&geonameid=5128581&dt=2025-12-20"},
	// Rosh Hashana falling on Shabbat
	{"la-rosh-hashana", "/shabbat?cfg=json&gd=10&geonameid=5368361&gm=9&gy=2026"},
	// Shabbat Shuva: a special Shabbat whose reading is a Haftarah only,
	// and a different Haftarah from the one the parsha reading uses
	{"la-shabbat-shuva", "/shabbat?cfg=json&gd=15&geonameid=5368361&gm=9&gy=2026"},
	// Yom Kippur midweek: the Mincha reading must not leak into the item
	{"la-yom-kippur", "/shabbat?cfg=json&gd=1&geonameid=5368361&gm=10&gy=2025"},
	// Tzom Tammuz: the one event @hebcal/core spells with a double "m",
	// which normMonth() must leave alone or the reading stops matching
	{"ny-tzom-tammuz", "/shabbat?cfg=json&geonameid=5128581&dt=2025-07-10"},
	// the same Sukkot week read two ways: the Diaspora schedule, then the
	// Israel schedule (i=on), which is a day out of step through Chol
	// ha-Moed
	{"ny-sukkot", "/shabbat?cfg=json&geonameid=5128581&dt=2025-10-07"},
	{"il-sukkot", "/shabbat?cfg=json&geonameid=281184&dt=2025-10-07"},
	// Purim in Israel: the megillah is folded into the torah summary
	{"il-purim", "/shabbat?cfg=json&geonameid=281184&dt=2026-03-03"},
	// the readings stay English whatever lg asks for, and are still matched
	// to their events by the untranslated event description
	{"ny-shekalim-he", "/shabbat?cfg=json&geonameid=5128581&dt=2026-02-14&lg=he"},
	{"la-reeh-ashkenazi", "/shabbat?cfg=json&geonameid=5368361&dt=2026-08-08&lg=a"},
}

func TestShabbatLeyningMatchesHebcalWeb(t *testing.T) {
	srv, _ := testServerWithLeyning(t)
	for _, tc := range leyningGoldens {
		t.Run(tc.name, func(t *testing.T) {
			golden, err := os.ReadFile("testdata/leyning/" + tc.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			want := leyningByItem(t, golden)
			if len(want) == 0 {
				t.Fatalf("golden %s has no leyning to compare", tc.name)
			}
			resp, body := get(t, srv, tc.path)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d: %s", resp.StatusCode, body)
			}
			got := leyningByItem(t, []byte(body))
			for key, wantLey := range want {
				gotLey, ok := got[key]
				if !ok {
					t.Errorf("%s: no leyning for %s", tc.name, key)
					continue
				}
				if gotLey != wantLey {
					t.Errorf("%s: leyning for %s\n got: %s\nwant: %s", tc.name, key, gotLey, wantLey)
				}
			}
			for key := range got {
				if _, ok := want[key]; !ok {
					t.Errorf("%s: unexpected leyning for %s: %s", tc.name, key, got[key])
				}
			}
		})
	}
}

// TestShabbatLeyningOff verifies that leyning={off,0} omits every reading and
// never calls the upstream service.
func TestShabbatLeyningOff(t *testing.T) {
	for _, off := range []string{"off", "0"} {
		srv, stub := testServerWithLeyning(t)
		resp, body := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&dt=2026-08-08&leyning="+off)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d: %s", resp.StatusCode, body)
		}
		if strings.Contains(body, `"leyning"`) {
			t.Errorf("leyning=%s: body has a leyning object: %s", off, body)
		}
		if n := stub.requests.Load(); n != 0 {
			t.Errorf("leyning=%s: made %d upstream requests, want 0", off, n)
		}
	}
}

// TestShabbatLeyningUnavailable verifies that an upstream failure surfaces as
// 503 rather than a response that silently drops the readings.
func TestShabbatLeyningUnavailable(t *testing.T) {
	srv, stub := testServerWithLeyning(t)
	stub.status = http.StatusInternalServerError
	resp, _ := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&dt=2026-08-08")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	// the caching headers set for the body we did not send must be gone
	for _, h := range []string{"Cache-Control", "ETag", "Expires"} {
		if v := resp.Header.Get(h); v != "" {
			t.Errorf("503 response carries %s: %q", h, v)
		}
	}
}

// TestShabbatLeyningCache verifies that a repeat request for the same week
// and the same coarse location is served entirely from the LRU cache, and
// that Israel and the Diaspora are cached separately.
func TestShabbatLeyningCache(t *testing.T) {
	srv, stub := testServerWithLeyning(t)
	// New York and Los Angeles share a Shabbat week, so the second request
	// must not reach the upstream service.
	get(t, srv, "/shabbat?cfg=json&geonameid=5128581&dt=2025-10-07")
	after1 := stub.requests.Load()
	if after1 != 1 {
		t.Fatalf("first request made %d upstream calls, want 1", after1)
	}
	get(t, srv, "/shabbat?cfg=json&geonameid=5368361&dt=2025-10-07")
	if n := stub.requests.Load(); n != after1 {
		t.Errorf("second Diaspora request made %d more upstream calls, want 0", n-after1)
	}
	// Jerusalem reads a different schedule, so it is a separate cache entry.
	get(t, srv, "/shabbat?cfg=json&geonameid=281184&dt=2025-10-07")
	if n := stub.requests.Load(); n != after1+1 {
		t.Errorf("Israel request made %d upstream calls, want 1", n-after1)
	}
}
