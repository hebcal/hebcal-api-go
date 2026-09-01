package pdf

import (
	"errors"
	"iter"

	"seehuhn.de/go/postscript/cid"

	"seehuhn.de/go/pdf"
	"seehuhn.de/go/pdf/font"
	"seehuhn.de/go/pdf/font/charcode"
	"seehuhn.de/go/pdf/font/cmap"
	"seehuhn.de/go/pdf/font/encoding/cidenc"
	"seehuhn.de/go/pdf/font/mapping"
)

// identityEncoder is a drop-in replacement for cidenc.NewCompositeIdentity that
// does not materialise the two 65536-entry code<->CID maps its factory builds
// on every call.
//
// seehuhn's default identity encoder (cidenc.NewFromCMap, reached through
// truetype/cff.NewComposite's MakeEncoder hook) walks the whole Identity-H code
// space and fills a map[CID]Code and a map[Code]CID with all 65536 entries,
// then never adds to them. Because Instances.Embed builds a fresh encoder per
// font per document, that rebuild dominated a render: profiling a Spanish
// full-holiday calendar put it at ~40% of the render's CPU and ~65% of its
// allocations, none of it document-specific.
//
// The Identity-H mapping is fixed and arithmetic, so those maps are avoidable
// entirely. cmap.All enumerates the single CID range 0x0000-0xFFFF big-endian
// (index i is bytes {i>>8, i&0xFF}), and charcode.Codec.Decode packs the first
// byte into the least-significant position, so the code a CID maps to is the
// CID with its two bytes swapped -- and the inverse is the same swap. swap16
// replaces both lookups. Everything document-specific (the per-code text and
// per-CID width, and the ToUnicode CMap built from them) is kept exactly as the
// library's encoder keeps it, and CMap/Codec return the same shared predefined
// Identity-H objects, so the embedded font is identical.
type identityEncoder struct {
	cmap  *cmap.File
	codec *charcode.Codec
	wMode font.WritingMode
	text  map[charcode.Code]string
	width map[cid.CID]float64
}

var _ cidenc.CIDEncoder = (*identityEncoder)(nil)

// swap16 exchanges the two low bytes of a 16-bit value, which is both the
// CID->code and the code->CID mapping for Identity-H (see the type doc).
func swap16(v uint32) uint32 { return ((v & 0xFF) << 8) | ((v >> 8) & 0xFF) }

// makeIdentityEncoder is the MakeEncoder hook passed to NewComposite. It matches
// cidenc.NewCompositeIdentity's signature and result for horizontal writing,
// which is the only mode this service uses; a vertical request falls back to
// the library so no behaviour is silently lost.
func makeIdentityEncoder(cid0Width float64, wMode font.WritingMode) cidenc.CIDEncoder {
	if wMode != font.Horizontal {
		return cidenc.NewCompositeIdentity(cid0Width, wMode)
	}
	// Both Predefined and its Codec are cheap and, in seehuhn, globally cached
	// and read-only, which is why sharing them across documents is safe -- the
	// library's own default encoder shares the same predefined File.
	file, err := cmap.Predefined("Identity-H")
	if err != nil {
		return cidenc.NewCompositeIdentity(cid0Width, wMode)
	}
	codec, err := file.Codec()
	if err != nil {
		return cidenc.NewCompositeIdentity(cid0Width, wMode)
	}
	return &identityEncoder{
		cmap:  file,
		codec: codec,
		wMode: wMode,
		text:  make(map[charcode.Code]string),
		width: map[cid.CID]float64{0: cid0Width},
	}
}

func (f *identityEncoder) WritingMode() font.WritingMode { return f.wMode }

func (f *identityEncoder) CMap(ros *cid.SystemInfo) *cmap.File { return f.cmap }

func (f *identityEncoder) Codec() *charcode.Codec { return f.codec }

func (f *identityEncoder) CodesRemaining() int { return 0 }

