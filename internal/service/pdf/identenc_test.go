package pdf

import (
	"reflect"
	"testing"

	"seehuhn.de/go/postscript/cid"

	"seehuhn.de/go/pdf"
	"seehuhn.de/go/pdf/font"
	"seehuhn.de/go/pdf/font/encoding/cidenc"
)

// TestIdentityEncoderMatchesLibrary asserts the arithmetic identity encoder
// behaves identically to the library's map-backed one for every operation the
// renderer performs: encode a CID with text and width, get its code back, read
// its width, and decode a produced string. If these ever diverge the embedded
// font would map glyphs or extract text differently, so this is the guard for
// the profiling optimisation in identenc.go.
func TestIdentityEncoderMatchesLibrary(t *testing.T) {
	const cid0Width = 500.0
	ref := cidenc.NewCompositeIdentity(cid0Width, font.Horizontal)
	got := makeIdentityEncoder(cid0Width, font.Horizontal)

	if _, ok := got.(*identityEncoder); !ok {
		t.Fatalf("makeIdentityEncoder returned %T, want *identityEncoder", got)
	}
	if got.WritingMode() != ref.WritingMode() {
		t.Errorf("WritingMode = %v, want %v", got.WritingMode(), ref.WritingMode())
	}

	// A spread of CIDs including both bytes non-zero, so a byte-swap error would
	// show, and 0x20 which triggers UseWordSpacing.
	cids := []cid.CID{0, 1, 3, 0x20, 0x41, 0xFF, 0x100, 0x1234, 0xABCD, 0xFFFF}
	for _, c := range cids {
		text := "x" + string(rune('A'+int(c%26)))
		width := float64(int(c)%900 + 100)

		rc, rerr := ref.Encode(c, text, width)
		gc, gerr := got.Encode(c, text, width)
		if (rerr == nil) != (gerr == nil) {
			t.Fatalf("cid %d: Encode err mismatch: ref=%v got=%v", c, rerr, gerr)
		}
		if rc != gc {
			t.Errorf("cid %d: code = %d, want %d", c, gc, rc)
		}

		rGot, rOK := ref.GetCode(c, text)
		gGot, gOK := got.GetCode(c, text)
		if rGot != gGot || rOK != gOK {
			t.Errorf("cid %d: GetCode = (%d,%v), want (%d,%v)", c, gGot, gOK, rGot, rOK)
		}
		if got.Width(gc) != ref.Width(rc) {
			t.Errorf("cid %d: Width(%d) = %v, want %v", c, gc, got.Width(gc), ref.Width(rc))
		}
	}

	// CIDs past the code space must be rejected the same way.
	if _, err := got.Encode(0x10000, "z", 100); err == nil {
		t.Error("Encode of out-of-range CID should fail")
	}

	// Decode a string built from several codes and compare the yielded
	// font.Code sequence.
	var s pdf.String
	for _, c := range cids {
		code, _ := got.GetCode(c, "")
		s = got.Codec().AppendCode(s, code)
	}
	rCodes := collect(ref.Codes(s))
	gCodes := collect(got.Codes(s))
	if len(rCodes) != len(gCodes) {
		t.Fatalf("Codes yielded %d, want %d", len(gCodes), len(rCodes))
	}
	for i := range rCodes {
		if rCodes[i] != gCodes[i] {
			t.Errorf("Codes[%d] = %+v, want %+v", i, gCodes[i], rCodes[i])
		}
	}

	// ToUnicode CMaps are built from the same recorded text, so they must be
	// structurally equal.
	if !reflect.DeepEqual(got.ToUnicode(), ref.ToUnicode()) {
		t.Error("ToUnicode CMaps differ")
	}
}

func collect(seq func(yield func(font.Code) bool)) []font.Code {
	var out []font.Code
	seq(func(c font.Code) bool { out = append(out, c); return true })
	return out
}
