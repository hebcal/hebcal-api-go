package pdf

import (
	"sync"
	"unicode"

	"github.com/go-text/typesetting/di"
	gotextfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/unicode/bidi"
	pdffont "seehuhn.de/go/pdf/font"
	"seehuhn.de/go/sfnt/glyph"
)

// ShapedRun is one directional run of text, already shaped into glyphs.
//
// A single calendar string can produce several of these: "Candle lighting:
// 6:56pm" next to a Hebrew holiday name is two runs with opposite directions.
// They are returned in visual order, left to right, so a caller can draw them
// one after another without knowing anything about bidi.
type ShapedRun struct {
	// Glyphs are the shaped glyphs in visual order.
	Glyphs []pdffont.Glyph
	// Skip is a leading horizontal offset, used when the first glyph of a run
	// is a combining mark that HarfBuzz placed off the pen position.
	Skip float64
	// Width is the total advance of the run in PDF text-space units.
	Width float64
	// RTL reports whether this run was laid out right to left.
	RTL bool
}

// Shaper turns strings into positioned glyphs.
//
// This replaces the reverseHebrewWords() hack in hebcal-web's src/pdf.js, which
// reversed word order by hand and patched up parentheses and trailing commas
// because pdfkit offered no bidi support at all. Here the Unicode
// bidirectional algorithm (UAX #9) decides visual order and HarfBuzz does the
// shaping, so mixed Hebrew/Latin strings, nikud and presentation forms come out
// correct without any per-string special-casing.
type Shaper struct {
	faces map[string]*gotextfont.Face

	// HarfbuzzShaper carries an internal font cache and is not safe for
	// concurrent use, so each goroutine borrows one. The faces above are
	// read-only after construction and are shared.
	pool sync.Pool
}

// NewShaper builds a Shaper over the already-parsed faces.
func NewShaper(faces map[string]*gotextfont.Face) *Shaper {
	s := &Shaper{faces: faces}
	s.pool.New = func() any {
		hb := &shaping.HarfbuzzShaper{}
		// Bounded so a long-lived process cannot accumulate shaping caches the
		// way hebcal-web's reverted pdfkit cache accumulated parsed fonts.
		hb.SetFontCacheSize(len(faces))
		return hb
	}
	return s
}

// shapeRefSize is the point size every run is shaped at before being scaled to
// the size actually drawn. See shapeRun.
const shapeRefSize = 1000.0

// hebrewLang is passed to HarfBuzz so it can apply Hebrew-specific features.
var hebrewLang = language.NewLanguage("he")

// Shape lays out s in the named font at the given size and returns the runs in
// visual order.
func (s *Shaper) Shape(fontName string, size float64, str string) []ShapedRun {
	face := s.faces[fontName]
	if face == nil || str == "" {
		return nil
	}

	var p bidi.Paragraph
	if _, err := p.SetString(str); err != nil {
		// A string bidi cannot parse is still worth drawing; treat it as a
		// single left-to-right run rather than dropping the event.
		return []ShapedRun{s.shapeRun(face, size, str, false)}
	}
	ord, err := p.Order()
	if err != nil || ord.NumRuns() == 0 {
		return []ShapedRun{s.shapeRun(face, size, str, false)}
	}

	runs := make([]ShapedRun, 0, ord.NumRuns())
	for i := 0; i < ord.NumRuns(); i++ {
		r := ord.Run(i)
		rtl := r.Direction() == bidi.RightToLeft
		runs = append(runs, s.shapeRun(face, size, r.String(), rtl))
	}
	// Order() yields runs in logical order. Callers draw them left to right,
	// so a right-to-left paragraph needs them reversed: "ינואר 2027" is stored
	// with the month first but displays with the year on the left.
	if p.Direction() == bidi.RightToLeft {
		for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
			runs[i], runs[j] = runs[j], runs[i]
		}
	}
	return runs
}

// Width returns the total advance of s, the equivalent of pdfkit's
// widthOfString(). The layout decisions in the renderer -- the font-shrinking
// loop and the two-line fallback -- branch on this.
func (s *Shaper) Width(fontName string, size float64, str string) float64 {
	var w float64
	for _, r := range s.Shape(fontName, size, str) {
		w += r.Width
	}
	return w
}

