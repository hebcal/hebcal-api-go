package httpx

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// pdfShedTotal counts PDF requests refused because the renderer was already at
// its concurrency limit, so an operator can see load-shedding happening in
// /metrics rather than inferring it from latency.
var pdfShedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "pdf_shed_total",
	Help: "PDF requests rejected with 503 because the render concurrency limit was reached",
})

// pdfRenderInflight is the number of PDF renders holding a slot right now. It is
// the signal for tuning -pdf-max-concurrency: watch it against the cap, and
// against go_memstats_heap_inuse_bytes, to see how close a flood drives the pool
// to its limit before it starts shedding. A gauge, not a log line -- per-request
// heap cannot be attributed under concurrency, and ReadMemStats stops the world.
var pdfRenderInflight = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "pdf_render_inflight",
	Help: "PDF renders currently holding a concurrency slot",
})

// Limiter bounds how many wrapped handlers run at once and sheds the overflow
// with 503 rather than letting it queue.
//
// The PDF calendars are the reason it exists. Each render is only ~50ms of CPU
// but churns tens of MiB of transient heap, so on a small box an unbounded
// flood of simultaneous renders grows the heap past physical RAM and the
// machine starts swapping -- which is what turned a 50ms request into the 10-25s
// durations seen in production. Capping concurrency bounds the peak working set
// (roughly max * per-render footprint) and keeps each render running at full
// speed instead of thrashing.
//
// A request that cannot get a slot waits up to Wait for one, then is refused
// with 503 + Retry-After. Shedding beats queueing here: the client (Varnish)
// can retry or fall back, and a request left waiting only ties up memory.
type Limiter struct {
	sem  chan struct{}
	wait time.Duration
}

// NewLimiter returns a Limiter allowing max concurrent handlers, refusing a
// waiting request after wait. A max of zero (or less) disables limiting, in
// which case Wrap returns the handler unchanged.
func NewLimiter(max int, wait time.Duration) *Limiter {
	if max <= 0 {
		return nil
	}
	return &Limiter{sem: make(chan struct{}, max), wait: wait}
}

// Wrap gates h behind the limiter. A nil Limiter (max <= 0) is a no-op, so the
// feature can be turned off by configuration without a second code path.
func (l *Limiter) Wrap(h http.HandlerFunc) http.HandlerFunc {
	if l == nil {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// Fast path: a free slot, taken without arming a timer.
		select {
		case l.sem <- struct{}{}:
			defer l.release()
			pdfRenderInflight.Inc()
			h(w, r)
			return
		default:
		}
		// Saturated: wait a bounded time for a slot, but give up early if the
		// client has already gone away (no point rendering for a closed socket).
		timer := time.NewTimer(l.wait)
		defer timer.Stop()
		select {
		case l.sem <- struct{}{}:
			defer l.release()
			pdfRenderInflight.Inc()
			h(w, r)
		case <-r.Context().Done():
			pdfShedTotal.Inc()
			http.Error(w, "client closed request", 499)
		case <-timer.C:
			pdfShedTotal.Inc()
			// A shed response must not be cached: it says "busy now", not
			// "this calendar does not exist". http.Error sets no Cache-Control,
			// and the handler that would have set the 14-day one never runs.
			w.Header().Set("Retry-After", "5")
			http.Error(w, "PDF renderer is busy; please retry", http.StatusServiceUnavailable)
		}
	}
}

// release frees a slot and updates the in-flight gauge. Paired with the Inc()
// that follows each successful acquire.
func (l *Limiter) release() {
	<-l.sem
	pdfRenderInflight.Dec()
}
