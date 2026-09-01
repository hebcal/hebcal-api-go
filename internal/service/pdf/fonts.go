package pdf

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	gotextfont "github.com/go-text/typesetting/font"
	// Aliased because this package is itself named pdf; pdflib is seehuhn's
	// PDF writer, not anything in here.
	pdflib "seehuhn.de/go/pdf"
	"seehuhn.de/go/pdf/font"
	"seehuhn.de/go/pdf/font/cff"
	"seehuhn.de/go/pdf/font/truetype"
	"seehuhn.de/go/sfnt"
)

// Font names match the names hebcal-web registers in src/pdf.js, so the
// rendering code reads the same way in both implementations.
const (
	FontPlain      = "plain"
	FontSemi       = "semi"
	FontBold       = "bold"
	FontHebrew     = "hebrew"
	FontHebrewBold = "hebrew-bold"
)

// fontFiles mirrors FONT_FILES from hebcal-web. The Source Sans Pro faces are
// TrueType (glyf outlines); the Adobe Hebrew faces are OpenType/CFF, which is
// why they are embedded through a different package below.
var fontFiles = map[string]string{
	FontPlain:      "Source_Sans_Pro/SourceSansPro-Regular.ttf",
	FontSemi:       "Source_Sans_Pro/SourceSansPro-SemiBold.ttf",
	FontBold:       "Source_Sans_Pro/SourceSansPro-Bold.ttf",
	FontHebrew:     "Adobe_Hebrew/adobehebrew-regular.otf",
	FontHebrewBold: "Adobe_Hebrew/adobehebrew-bold.otf",
}

// FontSet holds the parsed fonts for the process.
//
// Parsing is done once at startup and the *sfnt.Font values are then shared by
// every request. That is safe here in a way it was not in the Node
// implementation: sfnt.Font is read-only after parsing, whereas pdfkit's
// EmbeddedFont accumulates per-document subset state. hebcal-web tried sharing
// at the pdfkit layer and had to revert it after the workers were OOM-killed --
// see the PDF section of its CLAUDE.md. Each request still builds its own
// embedded font instances (Instances below), which are per-document by design.
type FontSet struct {
	// parsed drives PDF embedding and subsetting (seehuhn).
	parsed map[string]*sfnt.Font
	// faces drive shaping (go-text/HarfBuzz). Both are parsed from the same
	// files, so glyph IDs from a face are valid indices into the matching
	// embedded font.
	faces map[string]*gotextfont.Face
	// hheaAscent holds the hhea ascender per font, which is what pdfkit uses
	// to place a baseline. See Ascent.
	hheaAscent map[string]float64
}

// LoadFonts parses every font once. dir is the directory holding the
// Source_Sans_Pro and Adobe_Hebrew subdirectories.
func LoadFonts(dir string) (*FontSet, error) {
	fs := &FontSet{
		parsed:     make(map[string]*sfnt.Font, len(fontFiles)),
		faces:      make(map[string]*gotextfont.Face, len(fontFiles)),
		hheaAscent: make(map[string]float64, len(fontFiles)),
	}
	for name, rel := range fontFiles {
		path := filepath.Join(dir, rel)
		f, err := sfnt.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("loading font %s (%s): %w", name, rel, err)
		}
		fs.parsed[name] = f

		fh, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening font %s: %w", name, err)
		}
		face, err := gotextfont.ParseTTF(fh)
		fh.Close()
		if err != nil {
			return nil, fmt.Errorf("shaping face %s (%s): %w", name, rel, err)
		}
		fs.faces[name] = face

		if asc, err := readHheaAscent(path); err == nil {
			fs.hheaAscent[name] = asc
		}
	}
	return fs, nil
}

