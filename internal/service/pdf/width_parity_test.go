package pdf

import "testing"

// Hebrew string widths must match what pdfkit draws, because the calendar
// right-aligns Hebrew event text inside its cell: a width that is off by a
// space moves the whole line.
//
// The reference values are pdfkit's widthOfString() for the string it actually
// draws, which is the output of reverseHebrewWords() -- that function rejoins
// words with two spaces, so a published calendar carries wider word gaps than
// a single space would give. This renderer reproduces the gap by widening the
// space glyph's advance instead of inserting a second space, so the text stays
// one space wide for copy/paste while the layout matches.
func TestHebrewWidthsMatchPdfkit(t *testing.T) {
	sh := testShaper(t)
	const tol = 0.01
	cases := []struct {
		name string
		s    string
		want float64
	}{
		{"two words", "פרשת ויחי", 43.188},
		{"three words", "ראש חודש שבט", 68.112},
		{"Hebrew then a number", "ינואר 2027", 49.620},
		// Pointing does not change this one's width, but does narrow the next,
		// so both directions are covered.
		{"two words, pointed", "פָּרָשַׁת וַיְחִי", 43.188},
		{"three words, pointed", "רֹאשׁ חֹדֶשׁ שְׁבָט", 65.232},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sh.Width(FontHebrew, 12, c.s)
			if d := got - c.want; d > tol || d < -tol {
				t.Errorf("Width(%q) = %.3f, want %.3f (pdfkit) within %.2f", c.s, got, c.want, tol)
			}
		})
	}
}

// The Latin faces carry most of the text, and the bold face positions every
// event subject that follows a time, so these have to agree closely.
//
// They only do because runs are shaped at a reference size and scaled:
// shaping directly at 8.5pt quantises advances to the pixel grid and comes out
// about 5.8% wide.
func TestLatinWidthsMatchPdfkit(t *testing.T) {
	sh := testShaper(t)
	const tol = 0.01
	cases := []struct {
		font string
		size float64
		s    string
		want float64
	}{
		{FontPlain, 10, "Candle lighting: 6:56pm", 98.92},
		{FontPlain, 10, "Parashat Vayakhel-Pekudei", 112.61},
		{FontSemi, 10, "Rosh Chodesh Adar II", 90.34},
		{FontBold, 8.5, "7:12pm ", 30.0815},
		{FontBold, 8.5, "8:51p ", 22.7630},
		{FontBold, 8.5, " ", 1.7000},
		{FontSemi, 26, "August 2026", 135.46},
	}
	for _, c := range cases {
		got := sh.Width(c.font, c.size, c.s)
		if d := got - c.want; d > tol || d < -tol {
			t.Errorf("Width(%s, %v, %q) = %.4f, want %.4f", c.font, c.size, c.s, got, c.want)
		}
	}
}
