package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings/readingstest"
	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// The transport half of the CSV calendars. What each row says is
// internal/service/csv's business, not this file's.

func TestCSVResponse(t *testing.T) {
	// Needs no fonts: the CSV routes do not draw anything.
	app, srv := testServer(t)
	app.CSV.Geo = testGeoDB(t)
	msg := &pb.Download{Year: 2026, IsHebrewYear: false, Major: true, Candlelighting: true,
		Geonameid: 5128581, NumYears: 1}
	path := "/v4/" + encode(t, msg) + "/hebcal_2026_new_york.csv"

	resp, body := get(t, srv, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	for header, want := range map[string]string{
		"Content-Type":                "text/x-csv; charset=utf-8",
		"Content-Disposition":         `attachment; filename="hebcal_2026_new_york.csv"`,
		"Cache-Control":               httpx.CacheControl14Days,
		"Access-Control-Allow-Origin": "*",
		"X-Content-Type-Options":      "nosniff",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if !strings.HasPrefix(body, "\xEF\xBB\xBF\"Subject\",") {
		t.Errorf("body does not begin with the BOM and header: %.40q", body)
	}
	if !strings.Contains(body, `,"New York"`+"\r\n") {
		t.Error("timed events should carry the location")
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	resp, _ = get(t, srv, path, "If-None-Match", etag)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("conditional request: status = %d, want 304", resp.StatusCode)
	}
}

func TestCSVErrors(t *testing.T) {
	app, srv := testServer(t)
	app.CSV.Geo = testGeoDB(t)
	tests := []struct {
		name string
		msg  *pb.Download
		want int
	}{
		{"unknown location", &pb.Download{Year: 2026, Major: true, Geonameid: 1}, http.StatusNotFound},
		{"year out of range", &pb.Download{Year: 9999, Major: true}, http.StatusGone},
		// no readings-svc is configured
		{"unsupported series", &pb.Download{Year: 2026, Major: true, DirshuAmudYomi: true}, http.StatusNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := get(t, srv, "/v4/"+encode(t, tc.msg)+"/hebcal.csv")
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d: %s", resp.StatusCode, tc.want, body)
			}
		})
	}
	resp, _ := get(t, srv, "/v4/!!!/hebcal.csv")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("undecodable payload: status = %d, want 404", resp.StatusCode)
	}
}

func TestCSVLearningFallback(t *testing.T) {
	up := readingstest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"title":"Yoma 49a","date":"2026-08-01",` +
			`"category":"dirshuAmudYomi","memo":"first line\nsecond, line"}]}`))
	}))
	app, srv := testServer(t)
	app.CSV.Learning = pdf.NewLearningFetcher(up)
	msg := &pb.Download{Year: 2026, Month: 8, DirshuAmudYomi: true}
	resp, body := get(t, srv, "/v4/"+encode(t, msg)+"/hebcal.csv")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	want := `"Yoma 49a","8/1/2026",,,,"true","first line / second; line","3","Jewish Holidays"` + "\r\n"
	if !strings.Contains(body, want) {
		t.Errorf("missing fetched row %q in\n%s", want, body)
	}
}
