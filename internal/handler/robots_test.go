package handler

import (
	"net/http"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
)

func TestRobotsTxt(t *testing.T) {
	_, srv := testServer(t)

	resp, body := get(t, srv, "/robots.txt")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if want := "User-agent: *\nAllow: /\n"; body != want {
		t.Errorf("body %q, want %q", body, want)
	}
	if got := resp.Header.Get("Content-Type"); got != httpx.ContentTypeText {
		t.Errorf("Content-Type %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != httpx.CacheControl1Day {
		t.Errorf("Cache-Control %q", got)
	}

	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/robots.txt", nil)
	headResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	headResp.Body.Close()
	if headResp.StatusCode != http.StatusOK {
		t.Errorf("HEAD status %d", headResp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/robots.txt", nil)
	postResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status %d, want 405", postResp.StatusCode)
	}
}
