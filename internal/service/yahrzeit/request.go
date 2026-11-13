package yahrzeit

import (
	"encoding/base64"
	"path"
	"regexp"
	"strings"
)

// Request is a download request reduced to the query string that describes
// its calendar and the file name it asks for.
type Request struct {
	// Path is the request's path after rewriting: the /v2/ and the
	// doubly-encoded forms become /export/<filename>.
	Path string
	// RawQuery is the query string the calendar was decoded from, before
	// parsing -- for /v2/ the one hidden in the base64 path.
	RawQuery string
	// Query is RawQuery parsed.
	Query *Query
	// Opaque is true when the query string was not in the URL's own query
	// (the /v2/ base64 form), so a log of the URL does not show it.
	Opaque bool
	// ID is a saved calendar's id (the /v3/ form); legacy URLs have none.
	ID string
	// Version changes whenever a saved calendar is edited, for the cache
	// validator. Empty for a calendar carried in its URL.
	Version string
}

// The URL prefixes a personal calendar is served under.
const (
	exportPrefix = "/export"
	cgiPrefix    = "/yahrzeit/yahrzeit.cgi/"
	hebcalPrefix = "/hebcal/index.cgi/"
	v2Prefix     = "/v2/"
)

// encodedQueryRe finds a query string that was percent-encoded into the path,
// question mark and all: /export/x.ics%3Fsubscribe%3D1%3Bt1%3D...
var encodedQueryRe = regexp.MustCompile(`\.ics%3F(subscribe|dl)%3D1`)

// ParseRequest decodes a download URL -- requestURI is the request target as
// it arrived, undecoded -- in any of the shapes that can name a personal
// calendar:
//
//	/v2/y/<base64 query string>/<name>.ics
//	/yahrzeit/yahrzeit.cgi/<name>.ics?<query>
//	/export/[<dir>/]<name>.ics?<query>
//	/hebcal/index.cgi/<name>.ics?<query>
//
// The query-string shapes come in three spellings of the query itself: with
// '&' between parameters, with the ';' the original CGI also accepted, and
// percent-encoded a second time (after a real '?', or along with it into the
// path). ok is false for any other path.
func ParseRequest(requestURI string) (req *Request, ok bool) {
	path, qs, _ := strings.Cut(requestURI, "?")
	req = &Request{Path: path}
	switch {
	case strings.HasPrefix(path, exportPrefix),
		strings.HasPrefix(path, cgiPrefix),
		strings.HasPrefix(path, hebcalPrefix):
		if strings.HasPrefix(qs, "subscribe=1%3B") || strings.HasPrefix(qs, "dl=1%3B") {
			qs = unescapeLatin1(qs)
		} else if loc := encodedQueryRe.FindStringIndex(path); loc != nil {
			qs = unescapeLatin1(path[loc[0]+len(".ics%3F"):])
			req.Path = "/export/export.ics"
		}
		qs = strings.ReplaceAll(qs, ";", "&")
	case strings.HasPrefix(path, v2Prefix) && len(path) >= len("/v2/y/"):
		// /v2/<kind>/<base64>/<filename>. The kind letter is not what decides
		// the calendar; the v= parameter inside the payload is.
		data, filename, found := strings.Cut(path[len("/v2/y/"):], "/")
		if !found {
			filename = "hebcal.ics"
		}
		qs = decodeBase64ASCII(data)
		req.Path = "/export/" + filename
		req.Opaque = true
	default:
		return nil, false
	}
	req.RawQuery = qs
	req.Query = ParseQuery(qs)
	// The oldest URLs name no calendar type at all; one that carries a first
	// entry's date is a personal calendar.
	q := req.Query
	if strings.HasPrefix(req.Path, "/export/") && q.Get("v") == "" &&
		q.Get("y1") != "" && q.Get("m1") != "" && q.Get("d1") != "" {
		q.Set("v", "yahrzeit")
	}
	return req, true
}

// IsYahrzeit reports whether the request names a personal calendar rather
// than a holiday one.
func (r *Request) IsYahrzeit() bool {
	return strings.HasPrefix(r.Query.Get("v"), "y")
}

// Ext returns the path's extension, ".ics" or ".csv" for a calendar, or ""
// when it has none.
func (r *Request) Ext() string {
	return path.Ext(r.Path)
}

// decodeBase64ASCII decodes as much of a base64 payload as is valid, in
// either alphabet and with or without padding, and clears the high bit of
// every byte: the payload is a percent-encoded query string, so it is ASCII.
func decodeBase64ASCII(s string) string {
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	if i := strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+' || r == '/')
	}); i >= 0 {
		s = s[:i]
	}
	if len(s)%4 == 1 {
		s = s[:len(s)-1]
	}
	raw, _ := base64.RawStdEncoding.DecodeString(s)
	for i, c := range raw {
		raw[i] = c & 0x7f
	}
	return string(raw)
}
