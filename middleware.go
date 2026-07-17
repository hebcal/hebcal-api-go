package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "http_requests_total",
	Help: "Total number of HTTP requests",
}, []string{"method", "status"})

// bufWriter buffers the response so the middleware can compress it, set
// Content-Length, and log the final status/size.
type bufWriter struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func newBufWriter() *bufWriter {
	return &bufWriter{header: make(http.Header), status: 200}
}

func (w *bufWriter) Header() http.Header { return w.header }

func (w *bufWriter) WriteHeader(status int) { w.status = status }

func (w *bufWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

// compressThreshold was chosen empirically: response bodies just above it
// (multi-day batches, XML with several events) shrink 40-60% under
// gzip/brotli, while typical single-date JSON below it saves almost nothing
// over the header overhead. See TestThresholdExperiment.
const compressThreshold = 512

// brotliQuality 6 matches the setting used by www.hebcal.com (app-www.js).
const brotliQuality = 6

// negotiateEncoding picks the response encoding from Accept-Encoding,
// preferring brotli, with the same simple substring matching that
// hebcal-web's ETag classing uses.
func negotiateEncoding(r *http.Request) string {
	ae := r.Header.Get("Accept-Encoding")
	if strings.Contains(ae, "br") {
		return "br"
	}
	if strings.Contains(ae, "gzip") {
		return "gzip"
	}
	return ""
}

func compressibleType(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") ||
		strings.HasPrefix(contentType, "application/json") ||
		strings.HasPrefix(contentType, "application/xml")
}

// serve runs the handler with buffering, then applies gzip compression,
// response-time and length headers, Prometheus metrics, and access logging.
func (app *appServer) serve(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		bw := newBufWriter()
		h(bw, r)

		body := bw.buf.Bytes()
		uncompressedLen := len(body)
		contentType := bw.header.Get("Content-Type")
		if compressibleType(contentType) && bw.header.Get("Content-Encoding") == "" {
			compressed := false
			enc := negotiateEncoding(r)
			if uncompressedLen > compressThreshold && enc != "" {
				var zbuf bytes.Buffer
				var zw io.WriteCloser
				if enc == "br" {
					zw = brotli.NewWriterLevel(&zbuf, brotliQuality)
				} else {
					zw = gzip.NewWriter(&zbuf)
				}
				zw.Write(body)
				zw.Close()
				body = zbuf.Bytes()
				bw.header.Set("Content-Encoding", enc)
				compressed = true
			}
			// mimic hebcal-web: Vary appears on any compressible response,
			// but is stripped from uncompressed JSON
			if compressed || !strings.HasPrefix(contentType, "application/json") {
				bw.header.Set("Vary", "Accept-Encoding")
			}
		}

		hdr := w.Header()
		for k, vv := range bw.header {
			for _, v := range vv {
				hdr.Add(k, v)
			}
		}
		durMs := float64(time.Since(start).Nanoseconds()) / 1e6
		hdr.Set("X-Response-Time", strconv.FormatFloat(durMs, 'f', 3, 64)+"ms")
		if bw.status != 304 && bw.status != 204 {
			hdr.Set("Content-Length", strconv.Itoa(len(body)))
		}
		w.WriteHeader(bw.status)
		if r.Method != http.MethodHead && bw.status != 304 && bw.status != 204 {
			w.Write(body)
		}

		httpRequestsTotal.WithLabelValues(r.Method, strconv.Itoa(bw.status)).Inc()
		app.logAccess(r, bw, uncompressedLen, start)
	}
}

// ------------------------------------------------------------------ logging

// accessLogger writes pino-compatible JSON log lines and supports reopening
// the log file on SIGHUP/SIGUSR1 for logrotate.
type accessLogger struct {
	mu       sync.Mutex
	path     string
	f        *os.File
	out      io.Writer
	hostname string
	pid      int
}

func newAccessLogger(path string) (*accessLogger, error) {
	hostname, _ := os.Hostname()
	lg := &accessLogger{path: path, out: os.Stdout, hostname: hostname, pid: os.Getpid()}
	if path != "" && path != "-" {
		if err := lg.Reopen(); err != nil {
			return nil, err
		}
	}
	return lg, nil
}

// Reopen closes and reopens the log file (called on SIGHUP/SIGUSR1).
func (lg *accessLogger) Reopen() error {
	if lg.path == "" || lg.path == "-" {
		return nil
	}
	f, err := os.OpenFile(lg.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	lg.mu.Lock()
	old := lg.f
	lg.f = f
	lg.out = f
	lg.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

const (
	levelInfo = 30
	levelWarn = 40
)

// write emits one JSON log line. Field order matches pino: level, time, pid,
// hostname, then the supplied fields.
func (lg *accessLogger) write(level int, fields []kv) {
	var buf bytes.Buffer
	buf.WriteString(`{"level":`)
	buf.WriteString(strconv.Itoa(level))
	buf.WriteString(`,"time":`)
	buf.WriteString(strconv.FormatInt(time.Now().UnixMilli(), 10))
	buf.WriteString(`,"pid":`)
	buf.WriteString(strconv.Itoa(lg.pid))
	buf.WriteString(`,"hostname":`)
	buf.Write(jsonString(lg.hostname))
	for _, f := range fields {
		buf.WriteByte(',')
		buf.Write(jsonString(f.k))
		buf.WriteByte(':')
		buf.Write(f.v)
	}
	buf.WriteString("}\n")
	lg.mu.Lock()
	defer lg.mu.Unlock()
	lg.out.Write(buf.Bytes())
}

type kv struct {
	k string
	v []byte
}

func jsonString(s string) []byte {
	// jsonMarshal avoids the \u0026 escaping of & that json.Marshal applies
	return jsonMarshal(s)
}

func jsonInt(n int) []byte {
	return []byte(strconv.Itoa(n))
}

// Info logs a startup/shutdown style message.
func (lg *accessLogger) Info(msg string) {
	lg.write(levelInfo, []kv{{"msg", jsonString(msg)}})
}

// logAccess writes one access-log line, similar to hebcal-web makeLogInfo().
func (app *appServer) logAccess(r *http.Request, bw *bufWriter, length int, start time.Time) {
	fields := []kv{
		{"status", jsonInt(bw.status)},
	}
	if length > 0 {
		fields = append(fields, kv{"length", jsonInt(length)})
	}
	fields = append(fields,
		kv{"duration", jsonInt(int(time.Since(start).Milliseconds()))},
		kv{"ip", jsonString(clientIP(r))},
		kv{"method", jsonString(r.Method)},
		kv{"url", jsonString(r.URL.RequestURI())},
		kv{"ua", jsonString(r.Header.Get("User-Agent"))},
	)
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		fields = append(fields, kv{"inm", jsonString(inm)})
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		fields = append(fields, kv{"ims", jsonString(ims)})
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		fields = append(fields, kv{"ref", jsonString(ref)})
	}
	if enc := bw.header.Get("Content-Encoding"); enc != "" {
		fields = append(fields, kv{"enc", jsonString(enc)})
	}
	level := levelInfo
	if bw.status >= 400 && bw.status != 404 {
		level = levelWarn
	}
	app.logger.write(level, fields)
}

// clientIP returns the client IP address, preferring X-Client-IP over
// X-Forwarded-For when the service runs behind a reverse proxy.
func clientIP(r *http.Request) string {
	if xcip := r.Header.Get("X-Client-IP"); xcip != "" {
		return strings.TrimSpace(xcip)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
