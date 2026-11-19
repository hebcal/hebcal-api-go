package httpx

import (
	"encoding/base64"
	"hash/fnv"
	"net/http"
	"sort"
	"strings"
)

// MakeETag computes a weak ETag from the request path, the query string
// (minus utm_* params) using a 128-bit FNV-1a hash.
// extra mixes in any per-request input that is
// not in the URL (e.g. the caller's IP for /complete).
func MakeETag(r *http.Request, extra string) string {
	h := fnv.New128a()
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
