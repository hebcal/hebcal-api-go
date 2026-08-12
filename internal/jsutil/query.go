package jsutil

import "net/url"

// QueryGet returns the query param value, or "undefined" when the parameter is
// absent, mimicking how the JS code interpolates missing values into error
// messages (parseInt(undefined) => "must be numeric: undefined").
func QueryGet(q url.Values, key string) string {
	if !q.Has(key) {
		return "undefined"
	}
	return q.Get(key)
}

// QueryEmpty reports whether a parameter is absent or empty, i.e. falsy in the
// JS routes this service is ported from.
func QueryEmpty(q url.Values, key string) bool {
	return q.Get(key) == ""
}

// IsOn reports whether a boolean query parameter is set, matching the
// booleanOpts loop in hebcal-web src/calendar.js.
func IsOn(v string) bool {
	return v == "on" || v == "1"
}
