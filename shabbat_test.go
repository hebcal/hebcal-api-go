package main

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

// TestNormMonth pins the "Tammuz" spellings: @hebcal/core writes the month
// as "Tamuz" but keeps "Tzom Tammuz" for the 17th-of-Tammuz fast, and that
// description is what title_orig, the MEMO key, the event URL and the
// /leyning lookup are all built from.
func TestNormMonth(t *testing.T) {
	cases := map[string]string{
		"Rosh Chodesh Tammuz":              "Rosh Chodesh Tamuz",
		"Shabbat Mevarchim Chodesh Tammuz": "Shabbat Mevarchim Chodesh Tamuz",
		"Tzom Tammuz":                      "Tzom Tammuz",
		"Rosh Chodesh Av":                  "Rosh Chodesh Av",
	}
	for in, want := range cases {
		if got := normMonth(in); got != want {
			t.Errorf("normMonth(%q) = %q, want %q", in, got, want)
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

func TestShabbatNoDB(t *testing.T) {
	_, srv := testServer(t)
	resp, _ := get(t, srv, "/shabbat?cfg=json&geonameid=5128581&leyning=off")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}
