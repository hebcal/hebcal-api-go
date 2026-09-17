package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/hebcal/hebcal-api-go/internal/reqlog"
)

// mcp forwards to the Model Context Protocol handler built by
// internal/service/mcp and wired in main. It goes through the shared
// middleware like every other route, so it gets the access log, the
// X-Response-Time header and the request counter; the MCP SDK owns the body
// and the status for a POST.
//
// The stateless SDK handler only accepts POST and answers anything else with a
// text/plain 405. An MCP client expects JSON-RPC on every response, so a
// non-POST method is rejected here instead, as a JSON-RPC error response
// rather than the SDK's plain text.
//
// Before handing off, it captures the JSON-RPC request body onto the request's
// reqlog.Collector so the access log emits it as "postBody" -- a bare
// `POST /mcp` line is otherwise opaque about which tool was called. The body is
// buffered and r.Body replaced with a fresh reader so the SDK still reads it in
// full.
//
// When MCP is nil (fonts-style optional wiring), the route answers 404 rather
// than panicking.
func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	if s.MCP == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeMCPMethodNotAllowed(w, r.Method)
		return
	}
	// Only POST carries a JSON-RPC body. Read the whole body (the SDK would
	// anyway) and restore it; the middleware compacts and size-caps it at emit
	// time.
	if r.Body != nil {
		if body, err := io.ReadAll(r.Body); err == nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			reqlog.FromContext(r.Context()).SetPostBody(body)
		}
	}
	s.MCP.ServeHTTP(w, r)
}

// writeMCPMethodNotAllowed answers a non-POST /mcp request with a JSON-RPC
// error rather than the MCP SDK's text/plain "Method Not Allowed", so an MCP
// client parsing every response as JSON-RPC gets one. -32000 is not a code
// JSON-RPC 2.0 assigns a meaning to; it is simply the top of the
// implementation-defined "server error" range the spec reserves
// (-32000 to -32099), which fits a transport-level rejection this well.
func writeMCPMethodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", http.MethodPost)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	fmt.Fprintf(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"Method %s not allowed"}}`, method)
}
