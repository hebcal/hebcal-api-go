package jsutil

import (
	"net/url"
	"strings"
	"unicode"
)

// QueryGet returns the query param value, or "undefined" when the parameter is
// absent, so a missing value interpolates into an error message the way the
// reference API's does ("must be numeric: undefined").
func QueryGet(q url.Values, key string) string {
	if !q.Has(key) {
		return "undefined"
	}
	return q.Get(key)
}

// QueryEmpty reports whether a parameter is absent or empty (falsy).
func QueryEmpty(q url.Values, key string) bool {
	return q.Get(key) == ""
}

// IsOn reports whether a boolean query parameter is set ("on" or "1").
func IsOn(v string) bool {
	return v == "on" || v == "1"
}

// TrimTrailingWhitespace strips trailing (but not leading) whitespace from
// every value of every query parameter, in place. Clients occasionally send
// a stray trailing space or newline (a copy-pasted value, a form field with
// autocomplete padding); rather than strictly rejecting that with a 400 from
// whatever numeric or date parser sees it next, most of our API surfaces
// tolerate it silently by trimming it here before any parsing happens.
// Leading whitespace is left alone, since a parser choking on that usually
// means the value itself is malformed rather than merely padded.
func TrimTrailingWhitespace(q url.Values) {
	for _, vals := range q {
		for i, v := range vals {
			vals[i] = strings.TrimRightFunc(v, unicode.IsSpace)
		}
	}
}
