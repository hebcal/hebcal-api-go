// Package reqlog carries per-request diagnostics that a handler collects deep
// in the call tree and the access-log middleware emits on the single line it
// writes for the request. A readings-svc round trip, for example, is recorded
// by the readings client and then folded into that request's log line as a
// nested "subreq" object, rather than logged on a line of its own.
package reqlog

import (
	"context"
	"sync"
	"time"
)

// Call is one outbound call to a backend service, as it should appear in the
// request's log line.
type Call struct {
	Status   int
	URL      string
	Duration time.Duration
	Length   int
}

// Collector accumulates the calls made while serving one request. It is
// safe for concurrent use, since a handler may fan out.
type Collector struct {
	mu       sync.Mutex
	calls    []Call
	err      error
	query    string
	postBody []byte
}

// Add records one backend call.
func (c *Collector) Add(call Call) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.calls = append(c.calls, call)
	c.mu.Unlock()
}

// SetError records the error a 4xx/5xx response is rendering, so the access-log
// middleware can emit its message under "msg" without re-parsing the response
// body. The last writer wins, matching the response the client actually gets.
func (c *Collector) SetError(err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
}

// SetQuery records a human-readable rendering of an otherwise opaque request,
// so the access-log middleware can emit it under "qs". The PDF handler uses it
// to log the query string a base64 download URL (the /v4/ protobuf or the
// /v2/h/ query string) decodes to, which is otherwise unreadable in the logged
// URL.
func (c *Collector) SetQuery(q string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.query = q
	c.mu.Unlock()
}

// Query returns the string recorded by SetQuery, or "".
func (c *Collector) Query() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.query
}

// SetPostBody records the raw request body, so the access-log middleware can
// emit it under "postBody". The /mcp handler uses it to log the JSON-RPC call
// (method + params) that an opaque `POST /mcp` would otherwise hide. The bytes
// are retained as-is; the middleware compacts and size-caps them at emit time.
func (c *Collector) SetPostBody(body []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.postBody = body
	c.mu.Unlock()
}

// PostBody returns the bytes recorded by SetPostBody, or nil.
func (c *Collector) PostBody() []byte {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.postBody
}

// Err returns the error recorded by SetError, or nil.
func (c *Collector) Err() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Calls returns the recorded calls in the order they were added.
func (c *Collector) Calls() []Call {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Call(nil), c.calls...)
}

type ctxKey struct{}

// NewContext returns a child context carrying a fresh Collector, plus the
// Collector itself so the middleware can read it after the handler returns.
func NewContext(ctx context.Context) (context.Context, *Collector) {
	c := &Collector{}
	return context.WithValue(ctx, ctxKey{}, c), c
}

// FromContext returns the Collector seeded by NewContext, or nil if none is
// present. A nil Collector's Add is a no-op, so callers need not check.
func FromContext(ctx context.Context) *Collector {
	c, _ := ctx.Value(ctxKey{}).(*Collector)
	return c
}