// Encode records a CID's text and width and returns its code, the port of
// cidenc.(*fixed).Encode with the map lookup replaced by swap16.
func (f *identityEncoder) Encode(cidVal cid.CID, text string, width float64) (charcode.Code, error) {
	if uint32(cidVal) > 0xFFFF {
		return 0, errors.New("CID not found in CMap")
	}
	code := charcode.Code(swap16(uint32(cidVal)))

	if existingWidth, hasWidth := f.width[cidVal]; hasWidth {
		if existingWidth != width {
			return 0, errors.New("width already set to different value")
		}
	} else {
		f.width[cidVal] = width
	}

	if existingText, hasText := f.text[code]; hasText {
		if existingText != text {
			return 0, errors.New("text already set to different value")
		}
	} else {
		f.text[code] = text
	}

	return code, nil
}

// GetCode mirrors cidenc.(*fixed).GetCode: a CID has a code only once its width
// has been recorded by Encode.
func (f *identityEncoder) GetCode(cidVal cid.CID, text string) (charcode.Code, bool) {
	if _, ok := f.width[cidVal]; !ok {
		return 0, false
	}
	return charcode.Code(swap16(uint32(cidVal))), true
}

// Width mirrors cidenc.(*fixed).Width: the width stored for the code's CID.
func (f *identityEncoder) Width(code charcode.Code) float64 {
	return f.width[cid.CID(swap16(uint32(code)))]
}

// Codes is the port of cidenc.(*fixed).Codes: decode the string and yield each
// code's CID, width and text, with the reverse lookup replaced by swap16.
func (f *identityEncoder) Codes(s pdf.String) iter.Seq[font.Code] {
	return func(yield func(font.Code) bool) {
		var code font.Code
		for len(s) > 0 {
			c, k, valid := f.codec.Decode(s)
			if !valid {
				k = 1
				c = 0
			}

			if valid {
				cidVal := cid.CID(swap16(uint32(c)))
				code = font.Code{
					CID:            cidVal,
					Width:          f.width[cidVal] / 1000,
					Text:           f.text[c],
					UseWordSpacing: k == 1 && c == 0x20,
				}
			} else {
				code = font.Code{
					CID:            0,
					Width:          f.width[0] / 1000,
					Text:           f.text[c],
					UseWordSpacing: k == 1 && c == 0x20,
				}
			}

			if !yield(code) {
				return
			}
			s = s[k:]
		}
	}
}

// MappedCodes is the port of cidenc.(*fixed).MappedCodes.
func (f *identityEncoder) MappedCodes() iter.Seq2[charcode.Code, *cidenc.Info] {
	return func(yield func(charcode.Code, *cidenc.Info) bool) {
		var info cidenc.Info
		for code, text := range f.text {
			cidVal := cid.CID(swap16(uint32(code)))
			info = cidenc.Info{
				CID:   cidVal,
				Width: f.width[cidVal],
				Text:  text,
			}
			if !yield(code, &info) {
				break
			}
		}
	}
}

// ToUnicode is the port of cidenc.(*fixed).ToUnicode: build a ToUnicode CMap
// from the recorded text, skipping codes whose text matches the CID's implied
// mapping. It uses the same shared cmap and codec, so the output is identical.
func (f *identityEncoder) ToUnicode() *cmap.ToUnicodeFile {
	m := make(map[charcode.Code]string)

	implied, _ := mapping.GetCIDTextMapping(f.cmap.ROS.Registry, f.cmap.ROS.Ordering)

	var buf []byte
	for code, text := range f.text {
		if text == "" {
			continue
		}
		buf = f.codec.AppendCode(buf[:0], code)
		cidVal := f.cmap.LookupCID(buf)
		if text == implied[cidVal] {
			continue
		}
		m[code] = text
	}

	if len(m) == 0 {
		return nil
	}
	toUnicode, _ := cmap.NewToUnicodeFile(f.cmap.CodeSpaceRange, m)
	return toUnicode
}
