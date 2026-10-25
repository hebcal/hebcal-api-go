package httpx

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/reqlog"
)

var httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "http_requests_total",
	Help: "Total number of HTTP requests",
}, []string{"method", "status"})

// httpRequestDuration records handler-to-last-byte time per route. The buckets
// are dense below 200ms, where nearly every response lands: the JSON routes sit
// under a millisecond, and a third of /v4 lands in 10-20ms. The tail to 10s
// is mostly the download limiter: a request refused after its 5s queue wait is
// recorded just past 5s, so the 5-10s bucket is shed traffic, not slow renders. route is the
// registered mux pattern, never the URL, so its cardinality is bounded by the
// routes in server.go.
var httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "http_request_duration_seconds",
	Help:    "HTTP request latency in seconds, by route",
	Buckets: []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.015, 0.02, 0.03, 0.05, 0.075, 0.1, 0.15, 0.2, 0.3, 0.5, 0.75, 1, 2.5, 5, 10},
}, []string{"route"})

// routeLabel names the mux pattern that matched r, without a trailing slash so
// "/shabbat" and "/shabbat/" count as one route. The catch-all "/" stays "/".
func routeLabel(r *http.Request) string {
	p := r.Pattern
	if p == "" {
		return "unknown"
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// bufWriter buffers the response so the middleware can compress it, set
// Content-Length, and log the final status/size. It also carries the request's
// reqlog.Collector, so an error helper handed this writer can record the error
// value it renders for the access log's "msg" field (see RecordError).
type bufWriter struct {
	header http.Header
	buf    bytes.Buffer
	status int
	calls  *reqlog.Collector
}

func newBufWriter(calls *reqlog.Collector) *bufWriter {
	return &bufWriter{header: make(http.Header), status: 200, calls: calls}
}

func (w *bufWriter) Header() http.Header { return w.header }

func (w *bufWriter) WriteHeader(status int) { w.status = status }

func (w *bufWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

// CompressThreshold was chosen empirically: response bodies just above it
// (multi-day batches, XML with several events) shrink 40-60% under
// gzip/brotli, while typical single-date JSON below it saves almost nothing
// over the header overhead. See TestThresholdExperiment.
const CompressThreshold = 512

// brotliQuality 6 matches the setting used by www.hebcal.com.
const brotliQuality = 6

// gzipWriters holds gzip.Writers for reuse. A new one allocates about 800 KB
// of compressor state, which dwarfs a typical body; Reset keeps that state.
var gzipWriters = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

// Brotli window sizes, in bits. brotliMaxLGWin is the library's default, so a
// large body compresses exactly as an unsized writer would. At 16 bits and
// below brotli switches to a weaker match finder, which costs a few percent on
// small bodies, so windows start at 17 (128 KB).
const (
	brotliMinLGWin = 17
	brotliMaxLGWin = 22
)

// brotliWriters holds brotli.Writers for reuse, one pool per window size,
// since a writer's window is fixed when it is created.
var brotliWriters [brotliMaxLGWin + 1]sync.Pool

func init() {
	for lg := brotliMinLGWin; lg <= brotliMaxLGWin; lg++ {
		opts := brotli.WriterOptions{Quality: brotliQuality, LGWin: lg}
		brotliWriters[lg].New = func() any { return brotli.NewWriterOptions(nil, opts) }
	}
}

// brotliLGWin returns the smallest window that holds a body of n bytes, so a
// short response does not need a writer with the default 4 MB ring buffer.
// Brotli reserves 16 bytes of each window. Only the stream header differs from
// a default-window encoding.
func brotliLGWin(n int) int {
	lg := brotliMinLGWin
	for lg < brotliMaxLGWin && (1<<lg)-16 < n {
		lg++
	}
	return lg
}

// compress encodes body with enc ("zstd", "br" or "gzip").
func compress(enc string, body []byte) []byte {
	if enc == "zstd" {
		return zstdEncoder.EncodeAll(body, nil)
	}
	// JSON, XML and CSV compress to well under half, so this sizes the buffer
	// without regrowing it in the common case.
	zbuf := bytes.NewBuffer(make([]byte, 0, len(body)/2))
	if enc == "br" {
		pool := &brotliWriters[brotliLGWin(len(body))]
		zw := pool.Get().(*brotli.Writer)
		zw.Reset(zbuf)
		zw.Write(body)
		zw.Close()
		zw.Reset(nil)
		pool.Put(zw)
	} else {
		zw := gzipWriters.Get().(*gzip.Writer)
		zw.Reset(zbuf)
		zw.Write(body)
		zw.Close()
		zw.Reset(nil)
		gzipWriters.Put(zw)
	}
	return zbuf.Bytes()
}

// zstdEncoder compresses whole buffered bodies with EncodeAll, which is safe
// for concurrent use. The default level is comparable to gzip -6 in speed and
// beats it on size.
var zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))

// negotiateEncoding picks the response encoding from Accept-Encoding,
// preferring zstd, then brotli, with the same simple substring matching the
// ETag classing uses.
func negotiateEncoding(r *http.Request) string {
	ae := r.Header.Get("Accept-Encoding")
	if strings.Contains(ae, "zstd") {
		return "zstd"
	}
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

// Middleware wraps route handlers with the response processing every route
// shares.
type Middleware struct {
	Logger *logger.AccessLogger
}

// callHandler runs h and recovers a panic into a 500, so a bug in one handler
// answers one request rather than taking down the process. h writes into bw
// rather than the real ResponseWriter, so a recovered panic can still discard
// a partial body and set a clean status; that would not be safe against the
// real http.ResponseWriter, which may already have flushed a header.
func (m *Middleware) callHandler(bw *bufWriter, r *http.Request, h http.HandlerFunc, calls *reqlog.Collector) {
	defer func() {
		if rec := recover(); rec != nil {
			m.Logger.Write(logger.LevelError, []logger.KV{
				{K: "panic", V: logger.String(fmt.Sprint(rec))},
				{K: "stack", V: logger.String(string(debug.Stack()))},
				{K: "url", V: logger.String(r.URL.RequestURI())},
			})
			bw.buf.Reset()
			bw.header = make(http.Header)
			bw.status = http.StatusInternalServerError
			calls.SetError(fmt.Errorf("panic: %v", rec))
			http.Error(bw, "Internal Server Error", http.StatusInternalServerError)
		}
	}()
	h(bw, r)
}

// Serve runs the handler with buffering, then applies zstd/brotli/gzip compression,
// response-time and length headers, Prometheus metrics, and access logging.
func (m *Middleware) Serve(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Seed a per-request collector so backend calls made deep in the handler
		// (readings-svc) can be folded into this request's single log line.
		ctx, calls := reqlog.NewContext(r.Context())
		bw := newBufWriter(calls)
		r = r.WithContext(ctx)
		m.callHandler(bw, r, h, calls)

		body := bw.buf.Bytes()
		uncompressedLen := len(body)
		contentType := bw.header.Get("Content-Type")
		if compressibleType(contentType) && bw.header.Get("Content-Encoding") == "" {
			compressed := false
			enc := negotiateEncoding(r)
			if uncompressedLen > CompressThreshold && enc != "" {
				body = compress(enc, body)
				bw.header.Set("Content-Encoding", enc)
				compressed = true
			}
			// Vary appears on any compressible response, but is stripped from
			// uncompressed JSON
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
		// Health probes arrive every few seconds and answer in microseconds, which
		// would drag every aggregate percentile toward zero.
		if route := routeLabel(r); route != "/ping" {
			httpRequestDuration.WithLabelValues(route).Observe(durMs / 1000)
		}
		m.logAccess(r, bw, uncompressedLen, start, calls)
	}
}

// logAccess writes one access-log line.
func (m *Middleware) logAccess(r *http.Request, bw *bufWriter, length int, start time.Time, calls *reqlog.Collector) {
	fields := []logger.KV{
		{K: "status", V: logger.Int(bw.status)},
	}
	// On a 4xx/5xx, log the error the response is rendering (e.g. the
	// OutOfRangeError's "No calendar for year 38") under "msg". The error
	// helpers record the value into the request's collector (see RecordError);
	// we render it here and place it right after status.
	if err := calls.Err(); err != nil {
		fields = append(fields, logger.KV{K: "msg", V: logger.String(err.Error())})
	}
	if length > 0 {
		fields = append(fields, logger.KV{K: "length", V: logger.Int(length)})
	}
	fields = append(fields,
		logger.KV{K: "duration", V: logger.Int(int(time.Since(start).Milliseconds()))},
		logger.KV{K: "ip", V: logger.String(ClientIP(r))},
		logger.KV{K: "method", V: logger.String(r.Method)},
		logger.KV{K: "host", V: logger.String(r.Host)},
		logger.KV{K: "url", V: logger.String(r.URL.RequestURI())},
		logger.KV{K: "ua", V: logger.String(r.Header.Get("User-Agent"))},
	)
	// A base64 PDF download URL (the /v4/ protobuf or the /v2/h/ query string) is
	// opaque in the logged url; the handler records the query string it decodes
	// to, which reproduces the request.
	if qs := calls.Query(); qs != "" {
		fields = append(fields, logger.KV{K: "qs", V: logger.String(qs)})
	}
	// The JSON-RPC body of a `POST /mcp` request, recorded by the mcp handler,
	// so the log shows which tool was called with what arguments.
	if pb := encodePostBody(calls.PostBody()); pb != nil {
		fields = append(fields, logger.KV{K: "postBody", V: pb})
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		fields = append(fields, logger.KV{K: "inm", V: logger.String(inm)})
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		fields = append(fields, logger.KV{K: "ims", V: logger.String(ims)})
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		fields = append(fields, logger.KV{K: "ref", V: logger.String(ref)})
	}
	if enc := bw.header.Get("Content-Encoding"); enc != "" {
		fields = append(fields, logger.KV{K: "enc", V: logger.String(enc)})
	}
	// A PDF request answered with the daily-learning fallback header records
	// which series were involved, so those requests can be found in the log.
	if un := bw.header.Get("X-Unsupported-Series"); un != "" {
		fields = append(fields, logger.KV{K: "unsupported", V: logger.String(un)})
	}
	// Fold any backend sub-requests this request made into a nested field: one
	// object for a single call, an array if the handler made several.
	if rs := encodeSubrequests(calls.Calls()); rs != nil {
		fields = append(fields, logger.KV{K: "subreq", V: rs})
	}
	level := logger.LevelInfo
	if bw.status >= 400 && bw.status != 404 {
		level = logger.LevelWarn
	}
	m.Logger.Write(level, fields)
}

// maxLoggedPostBody caps the postBody log field. JSON-RPC calls to /mcp are
// small (a method name plus a handful of arguments); the cap only guards
// against an oversized or pathological body bloating the log line.
const maxLoggedPostBody = 8192

// encodePostBody renders the recorded request body as the "postBody" log value.
// Valid JSON (the normal JSON-RPC case) is compacted so it embeds as a nested
// object on the single log line, exactly as the client sent it; anything else,
// or an over-cap body, is emitted as a bounded JSON string so the log line
// stays valid JSON either way. nil raw (no body recorded) omits the field.
func encodePostBody(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}
	if json.Valid(raw) {
		var compact bytes.Buffer
		if json.Compact(&compact, raw) == nil && compact.Len() <= maxLoggedPostBody {
			return compact.Bytes()
		}
	}
	if len(raw) > maxLoggedPostBody {
		raw = raw[:maxLoggedPostBody]
	}
	return logger.String(string(raw))
}

// encodeSubrequests renders the backend sub-requests as the log field value: a
// single {status,url,duration,length} object for one call, a JSON array for
// several, and nil (the field is omitted) for none. Duration is a float in
// milliseconds, emitted in its shortest round-tripping form.
func encodeSubrequests(calls []reqlog.Call) []byte {
	if len(calls) == 0 {
		return nil
	}
	var b bytes.Buffer
	if len(calls) > 1 {
		b.WriteByte('[')
	}
	for i, c := range calls {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"status":`)
		b.Write(logger.Int(c.Status))
		b.WriteString(`,"url":`)
		b.Write(logger.String(c.URL))
		b.WriteString(`,"duration":`)
		durMs := float64(c.Duration.Nanoseconds()) / 1e6
		// -1 precision emits the shortest form that round-trips: 3.5, not 3.500.
		b.WriteString(strconv.FormatFloat(durMs, 'f', -1, 64))
		b.WriteString(`,"length":`)
		b.Write(logger.Int(c.Length))
		b.WriteByte('}')
	}
	if len(calls) > 1 {
		b.WriteByte(']')
	}
	return b.Bytes()
}
