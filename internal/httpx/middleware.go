package httpx

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/reqlog"
)

var httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "http_requests_total",
	Help: "Total number of HTTP requests",
}, []string{"method", "status"})

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

// Middleware wraps route handlers with the response processing every route
// shares.
type Middleware struct {
	Logger *logger.AccessLogger
}

// Serve runs the handler with buffering, then applies gzip/brotli compression,
// response-time and length headers, Prometheus metrics, and access logging.
func (m *Middleware) Serve(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Seed a per-request collector so backend calls made deep in the handler
		// (readings-svc) can be folded into this request's single log line.
		ctx, calls := reqlog.NewContext(r.Context())
		bw := newBufWriter(calls)
		r = r.WithContext(ctx)
		h(bw, r)

		body := bw.buf.Bytes()
		uncompressedLen := len(body)
		contentType := bw.header.Get("Content-Type")
		if compressibleType(contentType) && bw.header.Get("Content-Encoding") == "" {
			compressed := false
			enc := negotiateEncoding(r)
			if uncompressedLen > CompressThreshold && enc != "" {
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
		m.logAccess(r, bw, uncompressedLen, start, calls)
	}
}

// logAccess writes one access-log line, similar to hebcal-web makeLogInfo().
func (m *Middleware) logAccess(r *http.Request, bw *bufWriter, length int, start time.Time, calls *reqlog.Collector) {
	fields := []logger.KV{
		{K: "status", V: logger.Int(bw.status)},
	}
	// On a 4xx/5xx, log the error the response is rendering (e.g. the
	// OutOfRangeError's "No calendar for year 38") under "msg", mirroring
	// pino/koa. The error helpers record the value into the request's collector
	// (see RecordError); we render it here and place it right after status, as
	// hebcal-web's log does.
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
