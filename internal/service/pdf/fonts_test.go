package pdf

import (
	"path/filepath"
	"testing"
)

// Ascent must come from the hhea table, which is what fontkit reports and
// therefore what pdfkit uses to place a baseline. sfnt.Font.Ascent exposes the
// OS/2 typographic ascender instead, and for Source Sans Pro the two disagree
// by 234 units -- enough to sit every day number about 3.3pt high in its cell.
func TestAscentComesFromHhea(t *testing.T) {
	fs := testFonts(t)
	tests := []struct {
		font string
		want float64 // hhea ascender, in units per em
	}{
		{FontSemi, 984},
		{FontHebrew, 727},
	}
	for _, tt := range tests {
		f := fs.parsed[tt.font]
		if f == nil {
			t.Fatalf("%s not loaded", tt.font)
		}
		got := fs.Ascent(tt.font, float64(f.UnitsPerEm))
		if got != tt.want {
			t.Errorf("%s: Ascent at 1em = %v, want %v (the hhea ascender)", tt.font, got, tt.want)
		}
	}
	// The Latin faces are the ones where hhea and OS/2 disagree; if this ever
	// stops being true the bug it guards against has changed shape.
	semi := fs.parsed[FontSemi]
	if float64(semi.Ascent) == fs.Ascent(FontSemi, float64(semi.UnitsPerEm)) {
		t.Error("hhea and OS/2 ascenders now agree for Source Sans Pro; " +
			"this test no longer guards anything")
	}
}

func TestReadHheaAscent(t *testing.T) {
	fs := testFonts(t)
	_ = fs
	got, err := readHheaAscent(filepath.Join(fontDir, fontFiles[FontSemi]))
	if err != nil {
		t.Fatalf("readHheaAscent: %v", err)
	}
	if got != 984 {
		t.Errorf("readHheaAscent(SemiBold) = %v, want 984", got)
	}
	if _, err := readHheaAscent(filepath.Join(fontDir, "does-not-exist.ttf")); err == nil {
		t.Error("expected an error for a missing file")
	}
}

// Ascent falls back rather than dividing by zero when a font is unknown.
func TestAscentUnknownFont(t *testing.T) {
	fs := testFonts(t)
	if got := fs.Ascent("no-such-font", 14); got <= 0 {
		t.Errorf("Ascent(unknown) = %v, want a positive fallback", got)
	}
}

// Both font formats must load: the Source Sans Pro faces are TrueType and the
// Adobe Hebrew faces are OpenType/CFF, which are embedded through different
// code paths.
func TestBothFontFormatsLoadAndEmbed(t *testing.T) {
	fs := testFonts(t)
	for name := range fontFiles {
		if fs.parsed[name] == nil {
			t.Errorf("%s: not parsed", name)
		}
		if fs.faces[name] == nil {
			t.Errorf("%s: no shaping face", name)
		}
	}
	inst, err := fs.Embed()
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	for name := range fontFiles {
		if inst.Get(name) == nil {
			t.Errorf("%s: not embedded", name)
		}
	}
}

// Embedded instances accumulate the glyph subset used by one document, so each
// render needs its own. Sharing them across documents is what OOM-killed the
// Node service; see the PDF section of hebcal-web's CLAUDE.md.
func TestEmbedReturnsIndependentInstances(t *testing.T) {
	fs := testFonts(t)
	a, err := fs.Embed()
	if err != nil {
		t.Fatal(err)
	}
	b, err := fs.Embed()
	if err != nil {
		t.Fatal(err)
	}
	if a.Get(FontPlain) == b.Get(FontPlain) {
		t.Error("Embed() returned the same instance twice; subsets would leak between documents")
	}
}
