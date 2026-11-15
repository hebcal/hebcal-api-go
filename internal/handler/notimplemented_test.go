package handler

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestUnsupportedCfgMisdirected verifies that a /shabbat or /converter
// request without a supported cfg is 421 on download.hebcal.com and still
// 501 on www.hebcal.com.
func TestUnsupportedCfgMisdirected(t *testing.T) {
	srv := testServerWithDB(t)
	cases := []struct {
		host, path string
		want       int
	}{
		{"download.hebcal.com", "/shabbat", http.StatusMisdirectedRequest},
		{"download.hebcal.com", "/shabbat?geonameid=1880252", http.StatusMisdirectedRequest},
		{"Download.Hebcal.com:443", "/shabbat?cfg=r", http.StatusMisdirectedRequest},
		{"download.hebcal.com", "/converter", http.StatusMisdirectedRequest},
		{"download.hebcal.com", "/converter?gd=14&gm=11&gy=2026&g2h=1", http.StatusMisdirectedRequest},
		{"www.hebcal.com", "/shabbat", http.StatusNotImplemented},
		{"www.hebcal.com", "/converter?gd=14&gm=11&gy=2026&g2h=1", http.StatusNotImplemented},
	}
	for _, c := range cases {
		req, err := http.NewRequest(http.MethodGet, srv.URL+c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = c.host
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s %s: status = %d, want %d (%s)", c.host, c.path, resp.StatusCode, c.want, body)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s %s: Content-Type = %q", c.host, c.path, ct)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("%s %s: Cache-Control = %q, want none", c.host, c.path, cc)
		}
	}
}
