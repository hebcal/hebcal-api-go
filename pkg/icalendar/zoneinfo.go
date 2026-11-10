package icalendar

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ZoneInfo reads VTIMEZONE definitions from a directory tree laid out by
// time zone identifier, <dir>/America/Los_Angeles.ics, which is what the vzic
// tool generates from the IANA database. Each file is a whole VCALENDAR
// holding one VTIMEZONE.
//
// A definition is read once and kept. A nil *ZoneInfo has no definitions.
type ZoneInfo struct {
	dir   string
	cache sync.Map // tzid -> string
}

// NewZoneInfo returns the definitions under dir.
func NewZoneInfo(dir string) *ZoneInfo {
	return &ZoneInfo{dir: dir}
}

// VTimezone returns the VTIMEZONE component for tzid -- its lines from
// BEGIN:VTIMEZONE through END:VTIMEZONE, CRLF-separated, with no trailing
// line break -- and whether there is one. A zone with no file, or an
// identifier that is not a plausible one, has none; the calendar is then
// written without it and applications fall back on their own definition.
func (z *ZoneInfo) VTimezone(tzid string) (string, bool) {
	if z == nil || z.dir == "" || !validTZID(tzid) {
		return "", false
	}
	if v, ok := z.cache.Load(tzid); ok {
		return v.(string), true
	}
	data, err := os.ReadFile(filepath.Join(z.dir, filepath.FromSlash(tzid)+".ics"))
	if err != nil {
		return "", false
	}
	vtz, ok := extractVTimezone(string(data))
	if !ok {
		return "", false
	}
	z.cache.Store(tzid, vtz)
	return vtz, true
}

// validTZID accepts identifiers shaped like the IANA database's, and so keeps
// one taken from a request from naming a file outside the directory.
func validTZID(tzid string) bool {
	if tzid == "" || len(tzid) > 64 || tzid[0] == '/' || strings.Contains(tzid, "..") {
		return false
	}
	for i := 0; i < len(tzid); i++ {
		c := tzid[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '/', c == '_', c == '-', c == '+':
		default:
			return false
		}
	}
	return true
}

// extractVTimezone cuts the VTIMEZONE component out of a VCALENDAR file.
func extractVTimezone(contents string) (string, bool) {
	var out []string
	inside := false
	for _, ln := range strings.Split(contents, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if ln == "BEGIN:VTIMEZONE" {
			inside = true
		}
		if inside {
			out = append(out, ln)
		}
		if ln == "END:VTIMEZONE" {
			return strings.Join(out, crlf), inside
		}
	}
	return "", false
}
