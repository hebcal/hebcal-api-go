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
	mu    sync.Mutex
	calls []Call
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
