package main

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// jsParseInt mimics JavaScript parseInt(str, 10): leading whitespace and an
// optional sign are allowed, then as many decimal digits as possible are
// consumed. Returns ok=false when no digits were found (NaN in JS).
func jsParseInt(s string) (int, bool) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' ||
		s[i] == '\v' || s[i] == '\f') {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	n := 0
	overflow := false
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		if !overflow {
			d := int(s[i] - '0')
			if n > (math.MaxInt-d)/10 {
				overflow = true
			} else {
				n = n*10 + d
			}
		}
		i++
	}
	if i == start {
		return 0, false
	}
	if overflow {
		n = math.MaxInt
	}
	if neg {
		n = -n
	}
	return n, true
}

// isoDateString formats a date the way JavaScript Date.prototype.toISOString
// does for the date portion: 4 digits for years 0-9999, "+"/"-" and 6 digits
// outside that range.
func isoDateString(gy int, gm time.Month, gd int) string {
	switch {
	case gy < 0:
		return fmt.Sprintf("-%06d-%02d-%02d", -gy, int(gm), gd)
	case gy > 9999:
		return fmt.Sprintf("+%06d-%02d-%02d", gy, int(gm), gd)
	default:
		return fmt.Sprintf("%04d-%02d-%02d", gy, int(gm), gd)
	}
}

// smartApostrophe converts straight apostrophes to U+2019, same as
// @hebcal/core's renderer does for event titles ("Sh'vat" => "Sh’vat").
func smartApostrophe(s string) string {
	return strings.ReplaceAll(s, "'", "’")
}

// urlFriendly mimics @hebcal/core urlFriendly() used in event URLs.
func urlFriendly(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

var nonWordRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)
var multiDashRe = regexp.MustCompile(`-+`)

// makeAnchor mimics @hebcal/rest-api makeAnchor() used for the CSV filename.
func makeAnchor(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "'", "")
	s = nonWordRe.ReplaceAllString(s, "-")
	s = multiDashRe.ReplaceAllString(s, "-")
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimSuffix(s, "-")
	return s
}

// xmlEscape escapes the five characters that EJS <%= %> escapes.
var xmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&#34;",
	"'", "&#39;",
)

func xmlEscape(s string) string {
	return xmlEscaper.Replace(s)
}
