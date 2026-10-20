package httpx

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// jsonish returns n bytes of repetitive event-like JSON.
func jsonish(n int) []byte {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, `{"date":"2026-%02d-%02d","title":"Candle lighting: %d:%02dpm"},`, i%12+1, i%28+1, i%12, i%60)
	}
	return []byte(b.String()[:n])
}

func decompress(t *testing.T, enc string, body []byte) []byte {
	t.Helper()
	var r io.Reader
	switch enc {
	case "zstd":
		d, err := zstd.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		r = d
	case "br":
		r = brotli.NewReader(bytes.NewReader(body))
	case "gzip":
		g, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r = g
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("%s: %v", enc, err)
	}
	return out
}

// TestCompressRoundTrip covers sizes on both sides of each brotli window
// boundary, and reuses pooled writers concurrently, so a writer that kept
// state across Reset or was handed out twice would corrupt a body.
func TestCompressRoundTrip(t *testing.T) {
	sizes := []int{1, 513, 4096, 131056, 131057, 1 << 20, 5 << 20}
	var wg sync.WaitGroup
	for _, enc := range []string{"zstd", "br", "gzip"} {
		for _, n := range sizes {
			for range 3 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					in := jsonish(n)
					if out := decompress(t, enc, compress(enc, in)); !bytes.Equal(out, in) {
						t.Errorf("%s/%d: round trip mismatch", enc, n)
					}
				}()
			}
		}
	}
	wg.Wait()
}

func TestBrotliLGWin(t *testing.T) {
	for _, tc := range []struct{ n, want int }{
		{0, 17}, {131056, 17}, {131057, 18}, {4<<20 - 16, 22}, {4<<20 - 15, 22}, {100 << 20, 22},
	} {
		if got := brotliLGWin(tc.n); got != tc.want {
			t.Errorf("brotliLGWin(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}

// BenchmarkCompress reports the per-response allocation of each encoding.
func BenchmarkCompress(b *testing.B) {
	for _, n := range []int{2 << 10, 50 << 10, 500 << 10} {
		in := jsonish(n)
		for _, enc := range []string{"zstd", "br", "gzip"} {
			b.Run(fmt.Sprintf("%s/%dK", enc, n>>10), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					compress(enc, in)
				}
			})
		}
	}
}
