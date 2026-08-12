package httpx

import (
	"encoding/base64"
	"hash/fnv"
	"net/http"
	"sort"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/config"
)

// MakeETag computes a weak ETag from the request path, the query string
// (minus utm_* params), the Accept-Encoding class, and the library versions.
// A 128-bit FNV-1a hash stands in for the murmurhash3 used by hebcal-web;
// weak ETags need not match across implementations. extra mixes in any
// per-request input that is not in the URL (e.g. the caller's IP for
// /complete).
func MakeETag(r *http.Request, extra string) string {
	h := fnv.New128a()
	h.Write([]byte(config.LibraryVersions))
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
	if extra != "" {
		h.Write([]byte(extra))
		h.Write([]byte{0})
	}
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

// CheckFresh reports whether the client's cached copy identified by
// If-None-Match is still fresh.
func CheckFresh(r *http.Request, etag string) bool {
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
