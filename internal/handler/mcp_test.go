package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/reqlog"
)

// The /mcp handler records the JSON-RPC POST body onto the request collector
// (which the access log emits as "postBody") and still hands the MCP SDK an
// intact body -- the logging read must not consume it.
func TestMCPCapturesAndRestoresBody(t *testing.T) {
	const body = `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`
	var gotBody, gotRecorded string
	app := New(logger.NewWriter(io.Discard, "test"))
	app.MCP = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotRecorded = string(reqlog.FromContext(r.Context()).PostBody())
	})
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/mcp", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotBody != body {
		t.Errorf("SDK saw body %q, want the original %q (logging consumed it)", gotBody, body)
	}
	if gotRecorded != body {
		t.Errorf("recorded postBody %q, want %q", gotRecorded, body)
	}
}

// A non-POST /mcp request is rejected before reaching the SDK, as a JSON-RPC
// error rather than the SDK's own text/plain "Method Not Allowed".
func TestMCPMethodNotAllowedIsJSONRPC(t *testing.T) {
	app := New(logger.NewWriter(io.Discard, "test"))
	app.MCP = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("SDK handler should not be reached for a non-POST method")
	})
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if allow := resp.Header.Get("Allow"); allow != http.MethodPost {
		t.Errorf("Allow = %q, want POST", allow)
	}
	body, _ := io.ReadAll(resp.Body)
	want := `{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"Method GET not allowed"}}`
	if string(body) != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// When MCP is nil (optional wiring) the route answers 404 rather than panicking.
func TestMCPNilAnswers404(t *testing.T) {
	app := New(logger.NewWriter(io.Discard, "test")) // app.MCP left nil
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/mcp", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
