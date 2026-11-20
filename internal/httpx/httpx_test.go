package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// An error written after a handler has already set caching headers for the
// body it meant to send must not carry them: a transient 500 or 503 would
// otherwise be cached as long as the calendar.
func TestErrorHelpersClearCaching(t *testing.T) {
	helpers := map[string]func(http.ResponseWriter, error){
		"WritePlainError": WritePlainError,
		"WriteJSONError":  WriteJSONError,
	}
	errs := []error{
		model.BadRequest("bad"),
		model.Internal("render: boom"),
		model.Unavailable("sidecar down"),
	}
	for name, write := range helpers {
		for _, err := range errs {
			rec := httptest.NewRecorder()
			h := rec.Header()
			h.Set("Cache-Control", CacheControl14Days)
			h.Set("Expires", "Sun, 08 Nov 2026 00:00:00 GMT")
			h.Set("Last-Modified", "Fri, 06 Nov 2026 00:00:00 GMT")
			h.Set("ETag", `W/"abc"`)
			h.Set("Access-Control-Allow-Origin", "*")
			write(rec, err)
			if rec.Code != model.StatusOf(err) {
				t.Errorf("%s(%v): status %d, want %d", name, err, rec.Code, model.StatusOf(err))
			}
			for _, k := range cachingHeaders {
				if v := rec.Header().Get(k); v != "" {
					t.Errorf("%s(%v): %s = %q, want none", name, err, k, v)
				}
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Errorf("%s(%v): dropped a non-caching header", name, err)
			}
		}
	}
}

func TestPosixError(t *testing.T) {
	_, err := os.Stat(filepath.Join(t.TempDir(), "missing"))
	if got := PosixError(err).Error(); got != "No such file or directory" {
		t.Errorf("ENOENT: %q", got)
	}
	if got := PosixError(syscall.EACCES).Error(); got != "Permission denied" {
		t.Errorf("EACCES: %q", got)
	}
	plain := errors.New("not an errno")
	if PosixError(plain) != plain {
		t.Errorf("an error without an errno must pass through")
	}
}
