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

// ParseFloat parses a query float, returning an error for empty/invalid input.
func ParseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

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
