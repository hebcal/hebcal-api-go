// Package readingstest starts a stub readings-svc on a unix domain socket, so
// the /shabbat and PDF tests exercise the real transport rather than a
// substitute over TCP.
package readingstest

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
)

// Serve starts h on a unix domain socket and returns a Client pointed at it.
// The server and the socket are torn down when the test ends.
func Serve(t *testing.T, h http.Handler) *readings.Client {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Listener.Close()
	ln, err := net.Listen("unix", socketPath(t))
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return readings.New(ln.Addr().String())
}

// socketPath returns a path that stays within the ~104-byte sun_path limit.
// t.TempDir() on macOS returns a long /var/folders path that overflows it
// (net.Listen fails with "bind: invalid argument"), so anchor the socket under
// a short base directory instead.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "readings")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "readings.sock")
}