// shapeRun shapes one single-direction run.
func (s *Shaper) shapeRun(face *gotextfont.Face, size float64, str string, rtl bool) ShapedRun {
	dir := di.DirectionLTR
	if rtl {
		dir = di.DirectionRTL
	}
	text := []rune(str)

	// Shape at a large reference size and scale the result, rather than
	// shaping at the size being drawn.
	//
	// HarfBuzz quantises advances to the pixel grid at the ppem it is given.
	// At the sizes a calendar uses that is a visible error -- an 8.5pt space
	// came out 1.797pt instead of 1.700, about 5.8% wide, and the error
	// compounds across a string. fontkit scales linearly from font units, so
	// pdfkit's widths are the unrounded ones. Shaping at 1000pt and scaling
	// brings the quantisation below a thousandth of a point.
	hb := s.pool.Get().(*shaping.HarfbuzzShaper)
	out := hb.Shape(shaping.Input{
		Text:      text,
		RunStart:  0,
		RunEnd:    len(text),
		Direction: dir,
		Face:      face,
		Size:      floatToFixed(shapeRefSize),
		Script:    language.LookupScript(firstRune(text)),
		Language:  hebrewLang,
	})
	s.pool.Put(hb)
	scale := size / shapeRefSize

	run := ShapedRun{RTL: rtl, Glyphs: make([]pdffont.Glyph, 0, len(out.Glyphs))}
	// hebcal-web's reverseHebrewWords() rejoins right-to-left text with two
	// spaces between words, and pdfkit then hands that string to fontkit,
	// which lays Hebrew out right-to-left and reverses it again. The visible
	// result is one space between the words and the second one accumulated at
	// the run's leading edge: "הדלקת נרות" draws 46.92pt of ink starting
	// 2.83pt in, inside a 49.75pt advance.
	//
	// Reproducing that means keeping the spaces single and adding their extra
	// width to Skip, rather than widening each space in place. Widening in
	// place gives the same total advance -- so the line still right-aligns
	// correctly -- but visibly too much air between the words and too little
	// between the text and the time that precedes it.
	var rtlExtraLead float64
	for i := 0; i < len(out.Glyphs); {
		cluster := out.Glyphs[i].ClusterIndex
		j := i
		for j < len(out.Glyphs) && out.Glyphs[j].ClusterIndex == cluster {
			j++
		}
		for k, txt := range assignClusterText(text, out.Glyphs[i:j]) {
			g := out.Glyphs[i+k]
			adv := fixedToFloat(g.Advance) * scale
			if rtl && txt == " " {
				rtlExtraLead += adv
			}
			// HarfBuzz positions a combining mark relative to the pen with an
			// (XOffset, YOffset). The vertical part maps straight onto Rise,
			// but a PDF glyph sequence has no horizontal equivalent, so the
			// shift is expressed through the advances around it: move the pen
			// forward by XOffset before the mark and back afterwards. Dropping
			// it leaves every niqud sitting to the left of its letter.
			dx := fixedToFloat(g.XOffset) * scale
			if dx != 0 {
				if n := len(run.Glyphs); n > 0 {
					run.Glyphs[n-1].Advance += dx
				} else {
					run.Skip += dx
				}
				adv -= dx
			}
			run.Glyphs = append(run.Glyphs, pdffont.Glyph{
				GID:     glyph.ID(g.GlyphID),
				Advance: adv,
				Rise:    fixedToFloat(g.YOffset) * scale,
				Text:    txt,
			})
		}
		i = j
	}
	// Total pen movement, summed after the mark offsets above have been folded
	// into the surrounding advances. Accumulating it in the loop would miss the
	// adjustment made to an already-emitted glyph, and the width drives
	// centring, right-alignment and the font-shrinking loop.
	run.Skip += rtlExtraLead
	run.Width = run.Skip
	for _, g := range run.Glyphs {
		run.Width += g.Advance
	}
	return run
}

// assignClusterText divides one cluster's source runes among its glyphs.
//
// A Hebrew letter and its nikud form a single cluster that shapes to several
// glyphs: one advancing base and one or more zero-advance combining marks.
// Every glyph drawn gets a character code in the PDF, and every code needs a
// real ToUnicode destination -- an empty one (`<>` in the CMap) is what broke
// copy/paste and search, because extractors treat it inconsistently and some
// fall back to repeating the base letter.
//
// The split follows the same distinction the shaper used: base runes (not
// combining marks) go to the advancing glyph, and combining marks go to the
// zero-advance glyphs. Read back in logical order the pieces concatenate to
// the original text, which is what an extractor reassembles.
func assignClusterText(text []rune, glyphs []shaping.Glyph) []string {
	out := make([]string, len(glyphs))
	if len(glyphs) == 0 {
		return out
	}
	runes := clusterRunes(text, glyphs[0])
	if len(runes) == 0 {
		return out
	}
	if len(glyphs) == 1 {
		out[0] = string(runes)
		return out
	}

	var base, marks []rune
	for _, r := range runes {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
			marks = append(marks, r)
		} else {
			base = append(base, r)
		}
	}
	// Without a mark/base distinction there is nothing to divide, so keep the
	// cluster whole on its first glyph rather than inventing a split.
	if len(base) == 0 || len(marks) == 0 {
		out[0] = string(runes)
		return out
	}

	baseIdx := -1
	for i, g := range glyphs {
		if g.Advance != 0 {
			baseIdx = i
			break
		}
	}
	if baseIdx < 0 {
		out[0] = string(runes)
		return out
	}
	out[baseIdx] = string(base)

	// Hand the marks to the zero-advance glyphs, in order. If there are more
	// marks than mark glyphs the last one takes the remainder, so no rune is
	// ever dropped from the CMap.
	markIdx := 0
	for i := range glyphs {
		if i == baseIdx || markIdx >= len(marks) {
			continue
		}
		last := true
		for j := i + 1; j < len(glyphs); j++ {
			if j != baseIdx {
				last = false
				break
			}
		}
		if last {
			out[i] = string(marks[markIdx:])
			markIdx = len(marks)
		} else {
			out[i] = string(marks[markIdx])
			markIdx++
		}
	}
	return out
}

// clusterRunes returns the source runes a shaped cluster came from.
func clusterRunes(text []rune, g shaping.Glyph) []rune {
	start := g.ClusterIndex
	if start < 0 || start >= len(text) {
		return nil
	}
	end := start + g.RuneCount
	if g.RuneCount <= 0 {
		end = start + 1
	}
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

// firstRune returns the first rune of a run, used to pick the script.
func firstRune(text []rune) rune {
	if len(text) == 0 {
		return ' '
	}
	return text[0]
}

// floatToFixed converts points to the 26.6 fixed-point format go-text uses.
func floatToFixed(v float64) fixed.Int26_6 {
	return fixed.Int26_6(v * 64)
}

// fixedToFloat converts 26.6 fixed point back to points.
func fixedToFloat(v fixed.Int26_6) float64 {
	return float64(v) / 64
}
