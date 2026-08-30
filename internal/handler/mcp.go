package handler

import "net/http"

// mcp forwards to the Model Context Protocol handler built by
// internal/service/mcp and wired in main. It goes through the shared
// middleware like every other route, so it gets the access log, the
// X-Response-Time header and the request counter; the MCP SDK owns the body,
// the status, and the 405 for a non-POST method.
//
// When MCP is nil (fonts-style optional wiring), the route answers 404 rather
// than panicking.
func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	if s.MCP == nil {
		http.NotFound(w, r)
		return
	}
	s.MCP.ServeHTTP(w, r)
}
