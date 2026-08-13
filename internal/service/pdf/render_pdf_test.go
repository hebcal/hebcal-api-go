package pdf

import (
	"bytes"
	"compress/zlib"
	"regexp"
	"strings"
	"testing"

	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"
)

// renderCalendar builds a real PDF the way the handler does, so the assertions
// below run against the same path production would take.
func renderCalendar(t *testing.T, p *Params) []byte {
	t.Helper()
	fs := testFonts(t)
	events, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events generated")
	}
	var buf bytes.Buffer
	if err := NewRenderer(fs).Render(&buf, p, events, "Test Calendar"); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return buf.Bytes()
}

// paloAlto is a location that produces candle-lighting times without needing
// the geo databases.
func paloAlto() *zmanim.Location {
	return &zmanim.Location{
		Name: "Palo Alto", Latitude: 37.44, Longitude: -122.14,
		TimeZoneId: "America/Los_Angeles",
	}
}

func gregorianParams() *Params {
	p := &Params{Locale: "en", MonthMode: GregorianArabic}
	p.Opts = hebcal.CalOptions{
		Year: 2026, Sedrot: true, CandleLighting: true, Location: paloAlto(),
	}
	return p
}

// inflateAll returns the document's raw bytes plus every stream it can
// decompress, which is where fonts, CMaps and object streams live.
func inflateAll(pdf []byte) []byte {
	var out bytes.Buffer
	out.Write(pdf)
	for _, m := range regexp.MustCompile(`stream\r?\n`).FindAllIndex(pdf, -1) {
		end := bytes.Index(pdf[m[1]:], []byte("endstream"))
		if end < 0 {
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader(pdf[m[1] : m[1]+end]))
		if err != nil {
			continue
		}
		var dec bytes.Buffer
		if _, err := dec.ReadFrom(zr); err == nil {
			out.Write(dec.Bytes())
		}
		zr.Close()
	}
	return out.Bytes()
}

func TestRenderProducesAValidPDF(t *testing.T) {
	pdf := renderCalendar(t, gregorianParams())
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.")) {
		t.Errorf("missing PDF header, got %q", pdf[:min(16, len(pdf))])
	}
	if !bytes.Contains(pdf[max(0, len(pdf)-64):], []byte("%%EOF")) {
		t.Error("missing EOF trailer")
	}
	if len(pdf) < 10000 {
		t.Errorf("PDF is only %d bytes; a year of events should be much larger", len(pdf))
	}
}

// An empty ToUnicode destination is what broke copy/paste and search on Hebrew
// calendars: extractors disagree about `<>`, and some repeat the base letter.
// No CMap in a rendered document may contain one.
func TestNoEmptyToUnicodeDestinations(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    *Params
	}{
		{"english", gregorianParams()},
		{"hebrew", hebrewParams()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			all := inflateAll(renderCalendar(t, tc.p))
			for _, cmap := range regexp.MustCompile(`(?s)begincmap.*?endcmap`).FindAll(all, -1) {
				body := string(cmap)
				if i := strings.Index(body, "endcodespacerange"); i >= 0 {
					body = body[i:]
				}
				if n := strings.Count(body, "<>"); n > 0 {
					t.Errorf("CMap has %d empty destination(s):\n%s", n, cmap)
				}
			}
		})
	}
}

func hebrewParams() *Params {
	p := &Params{Locale: "he", RTL: true, MonthMode: GregorianArabic}
	p.Opts = hebcal.CalOptions{
		Year: 2026, Sedrot: true, CandleLighting: true, Location: paloAlto(),
	}
	return p
}

// Events carry links to their pages on hebcal.com. A calendar with a year of
// holidays and parshiyot should have a substantial number of them, and each
// should be a tracked short URL.
func TestRenderEmbedsTrackedLinks(t *testing.T) {
	all := inflateAll(renderCalendar(t, gregorianParams()))
	links := bytes.Count(all, []byte("/Subtype /Link")) + bytes.Count(all, []byte("/Subtype/Link"))
	if links < 50 {
		t.Errorf("found %d link annotations, want at least 50 for a year", links)
	}
	uris := regexp.MustCompile(`/URI\s*\(([^)]+)\)`).FindAllSubmatch(all, -1)
	if len(uris) == 0 {
		t.Fatal("no URI actions found")
	}
	for _, u := range uris {
		s := string(u[1])
		if !strings.HasPrefix(s, "https://hebcal.com/") {
			t.Errorf("link %q should point at the short hebcal.com form", s)
			break
		}
		if !strings.Contains(s, "uc=") {
			t.Errorf("link %q is missing its uc= campaign", s)
			break
		}
	}
}

// Both font formats have to reach the page: Source Sans Pro is TrueType and
// Adobe Hebrew is OpenType/CFF, embedded through different code paths.
func TestHebrewCalendarEmbedsTheHebrewFont(t *testing.T) {
	all := inflateAll(renderCalendar(t, hebrewParams()))
	if !bytes.Contains(all, []byte("AdobeHebrew")) {
		t.Error("a Hebrew calendar should embed an Adobe Hebrew face")
	}
}

// mm=1 and mm=2 paginate by Hebrew month instead of Gregorian.
func TestHebrewMonthModesRender(t *testing.T) {
	for _, mm := range []MonthMode{HebrewArabic, HebrewHebrew} {
		p := hebrewParams()
		p.MonthMode = mm
		p.Opts.IsHebrewYear = true
		p.Opts.Year = 5787
		pdf := renderCalendar(t, p)
		if len(pdf) < 10000 {
			t.Errorf("mm=%v produced only %d bytes", mm, len(pdf))
		}
	}
}

// contentStreams returns just the page-drawing streams, identified by the text
// and path operators they contain.
//
// Whole files never match between runs: the PDF carries a random file ID and a
// creation timestamp. hebcal-web's perf harness compares inflated content
// streams for the same reason, and that is what actually determines what a
// reader sees.
func contentStreams(pdf []byte) [][]byte {
	var out [][]byte
	for _, m := range regexp.MustCompile(`stream\r?\n`).FindAllIndex(pdf, -1) {
		end := bytes.Index(pdf[m[1]:], []byte("endstream"))
		if end < 0 {
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader(pdf[m[1] : m[1]+end]))
		if err != nil {
			continue
		}
		var dec bytes.Buffer
		_, err = dec.ReadFrom(zr)
		zr.Close()
		if err != nil {
			continue
		}
		if bytes.Contains(dec.Bytes(), []byte(" Tf")) && bytes.Contains(dec.Bytes(), []byte("BT")) {
			out = append(out, dec.Bytes())
		}
	}
	return out
}

// Rendering the same calendar twice must draw exactly the same thing.
func TestRenderIsDeterministic(t *testing.T) {
	a := contentStreams(renderCalendar(t, gregorianParams()))
	b := contentStreams(renderCalendar(t, gregorianParams()))
	if len(a) == 0 {
		t.Fatal("no content streams found")
	}
	if len(a) != len(b) {
		t.Fatalf("got %d content streams then %d", len(a), len(b))
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Errorf("content stream %d differs between two renders of the same calendar", i)
			return
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
