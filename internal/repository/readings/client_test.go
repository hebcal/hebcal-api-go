package readings_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings/readingstest"
)

// A client with no socket configured must fail rather than dial "": the two
// callers turn that error into the 501 and 503 that say the sidecar is missing.
func TestNoSocketIsAnError(t *testing.T) {
	c := readings.New("")
	_, err := c.Leyning(t.Context(), model.GregDate{Year: 2026, Month: 8, Day: 8},
		model.GregDate{Year: 2026, Month: 8, Day: 8}, false)
	if !errors.Is(err, readings.ErrNoSocket) {
		t.Errorf("Leyning error = %v, want ErrNoSocket", err)
	}
	if err := c.DialSocket(t.Context()); !errors.Is(err, readings.ErrNoSocket) {
		t.Errorf("DialSocket error = %v, want ErrNoSocket", err)
	}
	if _, err := c.Learning(t.Context(), []string{"dcc"}, "", time.Now(),
		time.Now()); !errors.Is(err, readings.ErrNoSocket) {
		t.Errorf("Learning error = %v, want ErrNoSocket", err)
	}
}

// Desc is what both callers match an event on: the untranslated description,
// which the classic API reports as title_orig only when it differs from title.
func TestItemDesc(t *testing.T) {
	if got := (&readings.Item{Title: "Parashat Re'eh"}).Desc(); got != "Parashat Re'eh" {
		t.Errorf("Desc without title_orig = %q", got)
	}
	it := &readings.Item{Title: "Hilchos Rechilus 3.2-3.4", TitleOrig: "HilchosRechilus 3.2-3.4"}
	if got := it.Desc(); got != "HilchosRechilus 3.2-3.4" {
		t.Errorf("Desc with title_orig = %q", got)
	}
}

// Every date in the requested span is cached, including the ones with no
// reading, so a repeat week never reaches the sidecar again; Israel and the
// Diaspora are separate entries.
func TestLeyningCachesEveryDayInTheSpan(t *testing.T) {
	var calls atomic.Int32
	c := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"items": []any{
			// only the Saturday has a reading; the other five days are still
			// cached, as empty
			map[string]any{"title": "Parashat Re'eh", "date": "2026-08-08",
				"category": "parashat", "leyning": map[string]string{"torah": "x"}},
		}})
	}))
	start := model.GregDate{Year: 2026, Month: 8, Day: 3}
	end := model.GregDate{Year: 2026, Month: 8, Day: 8}

	got, err := c.Leyning(t.Context(), start, end, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got["2026-08-08"]); n != 1 {
		t.Fatalf("Saturday has %d items, want 1", n)
	}
	if _, err := c.Leyning(t.Context(), start, end, false); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("made %d requests for the same week, want 1", n)
	}
	if _, err := c.Leyning(t.Context(), start, end, true); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("Israel made %d total requests, want 2 (a separate cache entry)", n)
	}
}

// The "ah"/"sh" transliteration spellings are not @hebcal/locales names --
// readings-svc 400s on them -- so Learning sends their base locale ("a"/"s")
// instead; every other lg passes through unchanged.
func TestLearningMapsTransliterationLocales(t *testing.T) {
	tests := []struct{ lg, want string }{
		{"ah", "a"},
		{"sh", "s"},
		{"he", "he"},
		{"", ""},
	}
	for _, tt := range tests {
		var got string
		c := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Query().Get("lg")
			json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		}))
		day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		if _, err := c.Learning(t.Context(), []string{"dsm"}, tt.lg, day, day); err != nil {
			t.Fatalf("lg=%q: %v", tt.lg, err)
		}
		if got != tt.want {
			t.Errorf("lg=%q sent lg=%q to sidecar, want %q", tt.lg, got, tt.want)
		}
	}
}

// A hung sidecar must not hang the /shabbat response. Leyning bounds every
// request at 3 seconds of its own, and honours an earlier caller deadline,
// which is what this checks.
func TestLeyningHonoursTheCallerDeadline(t *testing.T) {
	c := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	day := model.GregDate{Year: 2026, Month: time.August, Day: 8}
	if _, err := c.Leyning(ctx, day, day, false); err == nil {
		t.Error("expected a timeout error")
	}
}