// Instances holds the embedded-font instances for a single PDF document.
//
// These must not be shared between documents: each one accumulates the subset
// of glyphs actually used, and that subset belongs to one document's content.
type Instances struct {
	byName map[string]font.Layouter
	// shapes memoises shaping results for the life of one document. A calendar
	// shapes the same (font, size, string) many times over -- the day numbers,
	// the weekday headings, a repeated "Candle lighting" subject, and, within a
	// single event, the measuring pass and the drawing pass -- so ~83% of the
	// Shape calls in a typical render repeat one of a few hundred distinct keys.
	// Caching here rather than on the shared Renderer keeps it per-document:
	// bounded by one calendar's content, freed when the document is, and free of
	// the cross-goroutine locking a process-wide cache on the hot path would
	// need. The cached runs are only ever read (widths summed, glyphs shown),
	// never mutated, so sharing one slice between draws is safe.
	shapes map[shapeKey][]ShapedRun
}

// shapeKey identifies a shaping request within one document.
type shapeKey struct {
	font string
	size float64
	str  string
}

// Embed builds a fresh set of embedded instances for one document.
func (fs *FontSet) Embed() (*Instances, error) {
	in := &Instances{
		byName: make(map[string]font.Layouter, len(fs.parsed)),
		shapes: make(map[shapeKey][]ShapedRun),
	}
	for name, parsed := range fs.parsed {
		var (
			l   font.Layouter
			err error
		)
		// Composite (Type0/CID) rather than simple fonts, which is how pdfkit
		// embeds them. A simple font addresses at most 256 glyphs through a
		// custom encoding, and viewers differ in how they treat one whose
		// encoding is not a standard base: Chrome's PDFium rendered the Hebrew
		// noticeably heavier than macOS Preview did from the same file.
		// Composite fonts sidestep that and lift the 256-glyph ceiling.
		if parsed.Outlines != nil && parsed.AsCFF() != nil {
			l, err = cff.NewComposite(parsed, &cff.OptionsComposite{MakeEncoder: makeIdentityEncoder})
		} else {
			l, err = truetype.NewComposite(parsed, &truetype.OptionsComposite{MakeEncoder: makeIdentityEncoder})
		}
		if err != nil {
			return nil, fmt.Errorf("embedding font %s: %w", name, err)
		}
		in.byName[name] = l
	}
	return in, nil
}

// Get returns the embedded instance for a registered font name.
func (in *Instances) Get(name string) font.Layouter {
	return in.byName[name]
}

// Ascent returns the font's ascender in PDF text-space units at the given size.
//
// pdfkit's doc.text(str, x, y) treats y as the top of the text box and puts the
// baseline one ascender below it, while PDF coordinates run bottom-up from the
// page edge. Converting hebcal-web's layout constants therefore needs the
// ascender, and specifically the one fontkit reports, which is the hhea
// table's. sfnt.Font.Ascent exposes the OS/2 typographic ascender instead, and
// the two disagree sharply for Source Sans Pro -- 984 against 750 -- which put
// every day number about 3.3pt too high in its cell. They happen to agree for
// Adobe Hebrew, which is why only the Latin faces looked wrong.
func (fs *FontSet) Ascent(name string, size float64) float64 {
	f := fs.parsed[name]
	if f == nil || f.UnitsPerEm == 0 {
		return size * 0.75
	}
	asc, ok := fs.hheaAscent[name]
	if !ok {
		asc = float64(f.Ascent)
	}
	return asc / float64(f.UnitsPerEm) * size
}

// readHheaAscent returns the ascender from a font file's hhea table, which is
// the value fontkit (and therefore pdfkit) reports as the font ascender.
func readHheaAscent(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) < 12 {
		return 0, fmt.Errorf("%s: too short for an sfnt header", path)
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	for i := 0; i < numTables; i++ {
		rec := 12 + i*16
		if rec+16 > len(data) {
			break
		}
		if string(data[rec:rec+4]) != "hhea" {
			continue
		}
		off := int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		// hhea: version (4 bytes) then ascender as an int16.
		if off+6 > len(data) {
			break
		}
		return float64(int16(binary.BigEndian.Uint16(data[off+4 : off+6]))), nil
	}
	return 0, fmt.Errorf("%s: no hhea table", path)
}

// pdfVersion matches the pdfVersion pdfkit is configured with in hebcal-web's
// createPdfDoc, so viewers see the same declared feature level.
const pdfVersion = pdflib.V1_5
