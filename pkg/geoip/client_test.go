package geoip

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// shortSocketPath returns a path for a unix socket that stays within the
// ~104-byte sun_path limit. t.TempDir() on macOS returns a long /var/folders
// path that overflows it (net.Listen fails with "bind: invalid argument"), so
// anchor the socket under a short base dir instead.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "geoip")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "geoip2.sock")
}

func TestGeoIPClientReusesUnixSocketConnection(t *testing.T) {
	socketPath := shortSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	var newConns int
	srv := &http.Server{ConnState: func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns++
		}
	}}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"location":{"latitude":37.3861,"longitude":-122.0839}}`)
	})
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client := New(socketPath)
	for i := 0; i < 2; i++ {
		if _, err := client.LookupPoint(t.Context(), "8.8.8.8"); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	if newConns != 1 {
		t.Fatalf("new unix socket connections = %d, want 1", newConns)
	}
}

func TestLookupPointUnixSocket(t *testing.T) {
	socketPath := shortSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 56\r\n\r\n{\"location\":{\"latitude\":37.3861,\"longitude\":-122.0839}}"))
	}()
	pt, err := New(socketPath).LookupPoint(t.Context(), "8.8.8.8")
	if err != nil {
		t.Fatalf("LookupPoint: %v", err)
	}
	if pt.Latitude != 37.3861 || pt.Longitude != -122.0839 {
		t.Fatalf("point = %#v, want Mountain View coordinates", pt)
	}
}

// serveJSON stands up a one-shot geoip2 server on a unix socket that answers a
// single request with the given JSON body, and returns a client pointed at it.
func serveJSON(t *testing.T, body string) *Client {
	t.Helper()
	socketPath := shortSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
		resp := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
		_, _ = conn.Write([]byte(resp))
	}()
	return New(socketPath)
}

func TestLookupPointAccuracyRadius(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "coarse radius is rejected",
			body: `{"location":{"latitude":38.938,"longitude":-76.557,"accuracy_radius":1000}}`,
			// 1000 > 500: too imprecise, discard as if no answer.
			wantErr: true,
		},
		{
			name: "boundary radius is kept",
			body: `{"location":{"latitude":38.938,"longitude":-76.557,"accuracy_radius":500}}`,
			// Exactly 500 is not > 500, so it passes.
			wantErr: false,
		},
		{
			name: "missing radius is kept",
			body: `{"location":{"latitude":37.3861,"longitude":-122.0839}}`,
			// Absent field decodes to 0, which is not > 500.
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, err := serveJSON(t, tt.body).LookupPoint(t.Context(), "8.8.8.8")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LookupPoint = %#v, want error", pt)
				}
				return
			}
			if err != nil {
				t.Fatalf("LookupPoint: %v", err)
			}
			if pt == nil {
				t.Fatal("LookupPoint returned nil point without error")
			}
		})
	}
}
