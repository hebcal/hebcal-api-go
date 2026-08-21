package pdf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings/readingstest"
	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// The six series with no Go schedule are fetched from readings-svc rather than
// refusing the calendar, so every one needs a query parameter.
func TestEveryUnsupportedSeriesHasAQueryParameter(t *testing.T) {
	msg := &pb.Download{
		ChofetzChaim: true, ShemiratHaLashon: true, ArukhHaShulchanYomi: true,
		SeferHaMitzvot: true, KitzurShulchanAruch: true, DirshuAmudYomi: true,
	}
	for _, s := range unsupportedSeries(msg) {
		if _, ok := fallbackSeries[s]; !ok {
			t.Errorf("series %q has no query parameter, so it could never be fetched", s)
		}
	}
	if len(fallbackSeries) != len(unsupportedSeries(msg)) {
		t.Errorf("fallbackSeries has %d entries but %d series are unsupported; "+
			"the two lists have drifted", len(fallbackSeries), len(unsupportedSeries(msg)))
	}
}

func TestFetchBuildsTheExpectedQuery(t *testing.T) {
	var got url.Values
	var gotPath string
	client := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))

	f := NewLearningFetcher(client)
	_, err := f.Fetch(context.Background(),
		[]string{"dirshuAmudYomi", "chofetzChaim"}, "es",
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/learning" {
		t.Errorf("path = %q, want /learning", gotPath)
	}
	for k, want := range map[string]string{
		"lg":    "es",
		"start": "2026-08-01", "end": "2026-08-31",
		"ayd": "on", "dcc": "on",
	} {
		if got.Get(k) != want {
			t.Errorf("query %s = %q, want %q", k, got.Get(k), want)
		}
	}
	// Series that were not asked for must not be enabled.
	if got.Get("dsm") != "" {
		t.Errorf("unrequested series was enabled: dsm=%q", got.Get("dsm"))
	}
}

func TestFetchParsesItems(t *testing.T) {
	client := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[
      {"title":"Yoma 49a","date":"2026-08-01","category":"dirshuAmudYomi",
       "hebrew":"יומא מ״ט א",
       "link":"https://www.sefaria.org/Yoma.49a?lang=bi&utm_source=hebcal.com&utm_medium=api"},
      {"title":"Something Else","date":"2026-08-01","category":"dafyomi"},
      {"title":"Candle lighting","date":"2026-08-01T18:56:00-07:00","category":"candles"}
    ]}`))
	}))

	f := NewLearningFetcher(client)
	evs, err := f.Fetch(context.Background(), []string{"dirshuAmudYomi"}, "",
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// Only the requested category, and not the timed row.
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(evs), evs)
	}
	e := evs[0]
	if e.Subject != "Yoma 49a" {
		t.Errorf("Subject = %q", e.Subject)
	}
	if !e.Learning {
		t.Error("fetched rows must be marked as learning, which drives their colour and order")
	}
	// @hebcal/rest-api's own tracking is stripped; the renderer adds its own.
	if strings.Contains(e.URL, "utm_") {
		t.Errorf("URL still carries hebcal-web's tracking: %s", e.URL)
	}
	if !strings.HasPrefix(e.URL, "https://www.sefaria.org/") {
		t.Errorf("URL host was rewritten: %s", e.URL)
	}
}

// A failed fetch must be an error, so the handler can return 501 rather than a
// calendar quietly missing the rows the user asked for.
func TestFetchReportsFailures(t *testing.T) {
	client := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	f := NewLearningFetcher(client)
	if _, err := f.Fetch(context.Background(), []string{"dirshuAmudYomi"}, "",
		time.Now(), time.Now()); err == nil {
		t.Error("expected an error for a 500 response")
	}
}

// The fetch covers whole months, because the calendar draws whole months: a
// day at the edge carrying nothing else would otherwise lose its row.
func TestLearningRangeCoversWholeMonths(t *testing.T) {
	mk := func(y int, m time.Month, d int) Event {
		return Event{Greg: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
	}
	start, end, ok := learningRange(&Params{},
		[]Event{mk(2026, time.August, 3), mk(2026, time.October, 12)})
	if !ok {
		t.Fatal("no range")
	}
	if start.Day() != 1 || start.Month() != time.August {
		t.Errorf("start = %v, want 1 August", start)
	}
	if end.Month() != time.October || end.Day() != 31 {
		t.Errorf("end = %v, want 31 October", end)
	}
}

// A calendar asking only for one of the six has nothing generated locally, so
// the range comes from the request instead. The fetched rows are then the
// whole calendar.
func TestLearningRangeFallsBackToTheRequest(t *testing.T) {
	p := &Params{}
	p.Opts.Year = 2026
	p.Opts.Month = time.August
	start, end, ok := learningRange(p, nil)
	if !ok {
		t.Fatal("no range for a year/month request")
	}
	if start.Month() != time.August || start.Day() != 1 || end.Day() != 31 {
		t.Errorf("range = %v..%v, want the whole of August 2026", start, end)
	}
}

// A Hebrew-year calendar (e.g. 5787h) asking only for one of the six unsupported
// series generates nothing locally, so the range must come from the Hebrew year:
// 1 Tishrei of the year through the day before 1 Tishrei of the next, widened to
// whole Gregorian months. Without this the fetch has nothing to anchor to and the
// request 500s ("cannot determine the calendar's date range").
func TestLearningRangeHebrewYear(t *testing.T) {
	p := &Params{}
	p.Opts.Year = 5787
	p.Opts.IsHebrewYear = true
	start, end, ok := learningRange(p, nil)
	if !ok {
		t.Fatal("no range for a Hebrew-year request")
	}
	// 5787 runs 12 Sep 2026 .. 1 Oct 2027, so whole months are Sep 2026 .. Oct 2027.
	if start.Year() != 2026 || start.Month() != time.September || start.Day() != 1 {
		t.Errorf("start = %v, want 1 September 2026", start)
	}
	if end.Year() != 2027 || end.Month() != time.October || end.Day() != 31 {
		t.Errorf("end = %v, want 31 October 2027", end)
	}
}

// Merged rows outside the drawn range are dropped rather than creating pages.
func TestMergeLearningDropsOutOfRange(t *testing.T) {
	mk := func(y int, m time.Month, d int) Event {
		return Event{Greg: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
	}
	events := []Event{mk(2026, time.August, 1), mk(2026, time.August, 31)}
	learning := []Event{mk(2026, time.August, 15), mk(2027, time.January, 1)}
	got := mergeLearning(events, learning)
	if len(got) != 3 {
		t.Errorf("got %d events, want 3 (the 2027 row should be dropped)", len(got))
	}
}
