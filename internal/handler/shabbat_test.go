package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestShabbatGating verifies the 501 scope gate: only cfg=json is implemented.
func TestShabbatGating(t *testing.T) {
	srv := testServerWithDB(t)
	for _, path := range []string{
		"/shabbat?geonameid=5128581",                   // cfg missing
		"/shabbat?cfg=r&geonameid=5128581&leyning=off", // unsupported cfg
		"/shabbat?cfg=i&geonameid=5128581",
	} {
		resp, body := get(t, srv, path)
		if resp.StatusCode != http.StatusNotImplemented {
			t.Errorf("%s: status = %d, want 501 (%s)", path, resp.StatusCode, body)
		}
	}
}

func TestShabbatOptions(t *testing.T) {
	srv := testServerWithDB(t)
	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/shabbat", nil)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", resp.StatusCode)
	}
	if m := resp.Header.Get("Access-Control-Allow-Methods"); m != "GET" {
		t.Errorf("Allow-Methods = %q, want GET", m)
	}
}

// Trailing whitespace on a query value is tolerated rather than rejected.
func TestShabbatLatLongTrailingWhitespace(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv,
		"/shabbat?cfg=json&latitude=40.71427&longitude=-74.00597+&tzid=America/New_York&dt=2026-06-12&leyning=off")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
}

func TestShabbatBasic(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&dt=2026-06-12&leyning=off")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var out struct {
		Title    string `json:"title"`
		Location struct {
			City string `json:"city"`
		} `json:"location"`
		Range struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"range"`
		Items []struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Memo     string `json:"memo"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	if !strings.HasPrefix(out.Title, "Hebcal New York") {
		t.Errorf("title = %q", out.Title)
	}
	if out.Location.City != "New York" {
		t.Errorf("city = %q", out.Location.City)
	}
	cats := map[string]bool{}
	var parsha string
	for _, it := range out.Items {
		cats[it.Category] = true
		if it.Category == "parashat" {
			parsha = it.Title
		}
	}
	for _, want := range []string{"candles", "parashat", "havdalah"} {
		if !cats[want] {
			t.Errorf("missing item category %q in %v", want, cats)
		}
	}
	if parsha != "Parashat Sh’lach" {
		t.Errorf("parsha title = %q, want Parashat Sh’lach", parsha)
	}
	// candle-lighting carries the upcoming parsha as its memo
	for _, it := range out.Items {
		if it.Category == "candles" && it.Memo != "Parashat Sh’lach" {
			t.Errorf("candle memo = %q, want Parashat Sh’lach", it.Memo)
		}
	}
}

