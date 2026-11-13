package shabbat

import (
	"strings"
	"testing"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/molad"
	"github.com/hebcal/hebcal-go/zmanim"

	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// TestMoladInstant checks the conjunction timestamp against captured reference
// values, including a millisecond ending in a zero: trailing zeros are
// trimmed, so ".170" prints as ".17".
func TestMoladInstant(t *testing.T) {
	tests := []struct {
		year  int
		month hdate.HMonth
		want  string
	}{
		{5787, hdate.Kislev, "2026-11-09T20:06:13.504Z"},
		{5784, hdate.Adar1, "2024-02-09T19:08:20.17Z"},
		{5784, hdate.Sivan, "2024-06-06T22:04:33.504Z"},
	}
	for _, tc := range tests {
		if got := moladInstant(molad.New(tc.year, tc.month)); got != tc.want {
			t.Errorf("molad %d %v: instant = %q, want %q", tc.year, tc.month, got, tc.want)
		}
	}
}

// TestNormMonth pins the "Tammuz" spellings: the month is written as "Tamuz"
// but "Tzom Tammuz" is kept for the 17th-of-Tammuz fast, and that description
// is what title_orig, the MEMO key, the event URL and the /leyning lookup are
// all built from.
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

// Every event with a clock time is treated as timed, whatever its concrete
// type: Latest Kiddush Levana is not a hebcal.TimedEvent, and a type
// assertion on that would report it as an all-day event with the time left in
// its title.
func TestKiddushLevanaItemIsTimed(t *testing.T) {
	events, err := hebcal.HebrewCalendar(&hebcal.Options{
		Year: 2026, Month: 10, CandleLighting: true, KiddushLevanaMaharil: true,
		Location: zmanim.LookupCity("New York"),
	})
	if err != nil {
		t.Fatal(err)
	}
	loc := &geodb.Location{CC: "US"}
	found := false
	for _, ev := range events {
		if !ev.Flags().Has(event.KiddushLevana) {
			continue
		}
		found = true
		if got := renderBriefLike(ev, "en"); got != "Latest Kiddush Levana" {
			t.Errorf("brief title = %q", got)
		}
		obj := item(ev, loc, false, "en", "", true, event.ClockAuto, nil)
		got := map[string]interface{}{}
		for _, kv := range obj {
			got[kv.Key] = kv.Val
		}
		if got["title"] != "Latest Kiddush Levana" {
			t.Errorf("title = %v", got["title"])
		}
		if got["date"] != "2026-10-25T21:44:00-04:00" {
			t.Errorf("date = %v, want the event's time", got["date"])
		}
		for _, key := range []string{"hdate", "heDateParts", "leyning"} {
			if _, ok := got[key]; ok {
				t.Errorf("a timed item has no %q", key)
			}
		}
	}
	if !found {
		t.Fatal("no Latest Kiddush Levana event in October 2026")
	}
}

// A timed event's label is hebcal-go's brief rendering, which keeps the
// "(50 min)" of a havdalah a fixed number of minutes after sunset.
func TestTimedLabels(t *testing.T) {
	events, err := hebcal.HebrewCalendar(&hebcal.Options{
		Year: 2026, Month: 12, CandleLighting: true, HavdalahMins: 50,
		Location: zmanim.LookupCity("Chicago"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"Candle lighting": false, "Havdalah (50 min)": false, "Chanukah: 3 Candles": false, "Fast begins": false}
	for _, ev := range events {
		if _, ok := ev.(event.Timed); !ok {
			continue
		}
		label := renderBriefLike(ev, "en")
		if _, ok := want[label]; ok {
			want[label] = true
		}
		// the label is the full rendering without its time
		full := ev.Render("en")
		if i := strings.LastIndex(full, ": "); i < 0 || full[:i] != label {
			t.Errorf("label %q is not %q without its time", label, full)
		}
		kind, _ := hebcal.TimedKindOf(ev)
		if got, want := isCandleOrHavdalah(ev), kind == hebcal.CandleLighting || kind == hebcal.Havdalah; got != want {
			t.Errorf("%s: isCandleOrHavdalah = %v", label, got)
		}
	}
	for label, seen := range want {
		if !seen {
			t.Errorf("no timed event labelled %q", label)
		}
	}
}

// The two chametz deadlines of Erev Pesach are zmanim, each with its own
// subcategory.
func TestChametzCategories(t *testing.T) {
	events, err := hebcal.HebrewCalendar(&hebcal.Options{
		Year: 2026, Month: 4, CandleLighting: true,
		Location: zmanim.LookupCity("Chicago"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Finish eating chametz": "achilasChametz",
		"Biur Chametz":          "biurChametz",
	}
	seen := 0
	for _, ev := range events {
		sub, ok := want[ev.Desc()]
		if !ok {
			continue
		}
		seen++
		if cat, subcat := categoriesOf(ev, descOf(ev), ev.Flags()); cat != "zmanim" || subcat != sub {
			t.Errorf("%s: category %q/%q, want zmanim/%s", ev.Desc(), cat, subcat, sub)
		}
	}
	if seen != 2 {
		t.Errorf("found %d chametz events, want 2", seen)
	}
}
