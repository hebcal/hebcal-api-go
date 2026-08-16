package httpx

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestLimiterNilIsPassthrough: a disabled limiter (max <= 0) runs the handler
// unchanged, so turning the feature off needs no second code path.
func TestLimiterNilIsPassthrough(t *testing.T) {
	var l *Limiter // NewLimiter(0, ...) returns nil
	called := false
	h := l.Wrap(func(w http.ResponseWriter, r *http.Request) { called = true })
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v4/x.pdf", nil))
	if !called {
		t.Fatal("nil limiter did not call the handler")
	}
	if NewLimiter(0, time.Second) != nil {
		t.Fatal("NewLimiter(0) should be nil (disabled)")
	}
}

// TestLimiterShedsWhenSaturated: with one slot held, the next request is shed
// with 503 + Retry-After after the queue timeout rather than queued.
func TestLimiterShedsWhenSaturated(t *testing.T) {
	l := NewLimiter(1, 30*time.Millisecond)

	release := make(chan struct{})
	entered := make(chan struct{})
	block := l.Wrap(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		block(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v4/x.pdf", nil))
	}()
	<-entered // the one slot is now occupied

	// A second request cannot get the slot within the timeout, so it is shed.
	shedHandlerRan := false
	shed := l.Wrap(func(w http.ResponseWriter, r *http.Request) { shedHandlerRan = true })
	rec := httptest.NewRecorder()
	t0 := time.Now()
	shed(rec, httptest.NewRequest(http.MethodGet, "/v4/y.pdf", nil))
	waited := time.Since(t0)

	if shedHandlerRan {
		t.Error("shed request should not have run the handler")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("shed status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("shed 503 is missing Retry-After")
	}
	if waited < 30*time.Millisecond {
		t.Errorf("shed after %v, expected to wait the full queue timeout", waited)
	}

	close(release)
	wg.Wait()
}

// TestLimiterFastPathReleases: once a slot is freed, a later request takes the
// fast path (no wait) and runs the handler.
func TestLimiterFastPathReleases(t *testing.T) {
	l := NewLimiter(1, time.Second)
	ran := 0
	h := l.Wrap(func(w http.ResponseWriter, r *http.Request) { ran++ })
	for i := 0; i < 3; i++ {
		h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v4/x.pdf", nil))
	}
	if ran != 3 {
		t.Fatalf("ran %d handlers, want 3 (slot not released between calls)", ran)
	}
}
