package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
)

// The expected redirects are hebcal-web's shortUrlRedir() output for the same
// URLs, with the query string in url.Values' sorted encoding rather than
// URLSearchParams' insertion order.
func TestShortURL(t *testing.T) {
	_, srv := testServer(t)
	tests := []struct {
		path   string
		status int
		want   string // the Location for a 302, the error message otherwise
	}{
		{"/s/5786/12", 302, "https://www.hebcal.com/sedrot/vayechi-20260103?utm_medium=redir&utm_source=redir"},
		{"/s/5786i/12?uc=pdf-2026", 302, "https://www.hebcal.com/sedrot/vayechi-20260103?i=on&utm_campaign=pdf-2026&utm_medium=document&utm_source=pdf"},
		{"/s/5786/22d?us=foo&um=bar", 302, "https://www.hebcal.com/sedrot/vayakhel-pekudei-20260314?utm_medium=bar&utm_source=foo"},
		{"/s/5786/21d", 400, "invalid short redirect double parsha: 21d"},
		{"/s/5786/54", 404, "short redirect not found: 5786/54"},
		{"/s/5786/55", 400, "invalid short redirect parsha: 55"},
		{"/s/5786/0", 400, "invalid short redirect parsha: 0"},
		{"/s/5786/42", 404, "short redirect not found: 5786/42"},
		{"/s/5785/42d", 302, "https://www.hebcal.com/sedrot/matot-masei-20250726?utm_medium=redir&utm_source=redir"},
		{"/s/5787/51d?uc=pdf-palo-alto-5787", 302, "https://www.hebcal.com/sedrot/nitzavim-vayeilech-20270925?utm_campaign=pdf-palo-alto-5787&utm_medium=document&utm_source=pdf"},
		{"/s/5787/52d", 400, "invalid short redirect double parsha: 52d"},
		{"/s/1000/1", 410, "short redirect year out of range: 1000"},
		{"/s/abc/1", 302, "https://www.hebcal.com/sedrot/abc/1?utm_medium=redir&utm_source=redir"},
		{"/s/12", 400, "invalid short redirect parsha: 12"},
		{"/s/", 302, "https://www.hebcal.com/sedrot/"},
		{"/s/vayera?x=1", 302, "https://www.hebcal.com/sedrot/vayera?utm_medium=redir&utm_source=redir&x=1"},
		{"/s/5786/12/", 302, "https://www.hebcal.com/sedrot/vayechi-20260103?utm_medium=redir&utm_source=redir"},
		{"/h/chanukah-2026?uc=ical-foo&a=b+c&z=%7E*~", 302, "https://www.hebcal.com/holidays/chanukah-2026?a=b+c&utm_campaign=ical-foo&utm_medium=icalendar&utm_source=js&z=~%2A~"},
		{"/o/5786/3?utm_source=x&a=1&a=2", 302, "https://www.hebcal.com/omer/5786/3?a=1&a=2&utm_medium=redir&utm_source=x"},
		{"/h/purim?i=on&uc=other", 302, "https://www.hebcal.com/holidays/purim?i=on&utm_campaign=other&utm_medium=redir&utm_source=redir"},
		{"/s/3860/1", 302, "https://www.hebcal.com/sedrot/bereshit-00990926?utm_medium=redir&utm_source=redir"},
		{"/s/6760/1", 410, "short redirect year out of range: 6760"},
		{"/s/5786/53", 302, "https://www.hebcal.com/sedrot/haazinu-20251004?utm_medium=redir&utm_source=redir"},
	}
	for _, tt := range tests {
		resp, body := get(t, srv, tt.path)
		if resp.StatusCode != tt.status {
			t.Errorf("%s: status = %d, want %d (%s)", tt.path, resp.StatusCode, tt.status, body)
			continue
		}
		if tt.status == http.StatusFound {
			if loc := resp.Header.Get("Location"); loc != tt.want {
				t.Errorf("%s: Location\n got: %s\nwant: %s", tt.path, loc, tt.want)
			}
			if cc := resp.Header.Get("Cache-Control"); cc != httpx.CacheControl1Day {
				t.Errorf("%s: Cache-Control = %q", tt.path, cc)
			}
			continue
		}
		if got := strings.TrimSpace(body); got != tt.want {
			t.Errorf("%s: body = %q, want %q", tt.path, got, tt.want)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("%s: error carries Cache-Control %q", tt.path, cc)
		}
	}
}

func TestShortURLReferer(t *testing.T) {
	_, srv := testServer(t)
	tests := []struct{ ref, want string }{
		{"https://www.Example.org/x", "https://www.hebcal.com/holidays/foo?utm_medium=redir&utm_source=example.org"},
		{"https://hebcal.com/", "https://www.hebcal.com/holidays/foo?utm_medium=redir&utm_source=redir"},
		{"https://download.hebcal.com/", "https://www.hebcal.com/holidays/foo?utm_medium=redir&utm_source=redir"},
		{"not a url", "https://www.hebcal.com/holidays/foo?utm_medium=redir&utm_source=redir"},
	}
	for _, tt := range tests {
		resp, _ := get(t, srv, "/h/foo", "Referer", tt.ref)
		if loc := resp.Header.Get("Location"); loc != tt.want {
			t.Errorf("Referer %q: Location\n got: %s\nwant: %s", tt.ref, loc, tt.want)
		}
	}
	// An explicit source wins over the referrer.
	resp, _ := get(t, srv, "/h/foo?us=mail", "Referer", "https://example.org/")
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "utm_source=mail") {
		t.Errorf("explicit source: Location = %s", loc)
	}
}

func TestShortURLMethods(t *testing.T) {
	_, srv := testServer(t)
	resp, err := http.Post(srv.URL+"/s/5786/12", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("POST: Allow = %q", allow)
	}
	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/s/5786/12", nil)
	resp, err = http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("HEAD: status = %d, want 302", resp.StatusCode)
	}
}
