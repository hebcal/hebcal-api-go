package readings_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings/readingstest"
)

func TestTorahMemos(t *testing.T) {
	var gotQuery string
	body := `[{"date":"2026-11-07","desc":"Parashat Chayei Sara","memo":"Torah: Genesis 23:1-25:18\nHaftarah: I Kings 1:1-31"}]`
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torahMemo" {
			http.Error(w, `{"error":"Not Found"}`, http.StatusNotFound)
			return
		}
		gotQuery = r.URL.RawQuery
		w.Write([]byte(body))
	})
	client := readingstest.Serve(t, h)
	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.November, 30, 0, 0, 0, 0, time.UTC)

	rows, err := client.TorahMemos(context.Background(), start, end, true)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "end=2026-11-30&i=on&start=2026-11-01" {
		t.Errorf("query %q", gotQuery)
	}
	if len(rows) != 1 || rows[0].Desc != "Parashat Chayei Sara" || rows[0].Memo != "Torah: Genesis 23:1-25:18\nHaftarah: I Kings 1:1-31" {
		t.Errorf("rows %+v", rows)
	}

	// A Hebrew date where the Gregorian one belongs ("5787-08-26") would
	// match no event; it is an error rather than a feed without readings.
	body = `[{"date":"5787-08-26","desc":"Parashat Chayei Sara","memo":"Torah: Genesis 23:1-25:18"}]`
	if _, err := client.TorahMemos(context.Background(), start, end, false); err == nil {
		t.Error("accepted a row dated outside the range")
	}

	// An older readings-svc without the route answers 404: an error, never
	// "no readings".
	h404 := readingstest.Serve(t, http.NotFoundHandler())
	if _, err := h404.TorahMemos(context.Background(), start, end, false); err == nil {
		t.Error("a 404 was taken for an empty answer")
	}
}
