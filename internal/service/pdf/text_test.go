package pdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fontDir is where the Source Sans Pro and Adobe Hebrew families live: $FONT_DIR
// if it is set, otherwise the repo root's fonts/, which is a symlink to
// hebcal-web's copy. Tests that need real glyph metrics skip when it is absent
// rather than failing on a fresh checkout.
var fontDir = func() string {
	if dir := os.Getenv("FONT_DIR"); dir != "" {
		return dir
	}
	return filepath.Join("..", "..", "..", "fonts")
}()

func testFonts(t *testing.T) *FontSet {
	t.Helper()
	if _, err := os.Stat(fontDir); err != nil {
		t.Skipf("no %s directory; skipping tests that need real fonts", fontDir)
	}
	fs, err := LoadFonts(fontDir)
	if err != nil {
		t.Fatalf("LoadFonts: %v", err)
	}
	return fs
}

func testShaper(t *testing.T) *Shaper {
	t.Helper()
	return NewShaper(testFonts(t).faces)
}

// runText concatenates the text a run's glyphs claim to represent, which is
// what a PDF reader reassembles when the document is copied or searched.
func runText(r ShapedRun) string {
	var b strings.Builder
	for _, g := range r.Glyphs {
		b.WriteString(g.Text)
	}
	return b.String()
}

// Every rune of the source must survive into some glyph's Text. Losing one
// leaves a hole in the ToUnicode map, which is what broke copy/paste on Hebrew
// calendars: mark glyphs were given empty destinations.
func TestEveryRuneSurvivesShaping(t *testing.T) {
	sh := testShaper(t)
	cases := []struct{ font, text string }{
		{FontPlain, "Candle lighting: 6:56pm"},
		{FontPlain, "Parashat Vayakhel-Pekudei"},
		{FontHebrew, "פרשת ויחי"},
		{FontHebrew, "פָּרָשַׁת וַיְחִי"},   // with niqud
		{FontHebrew, "רֹאשׁ חֹדֶשׁ שְׁבָט"}, // several marked clusters
	}
	for _, c := range cases {
		var got string
		for _, r := range sh.Shape(c.font, 12, c.text) {
			got += runText(r)
		}
		if countRunes(got) != countRunes(c.text) {
			t.Errorf("%q: recovered %d runes from glyph text, want %d (got %q)",
				c.text, countRunes(got), countRunes(c.text), got)
		}
	}
}

func countRunes(s string) int { return len([]rune(s)) }

// No glyph may carry an empty ToUnicode destination when its cluster has text
// to give: an empty <> in the CMap is meaningless and extractors disagree
// about it, some repeating the base letter instead.
func TestNiqudClustersDistributeTheirText(t *testing.T) {
	sh := testShaper(t)
	runs := sh.Shape(FontHebrew, 12, "פָּרָשַׁת")
	var withText, total int
	for _, r := range runs {
		for _, g := range r.Glyphs {
			total++
			if g.Text != "" {
				withText++
			}
		}
	}
	if total == 0 {
		t.Fatal("no glyphs produced")
	}
	if withText != total {
		t.Errorf("%d of %d glyphs carry no text; every drawn glyph needs a ToUnicode destination",
			total-withText, total)
	}
}

// A right-to-left paragraph displays with its trailing number on the left, so
// the runs must come back in visual order. Returning them logically rendered
// "ינואר 2027" as "2027ינואר" with the year on the wrong side.
func TestRTLParagraphRunsAreInVisualOrder(t *testing.T) {
	sh := testShaper(t)
	runs := sh.Shape(FontHebrew, 14, "ינואר 2027")
	if len(runs) < 2 {
		t.Fatalf("expected the number to form its own run, got %d run(s)", len(runs))
	}
	if runs[0].RTL {
		t.Errorf("first run should be the left-to-right year, got an RTL run %q", runText(runs[0]))
	}
	if !runs[len(runs)-1].RTL {
		t.Errorf("last run should be the Hebrew month name, got %q", runText(runs[len(runs)-1]))
	}
}

