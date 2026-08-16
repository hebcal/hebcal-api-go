// Package jsutil holds the small JavaScript-compatibility helpers this service
// needs to reproduce hebcal-web's output byte for byte: JS parseInt and
// Date.toISOString semantics, the string munging @hebcal/core applies to event
// titles and anchors, JSON.stringify-compatible marshalling, and the
// "undefined"/falsy conventions the JS routes apply to query parameters.
package jsutil

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseInt mimics JavaScript parseInt(str, 10): leading whitespace and an
// optional sign are allowed, then as many decimal digits as possible are
// consumed ("2026abc" => 2026), and ok=false when no digits were found
// (NaN in JS). Sscanf's %d verb has exactly these semantics, except that it
// reports an error on int64 overflow where parseInt would yield a huge
// float; saturating instead keeps range checks (e.g. "Gregorian year cannot
// be greater than 9999") answering like the JS API.
func ParseInt(s string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		if errors.Is(err, strconv.ErrRange) {
			if strings.HasPrefix(strings.TrimSpace(s), "-") {
				return math.MinInt, true
			}
			return math.MaxInt, true
		}
		return 0, false
	}
	return n, true
}

// ParseFloat mimics JavaScript Number.parseFloat: leading whitespace is
// skipped, then the longest prefix that forms a valid float is consumed and
// whatever follows is ignored ("8.5deg" => 8.5). It returns an error only when
// no number could be read at all (NaN in JS), so a legacy URL with a
// trailing-junk float is read the way production reads it rather than rejected.
func ParseFloat(s string) (float64, error) {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' ||
			c == 'e' || c == 'E' {
			end++
			continue
		}
		break
	}
	for end > 0 {
		if f, err := strconv.ParseFloat(s[:end], 64); err == nil {
			return f, nil
		}
		end--
	}
	return 0, errNaN
}

// errNaN is returned by ParseFloat when JavaScript's Number.parseFloat would
// yield NaN, i.e. no numeric prefix.
var errNaN = errors.New("not a number")

// IsoDateString formats a date the way JavaScript Date.prototype.toISOString
// does for the date portion: 4 digits for years 0-9999, "+"/"-" and 6 digits
// outside that range.
func IsoDateString(gy int, gm time.Month, gd int) string {
	switch {
	case gy < 0:
		return fmt.Sprintf("-%06d-%02d-%02d", -gy, int(gm), gd)
	case gy > 9999:
		return fmt.Sprintf("+%06d-%02d-%02d", gy, int(gm), gd)
	default:
		return fmt.Sprintf("%04d-%02d-%02d", gy, int(gm), gd)
	}
}

// SmartApostrophe converts straight apostrophes to U+2019, same as
// @hebcal/core's renderer does for event titles ("Sh'vat" => "Sh’vat").
func SmartApostrophe(s string) string {
	return strings.ReplaceAll(s, "'", "’")
}

var nonWordRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)
var multiDashRe = regexp.MustCompile(`-+`)

// MakeAnchor mimics @hebcal/rest-api makeAnchor() used for the CSV filename.
func MakeAnchor(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "'", "")
	s = nonWordRe.ReplaceAllString(s, "-")
	s = multiDashRe.ReplaceAllString(s, "-")
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimSuffix(s, "-")
	return s
}

// xmlEscaper escapes the five characters that EJS <%= %> escapes.
var xmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&#34;",
	"'", "&#39;",
)

// XMLEscape escapes the five characters EJS <%= %> escapes.
func XMLEscape(s string) string {
	return xmlEscaper.Replace(s)
}
