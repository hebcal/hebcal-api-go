package download

import (
	"fmt"
	"net/url"
	"strings"
)

// The classic CGI download URL, /hebcal/index.cgi/<name>.pdf?<query>, older
// even than the /v2/h/ base64 form. Crawlers still ask for it, so this service
// serves it -- and it arrives in two encodings, which is most of what is below.
//
// A CGI request resolves to the same options as the equivalent /v2/ URL (see
// the "/v2/" note in CLAUDE.md, the legacy city and degrees/minutes location
// forms included), so it is decoded by building the same v2Query and running it
// through DecodeV2, after the legacy-parameter rewrites every query-string form
// gets (applyLegacyParams).
//
// The URL carries its parameters in the query string rather than the path, so
// unlike ParseV2Path/ParsePath this one needs the raw query as well.

// cgiPrefix is the classic CGI download path this service answers.
const cgiPrefix = "/hebcal/index.cgi/"

// exportPrefix is the older /export/ spelling of the same download. It takes
// the query string identically, and may nest the file under further path
// segments (/export/x.csv, /export/76/0db7b2.csv), which are only a cache-
// busting name and carry no options.
const exportPrefix = "/export/"

// isCGIPath reports whether path is a classic query-string download, under
// either prefix.
func isCGIPath(path string) bool {
	return strings.HasPrefix(path, cgiPrefix) || strings.HasPrefix(path, exportPrefix)
}

// ParseCGIPath decodes a /hebcal/index.cgi/<name><ext> or /export/.../<name><ext>
// request into the query map that carries it: querystring normalization
// (normalizeCGIQuery) followed by the legacy-parameter rewrites
// (applyLegacyParams).
//
// Only the caller's extension is accepted; the .ics feeds and the yahrzeit
// calendars this same path also serves are handled elsewhere, so anything else
// is an error the handler maps to 404, as it does for the equivalent /v2/
// shapes.
func ParseCGIPath(path, rawQuery, ext string) (v2Query, error) {
	if !isCGIPath(path) {
		return nil, fmt.Errorf("expected /hebcal/index.cgi/<name>%s or /export/<name>%s, got %q", ext, ext, path)
	}
	if !strings.HasSuffix(path, ext) {
		return nil, fmt.Errorf("not a %s request", ext)
	}
	// The error is ignored for the same reason ParseV2Path ignores it: a stray
	// percent sign has to leave the other parameters intact rather than fail
	// the request.
	values, _ := url.ParseQuery(normalizeCGIQuery(rawQuery))
	q := make(v2Query, len(values))
	for key, vals := range values {
		// Last value wins -- the same choice ParseV2Path makes.
		q[key] = vals[len(vals)-1]
	}
	applyLegacyParams(q)
	return q, nil
}

// normalizeCGIQuery rewrites the CGI query string into a standard one.
//
// Two encodings reach it. The old CGI accepted ';' as a parameter separator, so
// a query can arrive semicolon-separated (dl=1;v=1;...), which is turned into
// '&'. And a whole query can arrive percent-encoded a second time, with its ';'
// as %3B (and usually its '=' as %3D): that is spotted by the dl=1%3B /
// subscribe=1%3B prefix and unescaped before splitting -- a Latin-1 decode that
// leaves '+' alone (unescapeLatin1), because of the ancient encoding these URLs
// were written in. Production answers that second form with a 301 to the '&'
// spelling and renders the redirected request; this service does the unescape
// inline and renders in one hop.
func normalizeCGIQuery(qs string) string {
	if hasFold(qs, "dl=1%3B") || hasFold(qs, "subscribe=1%3B") {
		qs = unescapeLatin1(qs)
	}
	if strings.Contains(qs, ";") {
		qs = strings.ReplaceAll(qs, ";", "&")
	}
	return qs
}

// applyLegacyParams rewrites parameters old download URLs still carry, before
// DecodeV2 reads any option: both query-string forms, the CGI one and the
// /v2/h/ base64 one, can hold them. DecodeV2 has no counterpart for these.
func applyLegacyParams(q v2Query) {
	// nh=on is the very old "all holiday categories" switch, expanded to the
	// five negative options other than Rosh Chodesh. It requires the literal
	// "on", and sets each unconditionally, so it overrides an explicit
	// maj/min/mod/mf/ss in the same URL.
	if q["nh"] == "on" {
		for _, k := range []string{"maj", "min", "mod", "mf", "ss"} {
			q[k] = "on"
		}
		delete(q, "nh")
	}
	// Lowercase m=on is the old spelling of M=on (Havdalah at tzeit); the
	// numeric havdalah offset is the other meaning of m, so the two are told
	// apart by the literal value.
	if q["m"] == "on" {
		q["M"] = "on"
		delete(q, "m")
	}
}

// hasFold reports whether s begins with prefix, ignoring ASCII case in the
// percent escapes -- a crawler may lowercase %3B to %3b, and unescapeLatin1
// accepts either.
func hasFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// unescapeLatin1 decodes %XX escapes and leaves everything else -- '+'
// included, and a malformed '%' -- untouched. It is not url.PathUnescape,
// which rejects a bad escape, nor url.QueryUnescape, which also turns '+' into
// a space.
func unescapeLatin1(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
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