// A purely left-to-right string is one run and keeps its order.
func TestLTRParagraphIsASingleRun(t *testing.T) {
	sh := testShaper(t)
	runs := sh.Shape(FontPlain, 10, "Parashat Eikev")
	if len(runs) != 1 || runs[0].RTL {
		t.Fatalf("got %d run(s), first RTL=%v; want one left-to-right run", len(runs), runs[0].RTL)
	}
	if got := runText(runs[0]); got != "Parashat Eikev" {
		t.Errorf("run text = %q", got)
	}
}

// Width must equal the pen movement the glyphs actually produce, including the
// leading Skip and any mark offsets folded into neighbouring advances. It
// drives centring, right-alignment and the font-shrinking loop, so an
// inconsistent value misplaces text without any other symptom.
func TestWidthEqualsSumOfAdvances(t *testing.T) {
	sh := testShaper(t)
	for _, c := range []struct{ font, text string }{
		{FontPlain, "Candle lighting: 6:56pm"},
		{FontHebrew, "פָּרָשַׁת וַיְחִי"},
		{FontHebrew, "ינואר 2027"},
	} {
		for _, r := range sh.Shape(c.font, 12, c.text) {
			sum := r.Skip
			for _, g := range r.Glyphs {
				sum += g.Advance
			}
			if diff := sum - r.Width; diff > 0.0001 || diff < -0.0001 {
				t.Errorf("%q: Width=%.4f but advances sum to %.4f", c.text, r.Width, sum)
			}
		}
	}
}

// Widening the space advance in right-to-left runs reproduces the two-space
// word gaps that hebcal-web's reverseHebrewWords() leaves in published
// calendars, without putting a second space in the text.
func TestRTLWordGapsAreWidened(t *testing.T) {
	sh := testShaper(t)
	ltr := sh.Width(FontPlain, 12, "a a") - sh.Width(FontPlain, 12, "aa")
	rtl := sh.Width(FontHebrew, 12, "א א") - sh.Width(FontHebrew, 12, "אא")
	if rtl <= ltr {
		t.Errorf("right-to-left space = %.3f, left-to-right = %.3f; RTL should be wider", rtl, ltr)
	}
	// The text still contains exactly one space.
	var got string
	for _, r := range sh.Shape(FontHebrew, 12, "א א") {
		got += runText(r)
	}
	if strings.Count(got, " ") != 1 {
		t.Errorf("recovered text %q should contain exactly one space", got)
	}
}

// Shaping is used from several goroutines, and HarfbuzzShaper is not safe for
// concurrent use, so the pool has to hand out one per caller.
func TestShaperIsSafeForConcurrentUse(t *testing.T) {
	sh := testShaper(t)
	want := sh.Width(FontHebrew, 12, "פָּרָשַׁת וַיְחִי")
	done := make(chan float64, 8)
	for i := 0; i < 8; i++ {
		go func() { done <- sh.Width(FontHebrew, 12, "פָּרָשַׁת וַיְחִי") }()
	}
	for i := 0; i < 8; i++ {
		if got := <-done; got != want {
			t.Errorf("concurrent Width() = %v, want %v", got, want)
		}
	}
}

func TestShapeHandlesEmptyAndUnknownFont(t *testing.T) {
	sh := testShaper(t)
	if runs := sh.Shape(FontPlain, 10, ""); len(runs) != 0 {
		t.Errorf("empty string produced %d run(s)", len(runs))
	}
	if runs := sh.Shape("no-such-font", 10, "x"); len(runs) != 0 {
		t.Errorf("unknown font produced %d run(s)", len(runs))
	}
	if w := sh.Width("no-such-font", 10, "x"); w != 0 {
		t.Errorf("unknown font width = %v, want 0", w)
	}
}