// TestShabbatAshkenazi covers both spellings of the Ashkenazi
// transliteration option. hebcal-web's makeHebcalOptions() rewrites the very
// old a=on to lg=a when lg is absent, and lets an explicit lg win.
func TestShabbatAshkenazi(t *testing.T) {
	srv := testServerWithDB(t)
	const base = "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off"
	tests := []struct {
		query         string
		title         string
		wantTitleOrig bool
	}{
		{"", "Parashat Chayei Sara", false},
		{"&lg=a", "Parshas Chayei Sara", true},
		{"&a=on", "Parshas Chayei Sara", true},
		{"&a=on&lg=fr", "Parachah H̲ayé Sarah", true}, // explicit lg wins
		{"&a=off", "Parashat Chayei Sara", false},
	}
	for _, tc := range tests {
		resp, body := get(t, srv, base+tc.query)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status = %d: %s", tc.query, resp.StatusCode, body)
		}
		var out struct {
			Items []struct {
				Title     string `json:"title"`
				TitleOrig string `json:"title_orig"`
				Category  string `json:"category"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("%s: %v", tc.query, err)
		}
		var found bool
		for _, item := range out.Items {
			if item.Category != "parashat" {
				continue
			}
			found = true
			if item.Title != tc.title {
				t.Errorf("%q: parsha title = %q, want %q", tc.query, item.Title, tc.title)
			}
			// title_orig appears only when the rendered title differs from
			// the untranslated event description
			if got := item.TitleOrig != ""; got != tc.wantTitleOrig {
				t.Errorf("%q: title_orig = %q, want present=%v", tc.query, item.TitleOrig, tc.wantTitleOrig)
			}
			if tc.wantTitleOrig && item.TitleOrig != "Parashat Chayei Sara" {
				t.Errorf("%q: title_orig = %q", tc.query, item.TitleOrig)
			}
		}
		if !found {
			t.Errorf("%q: no parashat item", tc.query)
		}
	}
}

// TestShabbatLocaleValidation checks the accepted `lg` values against the set
// hebcal-web's makeHebcalOptions() takes, and the 400 it answers otherwise.
// Only /shabbat validates: hebcal-web's /converter and /zmanim render from
// the raw lg and fall back to English for anything they do not know.
func TestShabbatLocaleValidation(t *testing.T) {
	srv := testServerWithDB(t)
	const base = "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off"
	ok := []string{
		"", "s", "sh", "a", "ah", "h", "en", "he", "he-x-NoNikud", "he-x-nonikud", "HE",
		"ashkenazi", "ashkenazi_standard", "ashkenazi_litvish", "ashkenazi_poylish",
		"ashkenazi_romanian", "ashkenazi_komatz",
		"de", "es", "fi", "fr", "FR", "hu", "nl", "pl", "pt", "ro", "ru", "uk", "yi",
	}
	for _, lg := range ok {
		resp, body := get(t, srv, base+"&lg="+lg)
		if resp.StatusCode != 200 {
			t.Errorf("lg=%q: status = %d, want 200 (%s)", lg, resp.StatusCode, body)
		}
	}
	// "sephardic" is the internal name of the "s" locale, and hebcal-web
	// rejects it just like any other unknown value
	for _, lg := range []string{"it", "xx", "ru-RU", "sephardic"} {
		resp, body := get(t, srv, base+"&lg="+lg)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("lg=%q: status = %d, want 400 (%s)", lg, resp.StatusCode, body)
		}
		want := `{"error":"Locale '` + lg + `' not found"}`
		if body != want {
			t.Errorf("lg=%q: body = %s, want %s", lg, body, want)
		}
	}
}

// TestShabbatHavdalahMinutes verifies that a havdalah pinned to a fixed
// number of minutes after sunset (m=<min>) says so in its title, the way
// @hebcal/core's HavdalahEvent does. td=<deg> and the M=on default do not.
func TestShabbatHavdalahMinutes(t *testing.T) {
	srv := testServerWithDB(t)
	const base = "/shabbat?cfg=json&geonameid=5128581&dt=2026-11-07&leyning=off"
	tests := []struct {
		query, title, hebrew string
	}{
		{"&m=50", "Havdalah (50 min): ", "הבדלה (50 דקות)"},
		{"&m=72", "Havdalah (72 min): ", "הבדלה (72 דקות)"},
		{"&M=on", "Havdalah: ", "הבדלה"},
		{"&td=7.083", "Havdalah: ", "הבדלה"},
		{"", "Havdalah: ", "הבדלה"},
	}
	for _, tc := range tests {
		resp, body := get(t, srv, base+tc.query)
		if resp.StatusCode != 200 {
			t.Fatalf("%q: status = %d: %s", tc.query, resp.StatusCode, body)
		}
		var out struct {
			Items []struct {
				Title     string `json:"title"`
				TitleOrig string `json:"title_orig"`
				Hebrew    string `json:"hebrew"`
				Category  string `json:"category"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		var found bool
		for _, item := range out.Items {
			if item.Category != "havdalah" {
				continue
			}
			found = true
			if !strings.HasPrefix(item.Title, tc.title) {
				t.Errorf("%q: title = %q, want prefix %q", tc.query, item.Title, tc.title)
			}
			if item.Hebrew != tc.hebrew {
				t.Errorf("%q: hebrew = %q, want %q", tc.query, item.Hebrew, tc.hebrew)
			}
			// title_orig stays the untranslated event description
			if item.TitleOrig != "Havdalah" {
				t.Errorf("%q: title_orig = %q, want Havdalah", tc.query, item.TitleOrig)
			}
		}
		if !found {
			t.Errorf("%q: no havdalah item", tc.query)
		}
	}
}

// TestShabbatRoshChodeshLink guards the leap-year Adar URLs. The generic
// basename() rule strips a trailing Roman numeral, which would collapse
// "Rosh Chodesh Adar I" and "Rosh Chodesh Adar II" onto the plain-Adar slug —
// a different month, and a 404.
func TestShabbatRoshChodeshLink(t *testing.T) {
	srv := testServerWithDB(t)
	tests := map[string]string{
		// 5784 is a leap year: two Adars, two distinct slugs
		"2024-02-09": "https://hebcal.com/h/rosh-chodesh-adar-i-2024?us=js&um=api",
		"2024-03-10": "https://hebcal.com/h/rosh-chodesh-adar-ii-2024?us=js&um=api",
		// 5785 is not, so the same month is just "Adar"
		"2025-02-26": "https://hebcal.com/h/rosh-chodesh-adar-2025?us=js&um=api",
		"2024-12-26": "https://hebcal.com/h/rosh-chodesh-tevet-2024?us=js&um=api",
	}
	for dt, want := range tests {
		resp, body := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&leyning=off&dt="+dt)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status = %d: %s", dt, resp.StatusCode, body)
		}
		var out struct {
			Items []struct {
				Category string `json:"category"`
				Link     string `json:"link"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("%s: %v", dt, err)
		}
		var found bool
		for _, item := range out.Items {
			if item.Category != "roshchodesh" {
				continue
			}
			found = true
			if item.Link != want {
				t.Errorf("%s: link = %q, want %q", dt, item.Link, want)
			}
		}
		if !found {
			t.Errorf("%s: no roshchodesh item", dt)
		}
	}
}

// TestShabbatChanukahCandles verifies that the Chanukah candle-lighting, a
// timed event that is nonetheless the holiday itself, keeps the holiday's
// description and URL. Candle-lighting and havdalah, which merely link to a
// holiday, keep neither.
func TestShabbatChanukahCandles(t *testing.T) {
	srv := testServerWithDB(t)
	resp, body := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&dt=2024-12-26&leyning=off")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Items []struct {
			Title    string `json:"title"`
			Category string `json:"category"`
			Link     string `json:"link"`
			Memo     string `json:"memo"`
			Leyning  any    `json:"leyning"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var seen int
	for _, item := range out.Items {
		// English titles equal the description, so title_orig is absent
		switch {
		case strings.HasPrefix(item.Title, "Chanukah: "):
			seen++
			if item.Link != "https://hebcal.com/h/chanukah-2024?us=js&um=api" {
				t.Errorf("%s: link = %q", item.Title, item.Link)
			}
			if !strings.HasPrefix(item.Memo, "Hanukkah, the Jewish festival of rededication") {
				t.Errorf("%s: memo = %q", item.Title, item.Memo)
			}
			// a timed event never carries a reading, even when it is the
			// holiday: getLeyningForHoliday() rejects anything with a time
			if item.Leyning != nil {
				t.Errorf("%s: unexpected leyning %v", item.Title, item.Leyning)
			}
		case item.Category == "candles" || item.Category == "havdalah":
			if item.Link != "" {
				t.Errorf("%s: link = %q, want none", item.Title, item.Link)
			}
		}
	}
	if seen == 0 {
		t.Error("no Chanukah candle-lighting items")
	}
}

// TestShabbatMoladMemo pins the Molad announcement carried by Shabbat
// Mevarchim. Molad.render() curls the apostrophe in the month name, but only
// in the non-Hebrew sentence.
func TestShabbatMoladMemo(t *testing.T) {
	srv := testServerWithDB(t)
	tests := map[string]string{
		"":       "Molad Sh’vat: Thursday, 8:45am and 4 chalakim",
		"&lg=a":  "Molad Sh’vat: Thursday, 8:45am and 4 chalakim",
		"&lg=he": "מוֹלָד הָלְּבָנָה שְׁבָט יִהְיֶה בַּיּוֹם חֲמִישִׁי בשָׁבוּעַ, בְּשָׁעָה 8 בַּבֹּקֶר, ו-45 דַּקּוֹת ו-4 חֲלָקִים",
	}
	for query, want := range tests {
		resp, body := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&dt=2024-01-01&leyning=off"+query)
		if resp.StatusCode != 200 {
			t.Fatalf("%q: status = %d: %s", query, resp.StatusCode, body)
		}
		var out struct {
			Items []struct {
				Category string `json:"category"`
				Memo     string `json:"memo"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		var found bool
		for _, item := range out.Items {
			if item.Category != "mevarchim" {
				continue
			}
			found = true
			if item.Memo != want {
				t.Errorf("%q: molad memo = %q, want %q", query, item.Memo, want)
			}
		}
		if !found {
			t.Errorf("%q: no mevarchim item", query)
		}
	}
}

func TestShabbatNoDB(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&leyning=off")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}
