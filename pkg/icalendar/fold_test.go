package icalendar

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// checkFold verifies the properties every fold has to have: no physical line
// over 75 octets, no character cut in two, and the original back on unfolding.
func checkFold(t *testing.T, str string) string {
	t.Helper()
	folded := FoldLine(str)
	chunks := strings.Split(folded, "\r\n ")
	for i, chunk := range chunks {
		octets := len(chunk)
		if i > 0 {
			octets++ // the continuation's leading space
		}
		if octets > 75 {
			t.Errorf("physical line %d is %d octets", i, octets)
		}
		if !utf8.ValidString(chunk) {
			t.Errorf("physical line %d splits a UTF-8 sequence: %q", i, chunk)
		}
		if chunk == "" {
			t.Errorf("physical line %d is empty", i)
		}
	}
	if got := strings.Join(chunks, ""); got != str {
		t.Errorf("unfolding does not round-trip:\n got %q\nwant %q", got, str)
	}
	return folded
}

func TestFoldShort(t *testing.T) {
	for _, str := range []string{"", "SUMMARY:Rum Rebellion Day", "SUMMARY:Ĥéļļö Ŵöŕļđ", strings.Repeat("x", 74)} {
		if got := FoldLine(str); got != str {
			t.Errorf("FoldLine(%q) = %q", str, got)
		}
	}
}

func TestFoldASCIILong(t *testing.T) {
	str := "SUMMARY:" + strings.Repeat("Foo", 50)
	want := "SUMMARY:FooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFoo\r\n" +
		" FooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFooFo\r\n" +
		" oFooFooFoo"
	if got := checkFold(t, str); got != want {
		t.Errorf("got %q", got)
	}
	if got := FoldLine(strings.Repeat("x", 75)); got != strings.Repeat("x", 74)+"\r\n x" {
		t.Errorf("75 octets: %q", got)
	}
}

func TestFoldTagSequenceFlag(t *testing.T) {
	// The flag of Scotland is a black flag followed by six tag characters and
	// a cancel tag: seven code points, 28 octets, one grapheme cluster. The
	// second one would straddle the limit, so the whole cluster moves down.
	const scotland = "🏴\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"
	str := "SUMMARY:" + scotland + " Burns Night " + scotland
	want := "SUMMARY:" + scotland + " Burns Night \r\n " + scotland
	if got := checkFold(t, str); got != want {
		t.Errorf("got %q", got)
	}
}

func TestFoldHebrewLong(t *testing.T) {
	str := "SUMMARY:בְּרֵאשִׁ֖ית בָּרָ֣א אֱלֹקִ֑ים אֵ֥ת הַשָּׁמַ֖יִם וְאֵ֥ת הָאָֽרֶץ"
	want := "SUMMARY:בְּרֵאשִׁ֖ית בָּרָ֣א אֱלֹקִ֑ים אֵ֥\r\n ת הַשָּׁמַ֖יִם וְאֵ֥ת הָאָֽרֶץ"
	if got := checkFold(t, str); got != want {
		t.Errorf("got %q", got)
	}
}

func TestFoldTwoOctetCharacters(t *testing.T) {
	// 74 is even, so an odd prefix puts a two-octet character across the limit.
	for _, prefix := range []string{"SUMMARY:", "SUMMARY:x", ""} {
		checkFold(t, prefix+strings.Repeat("é", 120))
	}
}

func TestFoldRegionalIndicatorFlag(t *testing.T) {
	// 🇮🇱 is two regional-indicator code points that must stay together.
	folded := checkFold(t, "SUMMARY:Hello "+strings.Repeat("🇮🇱 Israel ", 20))
	if strings.Contains(folded, "🇮\r\n") || strings.Contains(folded, "\r\n 🇱") {
		t.Errorf("flag split: %q", folded)
	}
	// With nothing between them the pairing is by position in the run.
	folded = checkFold(t, "X:"+strings.Repeat("🇮🇱", 40))
	for _, chunk := range strings.Split(folded, "\r\n ") {
		if strings.HasPrefix(chunk, "🇱") {
			t.Errorf("flag split: %q", folded)
		}
	}
}

func TestFoldASCIIWithLineBreaks(t *testing.T) {
	checkFold(t, "DESCRIPTION:"+strings.Repeat("a", 70)+"\n"+strings.Repeat("b", 70))
}

func TestFoldKeepsCRLFTogether(t *testing.T) {
	// A CR and its LF are one grapheme cluster. (Round-tripping cannot be
	// checked when the input itself contains CRLF + space.)
	for _, str := range []string{
		strings.Repeat("\r\n", 80),
		strings.Repeat("a", 73) + "\r\n" + strings.Repeat("b", 80),
		"é" + strings.Repeat("a", 71) + "\r\n" + strings.Repeat("b", 80),
		strings.Replace("DESCRIPTION:"+strings.Repeat("Pharaoh’s dreams and rises to be viceroy. ", 4), "dreams", "dreams\r\n", 1),
	} {
		folded := FoldLine(str)
		if strings.Contains(folded, "\r\r\n \n") {
			t.Errorf("CRLF split: %q", folded)
		}
		for _, chunk := range strings.Split(folded, "\r\n ") {
			if len(chunk) > 75 {
				t.Errorf("%d octets", len(chunk))
			}
		}
		if strings.ReplaceAll(folded, "\r\n ", "") != strings.ReplaceAll(str, "\r\n ", "") {
			t.Errorf("content changed: %q", folded)
		}
	}
}

