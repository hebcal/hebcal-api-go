package icalendar

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// foldWidth is the most octets of content a physical line carries. RFC 5545
// section 3.1 allows 75 octets excluding the line break; a continuation line
// spends one of them on its leading space, so content is cut at 74 and every
// physical line -- first or continuation -- stays within the limit.
const foldWidth = 74

// foldSep ends one physical line and opens its continuation.
const foldSep = "\r\n "

// FoldLine folds a content line longer than 74 octets into several physical
// lines joined by CRLF and a space, as RFC 5545 section 3.1 describes.
//
// The limit is counted in octets of UTF-8, but a fold never lands inside a
// character: not inside a multi-octet sequence, and not inside a grapheme
// cluster either, so a combining mark stays with its base letter, a ZWJ emoji
// sequence or a flag stays whole, and a CR stays with its LF. The one
// exception is a single cluster wider than a line, which has to be cut
// somewhere and is cut between code points.
func FoldLine(line string) string {
	if len(line) <= foldWidth {
		return line
	}
	var b strings.Builder
	b.Grow(len(line) + (len(line)/foldWidth+1)*len(foldSep))
	writeFolded(&b, line)
	return b.String()
}

// stringWriter is the part of strings.Builder and bytes.Buffer folding needs.
type stringWriter interface {
	WriteString(string) (int, error)
}

// writeFolded writes line to w, folded.
func writeFolded(w stringWriter, line string) {
	if len(line) <= foldWidth {
		w.WriteString(line)
		return
	}
	if isASCII(line) {
		// Every octet is a character, so the cut points are fixed -- except
		// that a CRLF pair is one grapheme cluster and is never separated.
		for len(line) > foldWidth {
			cut := foldWidth
			if line[cut-1] == '\r' && line[cut] == '\n' {
				cut--
			}
			w.WriteString(line[:cut])
			w.WriteString(foldSep)
			line = line[cut:]
		}
		w.WriteString(line)
		return
	}

	chunkStart := 0 // where the physical line being filled begins
	octets := 0     // how much of it is filled
	for i := 0; i < len(line); {
		size := 1
		if line[i] >= utf8.RuneSelf {
			_, size = utf8.DecodeRuneInString(line[i:])
		}
		if octets+size > foldWidth {
			brk := i
			if i > chunkStart {
				// Back up to the start of the cluster the cut would split,
				// unless that cluster is the whole line so far.
				// Nor when the cluster would not fit on the next line either:
				// one that wide is cut where it crosses the limit.
				if start := clusterStart(line, chunkStart, i); start > chunkStart && i+size-start <= foldWidth {
					brk = start
				}
			}
			w.WriteString(line[chunkStart:brk])
			w.WriteString(foldSep)
			chunkStart = brk
			// Whatever was backed over opens the continuation line.
			octets = i + size - brk
		} else {
			octets += size
		}
		i += size
	}
	w.WriteString(line[chunkStart:])
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// clusterStart returns the offset at which the grapheme cluster containing
// offset i begins, given that a cluster boundary lies at from (from < i).
//
// Only the candidate break offsets are ever asked about, one per physical
// line, and the common case is settled without segmenting anything: between
// two ASCII characters there is always a boundary, unless they are a CR and
// its LF.
func clusterStart(line string, from, i int) int {
	if prev, cur := line[i-1], line[i]; prev < utf8.RuneSelf && cur < utf8.RuneSelf {
		if prev == '\r' && cur == '\n' {
			return i - 1
		}
		return i
	}
	state := -1
	rest := line[from:]
	pos := from
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if pos+len(cluster) > i {
			return pos
		}
		pos += len(cluster)
	}
	return i
}
