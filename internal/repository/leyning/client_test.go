package leyning

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// TestLeyningAliyahFormat covers the citation formatting ported from
// @hebcal/leyning: the full form used for aliyot and the short form used
// inside a Haftarah summary.
func TestLeyningAliyahFormat(t *testing.T) {
	a := Aliyah{K: "Numbers", B: "28:9", E: "28:15"}
	if got, want := a.WithBook(), "Numbers 28:9-28:15"; got != want {
		t.Errorf("withBook() = %q, want %q", got, want)
	}
	if got, want := a.short(true), "Numbers 28:9-15"; got != want {
		t.Errorf("short(true) = %q, want %q", got, want)
	}
	if got, want := a.short(false), "28:9-15"; got != want {
		t.Errorf("short(false) = %q, want %q", got, want)
	}
	across := Aliyah{K: "Isaiah", B: "54:11", E: "55:5"}
	if got, want := across.short(true), "Isaiah 54:11-55:5"; got != want {
		t.Errorf("short across chapters = %q, want %q", got, want)
	}
	single := Aliyah{K: "Joel", B: "2:15", E: "2:15"}
	if got, want := single.short(true), "Joel 2:15"; got != want {
		t.Errorf("short single verse = %q, want %q", got, want)
	}
}

// TestHaftaraPartsSummary covers both shapes /leyning uses for a Haftarah:
// a bare object for one passage, an array for several.
func TestHaftaraPartsSummary(t *testing.T) {
	var one HaftaraParts
	if err := json.Unmarshal([]byte(`{"k":"Isaiah","b":"54:11","e":"55:5"}`), &one); err != nil {
		t.Fatal(err)
	}
	if got, want := one.Summary(), "Isaiah 54:11-55:5"; got != want {
		t.Errorf("single summary = %q, want %q", got, want)
	}
	var many HaftaraParts
	err := json.Unmarshal([]byte(`[{"k":"Hosea","b":"14:2","e":"14:10"},`+
		`{"k":"Micah","b":"7:18","e":"7:20"},{"k":"Joel","b":"2:15","e":"2:27"}]`), &many)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hosea 14:2-10; Micah 7:18-20; Joel 2:15-27"
	if got := many.Summary(); got != want {
		t.Errorf("multi summary = %q, want %q", got, want)
	}
}

// TestLeyningClientTimeout verifies that a hung upstream is bounded by the
// per-request timeout instead of hanging the /shabbat response.
func TestLeyningClientTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer slow.Close()
	c := New(slow.URL)
	c.client.Timeout = 50 * time.Millisecond
	start := model.GregDate{Year: 2026, Month: time.August, Day: 7}
	end := model.GregDate{Year: 2026, Month: time.August, Day: 8}
	if _, err := c.Readings(t.Context(), start, end, false); err == nil {
		t.Error("expected a timeout error")
	}
}
