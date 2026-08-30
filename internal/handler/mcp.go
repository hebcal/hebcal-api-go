package handler

import (
	"bytes"
	"io"
	"net/http"

	"github.com/hebcal/hebcal-api-go/internal/reqlog"
)

// mcp forwards to the Model Context Protocol handler built by
// internal/service/mcp and wired in main. It goes through the shared
// middleware like every other route, so it gets the access log, the
// X-Response-Time header and the request counter; the MCP SDK owns the body,
// the status, and the 405 for a non-POST method.
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
	// Only POST carries a JSON-RPC body; the GET SSE stream and DELETE session
	// teardown have none. Read the whole body (the SDK would anyway) and restore
	// it; the middleware compacts and size-caps it at emit time.
	if r.Method == http.MethodPost && r.Body != nil {
		if body, err := io.ReadAll(r.Body); err == nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			reqlog.FromContext(r.Context()).SetPostBody(body)
		}
	}
	s.MCP.ServeHTTP(w, r)
}
