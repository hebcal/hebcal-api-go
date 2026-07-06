package main

import (
	"encoding/base64"
	"hash/fnv"
	"net/http"
	"net/url"
	"runtime/debug"
	"sort"
	"strings"
)

// libraryVersions is baked into every ETag so tags change when the
// application or the hebcal libraries are upgraded.
var libraryVersions = func() string {
	var b strings.Builder
	if info, ok := debug.ReadBuildInfo(); ok {
		b.WriteString(info.Main.Path + "@" + info.Main.Version)
		for _, dep := range info.Deps {
			if strings.HasPrefix(dep.Path, "github.com/hebcal/") {
				b.WriteString("," + dep.Path + "@" + dep.Version)
			}
		}
	}
	return b.String()
}()

// makeETag computes a weak ETag from the request path, the query string
// (minus utm_* params), the Accept-Encoding class, and the library versions.
// A 128-bit FNV-1a hash stands in for the murmurhash3 used by hebcal-web;
// weak ETags need not match across implementations.
func makeETag(r *http.Request, extra string) string {
	h := fnv.New128a()
	h.Write([]byte(libraryVersions))
	h.Write([]byte{0})
	h.Write([]byte(r.URL.Path))
	h.Write([]byte{0})
	q := r.URL.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		if strings.HasPrefix(k, "utm_") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range q[k] {
			h.Write([]byte(k + "=" + v))
			h.Write([]byte{0})
		}
	}
	h.Write([]byte(extra))
	h.Write([]byte{0})
	// vary the tag by encoding class, like hebcal-web does
	enc := r.Header.Get("Accept-Encoding")
	if strings.Contains(enc, "zstd") {
		h.Write([]byte("zstd"))
	} else if strings.Contains(enc, "br") {
		h.Write([]byte("br"))
	} else if strings.Contains(enc, "gzip") {
		h.Write([]byte("gzip"))
	}
	sum := h.Sum(nil)
	return `W/"` + base64.RawURLEncoding.EncodeToString(sum) + `"`
}

// etagExtra builds a canonical representation of the parsed conversion for
// inclusion in the ETag.
func etagExtra(p convProps) string {
	var b strings.Builder
	if p.isRange {
		b.WriteString("range:")
		b.WriteString(url.QueryEscape(gregFromRD(p.startRD).String()))
		b.WriteString("-")
		b.WriteString(url.QueryEscape(gregFromRD(p.endRD).String()))
	} else {
		b.WriteString("single:")
		b.WriteString(p.dt.String())
		b.WriteString(",")
		b.WriteString(p.hd.String())
		if p.gs {
			b.WriteString(",gs")
		}
	}
	return b.String()
}

// checkFresh reports whether the client's cached copy identified by
// If-None-Match is still fresh.
func checkFresh(r *http.Request, etag string) bool {
	inm := r.Header.Get("If-None-Match")
	if inm == "" {
		return false
	}
	for _, val := range strings.Split(inm, ",") {
		val = strings.TrimSpace(val)
		if val == etag || val == "*" {
			return true
		}
	}
	return false
}
