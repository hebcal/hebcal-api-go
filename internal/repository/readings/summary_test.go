package readings_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings/readingstest"
)

// TestShabbatTorahReading checks that ShabbatTorahReading sends date and il and
// returns the name and summary the /shabbatTorahReading route reports.
func TestShabbatTorahReading(t *testing.T) {
	var gotDate, gotIL string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shabbatTorahReading" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		gotDate = r.URL.Query().Get("date")
		gotIL = r.URL.Query().Get("i")
		w.Header().Set("Content-Type", "application/json")
		// The real endpoint returns the whole getLeyningForParshaHaShavua
		// object; only name.en and summary are decoded.
		w.Write([]byte(`{"name":{"en":"Vayikra","he":"וַיִּקְרָא"},"summary":"Leviticus 1:1-5:26; Deuteronomy 25:17-19","fullkriyah":{"1":{"k":"Leviticus","b":"1:1","e":"1:13"}}}`))
	})
	client := readingstest.Serve(t, h)

	got, err := client.ShabbatTorahReading(context.Background(), "2024-03-23", true)
	if err != nil {
		t.Fatalf("ShabbatTorahReading: %v", err)
	}
	if want := "Leviticus 1:1-5:26; Deuteronomy 25:17-19"; got.Summary != want {
		t.Errorf("summary = %q, want %q", got.Summary, want)
	}
	if got.Name != "Vayikra" {
		t.Errorf("name = %q, want Vayikra", got.Name)
	}
	if gotDate != "2024-03-23" {
		t.Errorf("date param = %q, want 2024-03-23", gotDate)
	}
	if gotIL != "on" {
		t.Errorf("i param = %q, want on", gotIL)
	}
}

// TestShabbatTorahReadingChag reads the holiday reading label from name.en, its
// Hebrew from name.he, and the merged summary -- the shape /shabbatTorahReading
// returns when a chag displaces the parsha.
func TestShabbatTorahReadingChag(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":{"en":"Pesach Shabbat Chol ha-Moed","he":"פֶּסַח שַׁבָּת חוֹל הַמּוֹעֵד"},"summary":"Exodus 33:12-34:26; Numbers 28:19-25; Song of Songs 1:1-8:14"}`))
	})
	client := readingstest.Serve(t, h)

	got, err := client.ShabbatTorahReading(context.Background(), "2024-04-27", false)
	if err != nil {
		t.Fatalf("ShabbatTorahReading: %v", err)
	}
	if got.Name != "Pesach Shabbat Chol ha-Moed" {
		t.Errorf("name = %q, want the chag reading label", got.Name)
	}
	if got.NameHe != "פֶּסַח שַׁבָּת חוֹל הַמּוֹעֵד" {
		t.Errorf("nameHe = %q, want the chag reading label in Hebrew", got.NameHe)
	}
	if got.Summary == "" {
		t.Errorf("summary should be carried through for a chag reading")
	}
}

// TestShabbatTorahReadingNull decodes a bare null (a defensive case) to a zero
// reading rather than erroring, and omits the i param in the Diaspora.
func TestShabbatTorahReadingNull(t *testing.T) {
	var gotIL string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIL = r.URL.Query().Get("i")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`null`))
	})
	client := readingstest.Serve(t, h)

	got, err := client.ShabbatTorahReading(context.Background(), "2024-01-06", false)
	if err != nil {
		t.Fatalf("ShabbatTorahReading: %v", err)
	}
	if got.Name != "" || got.Summary != "" {
		t.Errorf("null should decode to an empty reading, got %+v", got)
	}
	if gotIL != "" {
		t.Errorf("i param = %q, want unset in the Diaspora", gotIL)
	}
}

// TestShabbatTorahReadingNoSocket returns an error rather than panicking when
// the client has no sidecar configured.
func TestShabbatTorahReadingNoSocket(t *testing.T) {
	client := readings.New("")
	if _, err := client.ShabbatTorahReading(context.Background(), "2024-01-06", false); err == nil {
		t.Error("expected an error with no socket configured")
	}
}
