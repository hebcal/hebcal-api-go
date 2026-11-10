// Package yahrzeit builds the personal Yahrzeit, Hebrew birthday and
// anniversary calendars download.hebcal.com serves as iCalendar feeds.
//
// A calendar is described entirely by its query string: a numbered entry per
// person or event (n1 the name, t1 the type, y1/m1/d1 the Gregorian date, s1
// whether it happened after sunset), plus a few calendar-wide options. That
// query string reaches the service in several URL shapes, which request.go
// reduces to one.
package yahrzeit

import (
	"strings"
)

// Query is a decoded query string that remembers the order its parameters
// arrived in, which names the calendar: the title lists the people in the
// order the URL does. A repeated parameter keeps its first value.
type Query struct {
	keys []string
	vals map[string]string
}

// ParseQuery decodes a query string. Decoding never fails: a malformed
// percent escape is kept as written and bytes that are not UTF-8 become
// U+FFFD, so one bad parameter in a ten-year-old URL does not cost the
// subscriber the rest of the calendar.
func ParseQuery(qs string) *Query {
	q := &Query{vals: make(map[string]string)}
	for qs != "" {
		var pair string
		pair, qs, _ = strings.Cut(qs, "&")
		if pair == "" {
			continue
		}
		key, val, _ := strings.Cut(pair, "=")
		key = unescapeComponent(key)
		if _, dup := q.vals[key]; dup {
			continue
		}
		q.keys = append(q.keys, key)
		q.vals[key] = unescapeComponent(val)
	}
	return q
}

// Get returns a parameter's value, or "" when it is absent.
func (q *Query) Get(key string) string { return q.vals[key] }

// Set adds or replaces a parameter.
func (q *Query) Set(key, val string) {
	if _, ok := q.vals[key]; !ok {
		q.keys = append(q.keys, key)
	}
	q.vals[key] = val
}

// Keys returns the parameter names in the order they first appeared.
func (q *Query) Keys() []string { return q.keys }

// on reports whether a checkbox parameter is set, in either spelling.
func (q *Query) on(key string) bool {
	v := q.vals[key]
	return v == "on" || v == "1"
}

// unescapeComponent decodes '+' as a space and each valid %XX escape as a
// byte, leaving anything else alone.
func unescapeComponent(s string) string {
	if !strings.ContainsAny(s, "%+") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '+':
			b.WriteByte(' ')
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return strings.ToValidUTF8(b.String(), "�")
}

// unescapeLatin1 decodes %XX escapes and nothing else: '+' stays a plus, and
// each escape is one character in its own right (%E9 is "é") rather than a
// byte of UTF-8. It undoes the second layer of encoding on a doubly-encoded
// query string, whose first layer is then decoded as usual.
func unescapeLatin1(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteRune(rune(unhex(s[i+1])<<4 | unhex(s[i+2])))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}
