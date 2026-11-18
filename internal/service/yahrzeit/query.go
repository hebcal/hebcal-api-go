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

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
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
		key = jsutil.QueryUnescape(key)
		if _, dup := q.vals[key]; dup {
			continue
		}
		q.keys = append(q.keys, key)
		q.vals[key] = jsutil.QueryUnescape(val)
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

// Delete removes a parameter.
func (q *Query) Delete(key string) {
	if _, ok := q.vals[key]; !ok {
		return
	}
	delete(q.vals, key)
	for i, k := range q.keys {
		if k == key {
			q.keys = append(q.keys[:i], q.keys[i+1:]...)
			break
		}
	}
}

// Keys returns the parameter names in the order they first appeared.
func (q *Query) Keys() []string { return q.keys }

// on reports whether a checkbox parameter is set, in either spelling.
func (q *Query) on(key string) bool {
	v := q.vals[key]
	return v == "on" || v == "1"
}
