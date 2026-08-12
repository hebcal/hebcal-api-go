// Package logger writes pino-compatible JSON log lines, so the access log this
// service produces is interchangeable with the one hebcal-web produces.
package logger

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// Log levels, matching pino's numeric levels.
const (
	LevelInfo = 30
	LevelWarn = 40
)

// KV is one log field: a key and its already-encoded JSON value.
type KV struct {
	K string
	V []byte
}

// AccessLogger writes pino-compatible JSON log lines and supports reopening
// the log file on SIGHUP/SIGUSR1 for logrotate.
type AccessLogger struct {
	mu       sync.Mutex
	path     string
	f        *os.File
	out      io.Writer
	hostname string
	pid      int
}

// New returns an AccessLogger writing to path, or to stdout when path is empty
// or "-".
func New(path string) (*AccessLogger, error) {
	hostname, _ := os.Hostname()
	lg := &AccessLogger{path: path, out: os.Stdout, hostname: hostname, pid: os.Getpid()}
	if path != "" && path != "-" {
		if err := lg.Reopen(); err != nil {
			return nil, err
		}
	}
	return lg, nil
}

// NewWriter returns an AccessLogger writing to out. It is intended for tests,
// which discard the log rather than opening a file.
func NewWriter(out io.Writer, hostname string) *AccessLogger {
	return &AccessLogger{out: out, hostname: hostname, pid: os.Getpid()}
}

// Reopen closes and reopens the log file (called on SIGHUP/SIGUSR1).
func (lg *AccessLogger) Reopen() error {
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

// Write emits one JSON log line. Field order matches pino: level, time, pid,
// hostname, then the supplied fields.
func (lg *AccessLogger) Write(level int, fields []KV) {
	var buf bytes.Buffer
	buf.WriteString(`{"level":`)
	buf.WriteString(strconv.Itoa(level))
	buf.WriteString(`,"time":`)
	buf.WriteString(strconv.FormatInt(time.Now().UnixMilli(), 10))
	buf.WriteString(`,"pid":`)
	buf.WriteString(strconv.Itoa(lg.pid))
	buf.WriteString(`,"hostname":`)
	buf.Write(String(lg.hostname))
	for _, f := range fields {
		buf.WriteByte(',')
		buf.Write(String(f.K))
		buf.WriteByte(':')
		buf.Write(f.V)
	}
	buf.WriteString("}\n")
	lg.mu.Lock()
	defer lg.mu.Unlock()
	lg.out.Write(buf.Bytes())
}

// String encodes a string as a JSON log value.
func String(s string) []byte {
	// jsutil.Marshal avoids the & escaping of & that json.Marshal applies
	return jsutil.Marshal(s)
}

// Int encodes an int as a JSON log value.
func Int(n int) []byte {
	return []byte(strconv.Itoa(n))
}

// Info logs a startup/shutdown style message.
func (lg *AccessLogger) Info(msg string) {
	lg.Write(LevelInfo, []KV{{"msg", String(msg)}})
}

// Warn logs a warning-level message.
func (lg *AccessLogger) Warn(msg string) {
	lg.Write(LevelWarn, []KV{{"msg", String(msg)}})
}
