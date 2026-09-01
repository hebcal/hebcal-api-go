package pdf

import (
	"bytes"
	"os"
	"testing"

	"github.com/hebcal/hebcal-go/hebcal"
)

func benchFonts(b *testing.B) *FontSet {
	b.Helper()
	if _, err := os.Stat(fontDir); err != nil {
		b.Skipf("no %s directory; skipping benchmark that needs real fonts", fontDir)
	}
	fs, err := LoadFonts(fontDir)
	if err != nil {
		b.Fatalf("LoadFonts: %v", err)
	}
	return fs
}

// benchParams mirrors the traffic in the access log: a Spanish, full-holiday,
// candle-lighting, sedrot single-year Gregorian calendar for a geoname
// location.
func benchParams() *Params {
	p := &Params{Locale: "es", MonthMode: GregorianArabic}
	p.Opts = hebcal.CalOptions{
		Year:           2026,
		Sedrot:         true,
		CandleLighting: true,
		Location:       paloAlto(),
		// maj/min/nx/mod/mf/ss all on in the sampled traffic: everything
		// enabled, nothing suppressed.
	}
	return p
}

func BenchmarkGenerate(b *testing.B) {
	p := benchParams()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Generate(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRender(b *testing.B) {
	fs := benchFonts(b)
	p := benchParams()
	events, err := Generate(p)
	if err != nil {
		b.Fatal(err)
	}
	title := CalendarTitle(p, events)
	r := NewRenderer(fs)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := r.Render(&buf, p, events, title); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrepareAndRender(b *testing.B) {
	fs := benchFonts(b)
	p := benchParams()
	r := NewRenderer(fs)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		events, err := Generate(p)
		if err != nil {
			b.Fatal(err)
		}
		title := CalendarTitle(p, events)
		var buf bytes.Buffer
		if err := r.Render(&buf, p, events, title); err != nil {
			b.Fatal(err)
		}
	}
}