func TestFoldKeepsNikudWithItsLetter(t *testing.T) {
	// Hebrew points are combining marks; none may open a continuation line.
	for _, prefix := range []string{"DESCRIPTION:", "D:", "DESC:"} {
		folded := checkFold(t, prefix+strings.Repeat("בְּרֵאשִׁ֖ית בָּרָ֣א ", 6))
		for _, chunk := range strings.Split(folded, "\r\n ")[1:] {
			if r, _ := utf8.DecodeRuneInString(chunk); r >= 0x0591 && r <= 0x05C7 {
				t.Errorf("continuation begins with a combining mark: %q", chunk)
			}
		}
	}
}

func TestFoldZWJSequence(t *testing.T) {
	// 👨‍👩‍👧‍👦 is four emoji joined by ZWJ: one cluster, 25 octets.
	folded := checkFold(t, "SUMMARY:"+strings.Repeat("👨‍👩‍👧‍👦 family ", 8))
	if strings.Contains(folded, "‍\r\n ") || strings.Contains(folded, "\r\n ‍") {
		t.Errorf("broke at a ZWJ: %q", folded)
	}
}

func TestFoldEmojiWithVariationSelector(t *testing.T) {
	// The candle is U+1F56F and a variation selector, which must not be left
	// behind.
	for n := 60; n < 76; n++ {
		folded := checkFold(t, "SUMMARY:"+strings.Repeat("x", n)+"🕯️ Yahrzeit")
		if strings.Contains(folded, "\r\n ️") {
			t.Errorf("n=%d: variation selector orphaned: %q", n, folded)
		}
	}
}

func TestFoldClusterWiderThanALine(t *testing.T) {
	// One base letter and 200 combining marks is a single cluster far wider
	// than a line. It has to be cut somewhere; it is cut between code points,
	// and without an empty first line.
	folded := checkFold(t, "a"+strings.Repeat("֑", 200))
	if strings.HasPrefix(folded, "\r\n ") {
		t.Error("empty leading line")
	}
	checkFold(t, "SUMMARY:"+strings.Repeat("֑", 200)+" tail "+strings.Repeat("é", 80))
}

func TestFoldInvalidUTF8(t *testing.T) {
	str := "X:" + strings.Repeat("\xff\xfe", 60) + "é"
	folded := FoldLine(str)
	if got := strings.ReplaceAll(folded, "\r\n ", ""); got != str {
		t.Error("bytes lost")
	}
	for _, chunk := range strings.Split(folded, "\r\n ") {
		if len(chunk) > 74 {
			t.Errorf("%d octets", len(chunk))
		}
	}
}

func FuzzFoldLine(f *testing.F) {
	f.Add("SUMMARY:" + strings.Repeat("🇮🇱 Israel ", 20))
	f.Add("DESCRIPTION:" + strings.Repeat("בְּרֵאשִׁ֖ית בָּרָ֣א ", 6))
	f.Add("a" + strings.Repeat("֑", 200))
	f.Fuzz(func(t *testing.T, s string) {
		if strings.Contains(s, "\r\n ") {
			return
		}
		folded := FoldLine(s)
		chunks := strings.Split(folded, "\r\n ")
		if strings.Join(chunks, "") != s {
			t.Fatalf("round trip: %q", s)
		}
		for _, c := range chunks {
			if len(c) > 74 {
				t.Fatalf("%d octets in %q", len(c), s)
			}
			if utf8.ValidString(s) && !utf8.ValidString(c) {
				t.Fatalf("split a character in %q", s)
			}
		}
	})
}

func BenchmarkFoldLine(b *testing.B) {
	cases := map[string]string{
		"short":  "SUMMARY:🕯️ Ploni Almoni’s 15th Yahrzeit (8th of Adar)",
		"ascii":  "DESCRIPTION:" + strings.Repeat("Hebcal joins you in remembering Ploni Almoni. ", 10),
		"quotes": "DESCRIPTION:" + strings.Repeat("Ploni’s Yahrzeit begins at sundown on Tuesday. ", 10),
		"hebrew": "DESCRIPTION:" + strings.Repeat("בְּרֵאשִׁ֖ית בָּרָ֣א אֱלֹהִ֑ים ", 12),
	}
	for name, str := range cases {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(str)))
			var sb strings.Builder
			for i := 0; i < b.N; i++ {
				sb.Reset()
				writeFolded(&sb, str)
			}
		})
	}
}
