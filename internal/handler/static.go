package handler

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
)

// staticTypes are the content types of the static files served; anything
// else falls back on the extension's registered type.
var staticTypes = map[string]string{
	".ics": "text/calendar; charset=utf-8",
	".csv": "text/csv; charset=utf-8",
}

// staticICal serves the static calendars, DocumentRoot/ical/<name>, for 7
// days. Only a plain file name is served: one flat directory, no
// subdirectories, no dot files, nothing a path could climb out of.
func (s *Server) staticICal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/ical/")
	if !plainFileName(name) {
		httpx.WriteNotFoundText(w)
		return
	}
	s.serveStatic(w, r, "ical/"+name, httpx.CacheControl7Days)
}

// faviconCacheControl is a year, immutable: the icon is replaced by a
// deploy, and a browser should never ask again before then.
const faviconCacheControl = httpx.CacheControl1Year + ", immutable"

// favicon serves DocumentRoot/favicon.ico, installed there by make install.
func (s *Server) favicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
		return
	}
	s.serveStatic(w, r, "favicon.ico", faviconCacheControl)
}

// plainFileName reports whether name is a single path element that is safe
// to join to a directory: not empty, not a dot file, no separators, and
// unchanged by cleaning.
func plainFileName(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") &&
		!strings.ContainsAny(name, `/\`) && path.Clean(name) == name
}

// serveStatic sends the regular file at rel, a slash-separated path the
// caller has already validated, under DocumentRoot. When the client accepts
// it and a brotli (or failing that gzip) copy of the file is on disk, that
// copy is sent as is, rather than the file compressed on the fly.
// Last-Modified is the modification time of whichever file is sent, so
// If-Modified-Since works for either; cacheControl is set on success only, so
// a 404 is never cached.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request, rel, cacheControl string) {
	if s.DocumentRoot == "" {
		httpx.WriteNotFoundText(w)
		return
	}
	file := filepath.Join(s.DocumentRoot, filepath.FromSlash(rel))
	encoding := ""
	for _, enc := range []struct{ name, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
		if acceptsEncoding(r.Header.Get("Accept-Encoding"), enc.name) && isRegular(file+enc.ext) {
			file += enc.ext
			encoding = enc.name
			break
		}
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			httpx.RecordError(w, err)
		}
		httpx.WriteNotFoundText(w)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", cacheControl)
	h.Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	h.Set("X-Content-Type-Options", "nosniff")
	if notModified(r, info.ModTime()) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, err := os.ReadFile(file)
	if err != nil {
		httpx.WritePlainError(w, err)
		return
	}
	ext := path.Ext(rel)
	ctype := staticTypes[ext]
	if ctype == "" {
		ctype = mime.TypeByExtension(ext)
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	h.Set("Content-Type", ctype)
	if encoding != "" {
		// The middleware leaves a body that is already encoded alone, so
		// the Vary it would have added is set here.
		h.Set("Content-Encoding", encoding)
		h.Set("Vary", "Accept-Encoding")
	}
	w.Write(body)
}

func isRegular(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.Mode().IsRegular()
}

// acceptsEncoding reports whether an Accept-Encoding header accepts coding:
// named, or covered by "*", with a non-zero quality.
func acceptsEncoding(header, coding string) bool {
	star := false
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			if k, v, ok := strings.Cut(strings.TrimSpace(p), "="); ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = f
				}
			}
		}
		switch name {
		case coding:
			return q > 0
		case "*":
			star = q > 0
		}
	}
	return star
}

// notModified reports whether a conditional GET may be answered 304: it
// carries If-Modified-Since no earlier than the file's modification time,
// no If-None-Match (there is no ETag to match), and no no-cache directive.
func notModified(r *http.Request, mtime time.Time) bool {
	ims := r.Header.Get("If-Modified-Since")
	if ims == "" || r.Header.Get("If-None-Match") != "" ||
		strings.Contains(r.Header.Get("Cache-Control"), "no-cache") {
		return false
	}
	t, err := http.ParseTime(ims)
	return err == nil && !mtime.Truncate(time.Second).After(t)
}
