package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/hebcal/hdate"
)

func gzSize(b []byte) int {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Len()
}

func brSize(b []byte, level int) int {
	var buf bytes.Buffer
	bw := brotli.NewWriterLevel(&buf, level)
	bw.Write(b)
	bw.Close()
	return buf.Len()
}

// TestThresholdExperiment prints compressed sizes for representative response
// bodies; used to choose the compression threshold empirically.
// Run with: RUN_EXPERIMENT=1 go test -run TestThresholdExperiment -v
func TestThresholdExperiment(t *testing.T) {
	if os.Getenv("RUN_EXPERIMENT") == "" {
		t.Skip("set RUN_EXPERIMENT=1 to run")
	}
	q := func(s string) url.Values {
		v, _ := url.ParseQuery(s)
		return v
	}
	single := func(gy int, gm time.Month, gd int) convProps {
		return g2h(gregDate{Year: gy, Month: gm, Day: gd}, false, false)
	}
	type sample struct {
		name string
		body []byte
	}
	var samples []sample
	add := func(name string, body []byte) {
		samples = append(samples, sample{name, body})
	}

	add("json single plain", renderSingleJSON(single(2026, time.July, 5), q(""), "s"))
	add("json single busy", renderSingleJSON(single(2022, time.January, 1), q(""), "s"))
	add("json single lg=h", renderSingleJSON(single(2022, time.January, 1), q(""), "h"))
	add("xml single", renderXML(single(2026, time.July, 5), q(""), "s"))
	add("xml busy", renderXML(single(2022, time.January, 1), q(""), "s"))
	add("xml chanukah", renderXML(single(2025, time.December, 21), q(""), "s"))
	for _, days := range []int{2, 3, 4, 5, 7, 14, 30, 180} {
		start := gregDate{Year: 2025, Month: time.January, Day: 1}
		p := convProps{isRange: true, startRD: start.RD(), endRD: start.RD() + int64(days-1)}
		add(fmt.Sprintf("json range %d days", days), renderRangeJSON(p, q(""), "s"))
	}
	add("csv", renderCSV(hdate.New(5786, hdate.Tamuz, 20)))

	t.Logf("%-22s %7s %7s %7s %7s %7s", "sample", "raw", "gzip", "gz%", "br5", "br5%")
	for _, s := range samples {
		raw := len(s.body)
		gz := gzSize(s.body)
		b5 := brSize(s.body, 5)
		t.Logf("%-22s %7d %7d %6.1f%% %7d %6.1f%%",
			s.name, raw, gz, 100*float64(gz)/float64(raw),
			b5, 100*float64(b5)/float64(raw))
	}

	mid := samples[9].body
	n := 2000
	t0 := time.Now()
	for i := 0; i < n; i++ {
		gzSize(mid)
	}
	gzDur := time.Since(t0)
	t0 = time.Now()
	for i := 0; i < n; i++ {
		brSize(mid, 5)
	}
	brDur := time.Since(t0)
	t.Logf("per-op on %d-byte body: gzip %v, brotli-5 %v",
		len(mid), gzDur/time.Duration(n), brDur/time.Duration(n))
}
