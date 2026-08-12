package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type shabbatTestItem struct {
	Title     string          `json:"title"`
	TitleOrig string          `json:"title_orig"`
	Date      string          `json:"date"`
	Category  string          `json:"category"`
	Hebrew    string          `json:"hebrew"`
	Link      string          `json:"link"`
	Memo      string          `json:"memo"`
	Molad     json.RawMessage `json:"molad"`
}

// getItems fetches /shabbat and decodes the items array.
func getItems(t *testing.T, srv *httptest.Server, path string) []shabbatTestItem {
	t.Helper()
	resp, body := get(t, srv, path)
	if resp.StatusCode != 200 {
		t.Fatalf("%s: status = %d: %s", path, resp.StatusCode, body)
	}
	var out struct {
		Items []shabbatTestItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out.Items
}

// candleTitles returns the candle-lighting and havdalah titles, in order.
func candleTitles(items []shabbatTestItem) []string {
	var out []string
	for _, item := range items {
		if item.Category == "candles" || item.Category == "havdalah" {
			out = append(out, item.Title)
		}
	}
	return out
}

// TestShabbatHavdalahPrecedence walks the b/m/M/td combinations through the
// precedence rules in makeHebcalOptions() (hebcal-web src/calendar.js), whose
// results were captured from a live hebcal-web.
func TestShabbatHavdalahPrecedence(t *testing.T) {
	srv := testServerWithDB(t)
	const ny = "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off"
	const il = "/shabbat?cfg=json&geonameid=281184&dt=2026-11-07&leyning=off"
	tests := []struct {
		name, path string
		want       []string
	}{
		{"default is tzeit at 8.5 degrees", ny, []string{"Candle lighting: 4:28pm", "Havdalah: 5:28pm"}},
		{"M=on is the same 8.5 degrees", ny + "&M=on", []string{"Candle lighting: 4:28pm", "Havdalah: 5:28pm"}},
		// M=off with no other preference still falls back on Zmanim.tzeit(),
		// whose own default angle is 8.5
		{"M=off alone changes nothing", ny + "&M=off", []string{"Candle lighting: 4:28pm", "Havdalah: 5:28pm"}},
		{"m=<min> is a fixed offset", ny + "&m=50", []string{"Candle lighting: 4:28pm", "Havdalah (50 min): 5:35pm"}},
		{"M=on beats m", ny + "&M=on&m=50", []string{"Candle lighting: 4:28pm", "Havdalah: 5:28pm"}},
		// a zero m asks for no havdalah at all, which is CalOptions.SuppressHavdalah:
		// a zero HavdalahMins would otherwise read as "use the default tzeit"
		{"m=0 drops havdalah", ny + "&m=0", []string{"Candle lighting: 4:28pm"}},
		{"M=off&m=0 drops havdalah", ny + "&M=off&m=0", []string{"Candle lighting: 4:28pm"}},
		{"m=on is the lowercase M=on", ny + "&m=on", []string{"Candle lighting: 4:28pm", "Havdalah: 5:28pm"}},
		{"td=<deg> sets the angle", ny + "&td=7.083", []string{"Candle lighting: 4:28pm", "Havdalah: 5:20pm"}},
		{"td=0 is ignored", ny + "&td=0", []string{"Candle lighting: 4:28pm", "Havdalah: 5:28pm"}},
		{"td beats m", ny + "&td=8.5&m=50", []string{"Candle lighting: 4:28pm", "Havdalah: 5:28pm"}},
		{"M=off picks m over td", ny + "&td=8.5&m=50&M=off",
			[]string{"Candle lighting: 4:28pm", "Havdalah (50 min): 5:35pm"}},
		// b=0 lights exactly at sunset
		{"b=0 lights at sunset", ny + "&b=0", []string{"Candle lighting: 4:46pm", "Havdalah: 5:28pm"}},
		{"b=<min> before sunset", ny + "&b=40", []string{"Candle lighting: 4:06pm", "Havdalah: 5:28pm"}},
		// in Israel an absent b, or the b=18 the web form submits by default,
		// yields the local custom (40 minutes in Jerusalem)
		{"Israel uses the local custom", il, []string{"Candle lighting: 16:05", "Havdalah: 17:23"}},
		{"Israel overrides a default b=18", il + "&b=18", []string{"Candle lighting: 16:05", "Havdalah: 17:23"}},
		{"Israel keeps a deliberate b", il + "&b=45", []string{"Candle lighting: 16:00", "Havdalah: 17:23"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := candleTitles(getItems(t, srv, tc.path))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestShabbatHour12 covers the h12 override of the 12- vs 24-hour clock,
// which otherwise follows the location's country.
func TestShabbatHour12(t *testing.T) {
	srv := testServerWithDB(t)
	const ny = "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off"
	const il = "/shabbat?cfg=json&geonameid=281184&dt=2026-11-07&leyning=off"
	tests := []struct {
		path string
		want []string
	}{
		{ny, []string{"Candle lighting: 4:28pm", "Havdalah: 5:28pm"}},
		{ny + "&h12=0", []string{"Candle lighting: 16:28", "Havdalah: 17:28"}},
		{ny + "&h12=off", []string{"Candle lighting: 16:28", "Havdalah: 17:28"}},
		{il, []string{"Candle lighting: 16:05", "Havdalah: 17:23"}},
		{il + "&h12=1", []string{"Candle lighting: 4:05pm", "Havdalah: 5:23pm"}},
	}
	for _, tc := range tests {
		got := candleTitles(getItems(t, srv, tc.path))
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: got %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestShabbatMolad covers the molad=on announcement, including the exact
// moment of the conjunction.
func TestShabbatMolad(t *testing.T) {
	srv := testServerWithDB(t)
	const base = "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off"

	for _, item := range getItems(t, srv, base) {
		if item.Category == "molad" {
			t.Fatalf("molad announced without molad=on: %+v", item)
		}
	}

	for _, on := range []string{"on", "1"} {
		var found bool
		for _, item := range getItems(t, srv, base+"&molad="+on) {
			if item.Category != "molad" {
				continue
			}
			found = true
			if item.Title != "Molad Kislev: Monday, 10:27pm and 3 chalakim" {
				t.Errorf("molad=%s: title = %q", on, item.Title)
			}
			if item.TitleOrig != "Molad Kislev 5787" {
				t.Errorf("molad=%s: title_orig = %q", on, item.TitleOrig)
			}
			// eventToClassicApiObject deletes `hebrew` on a molad item
			if item.Hebrew != "" {
				t.Errorf("molad=%s: hebrew = %q, want none", on, item.Hebrew)
			}
			want := `{"hy":5787,"hm":"Kislev","dow":1,"hour":22,"minutes":27,` +
				`"chalakim":3,"instant":"2026-11-09T20:06:13.504Z"}`
			if string(item.Molad) != want {
				t.Errorf("molad=%s: molad = %s\n              want %s", on, item.Molad, want)
			}
		}
		if !found {
			t.Errorf("molad=%s: no molad item", on)
		}
	}
}

// TestShabbatYomTovOnly covers yto=on. A week with no Yom Tov in it answers
// 200 with no items, rather than the 400 hebcal-web gives: the filter having
// nothing to keep is an empty answer, not a bad request.
func TestShabbatYomTovOnly(t *testing.T) {
	srv := testServerWithDB(t)
	items := getItems(t, srv, "/shabbat?cfg=json&geonameid=5128581&dt=2026-04-01&leyning=off&yto=on")
	var titles []string
	for _, item := range items {
		titles = append(titles, item.Title)
		if item.Category != "holiday" {
			t.Errorf("yto=on kept a %q item: %q", item.Category, item.Title)
		}
	}
	if strings.Join(titles, "|") != "Pesach I|Pesach II" {
		t.Errorf("yto=on titles = %v, want Pesach I, Pesach II", titles)
	}
	resp, body := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off&yto=on")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a week with no Yom Tov: status = %d, want 200 (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"items":[]`) {
		t.Errorf("a week with no Yom Tov: want an empty items array, got %s", body)
	}
	// the range describes the events, so it goes away with them
	if strings.Contains(body, `"range"`) {
		t.Errorf("a week with no Yom Tov: unexpected range in %s", body)
	}
}

// TestShabbatIsraelFlag covers i=on, which puts a Diaspora location on the
// Israel schedule.
func TestShabbatIsraelFlag(t *testing.T) {
	srv := testServerWithDB(t)
	const base = "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off"
	var links []string
	for _, item := range getItems(t, srv, base+"&i=on") {
		if item.Link != "" {
			links = append(links, item.Link)
		}
	}
	for _, want := range []string{
		"https://hebcal.com/s/5787i/5?us=js&um=api",
		"https://hebcal.com/h/sigd-2026?i=on&us=js&um=api",
	} {
		var found bool
		for _, link := range links {
			if link == want {
				found = true
			}
		}
		if !found {
			t.Errorf("i=on: missing %q in %v", want, links)
		}
	}
	for _, item := range getItems(t, srv, base) {
		if strings.Contains(item.Link, "i=on") || strings.Contains(item.Link, "5787i") {
			t.Errorf("without i=on, link = %q", item.Link)
		}
	}
}

// TestShabbatUseElevation covers ue=on, which folds the location's elevation
// into sunrise and sunset.
func TestShabbatUseElevation(t *testing.T) {
	srv := testServerWithDB(t)
	const base = "/shabbat?cfg=json&geonameid=281184&dt=2026-11-07&leyning=off"
	flat := candleTitles(getItems(t, srv, base))
	raised := candleTitles(getItems(t, srv, base+"&ue=on"))
	if strings.Join(flat, "|") == strings.Join(raised, "|") {
		t.Errorf("ue=on changed nothing in Jerusalem (786m): %v", flat)
	}
	off := candleTitles(getItems(t, srv, base+"&ue=off"))
	if strings.Join(flat, "|") != strings.Join(off, "|") {
		t.Errorf("ue=off differs from the default: %v vs %v", off, flat)
	}
}

// TestShabbatJSONP covers the callback parameter. hebcal-web ignores a
// callback that is too long or is not a plain dotted identifier, rather than
// sanitizing it.
func TestShabbatJSONP(t *testing.T) {
	srv := testServerWithDB(t)
	const base = "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off"
	for _, cb := range []string{"myCallback", "_$x", "window.hebcal.cb"} {
		resp, body := get(t, srv, base+"&callback="+cb)
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
			t.Errorf("callback=%s: Content-Type = %q", cb, ct)
		}
		if !strings.HasPrefix(body, cb+"({") || !strings.HasSuffix(body, "})\n") {
			t.Errorf("callback=%s: body = %.60s...%s", cb, body, body[len(body)-10:])
		}
	}
	for _, cb := range []string{"1bad", "has%20space", "semi%3Bcolon", strings.Repeat("a", 129)} {
		resp, body := get(t, srv, base+"&callback="+cb)
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("callback=%q: Content-Type = %q, want JSON", cb, ct)
		}
		if !strings.HasPrefix(body, "{") {
			t.Errorf("callback=%q: body = %.40s", cb, body)
		}
	}
}
