package config

import (
	"bufio"
	"os"
	"strings"
)

// DefaultINIFile is the site-wide configuration file shared with the rest of
// the Hebcal.com fleet. This service reads only the hebcal.mysql.* keys from
// it, for the saved Yahrzeit calendars.
const DefaultINIFile = "/etc/hebcal-dot-com.ini"

// ReadINI reads a flat key=value file: one setting per line, blank lines and
// lines starting with ';' or '#' ignored, a [section] header ignored (every
// key in the file is already fully qualified, "hebcal.mysql.host"), and a
// value wrapped in matching quotes unwrapped. Whitespace around the key and
// the value is trimmed. A repeated key keeps its last value.
func ReadINI(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == ';' || line[0] == '#' || line[0] == '[' {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if n := len(val); n >= 2 && (val[0] == '"' || val[0] == '\'') && val[n-1] == val[0] {
			val = val[1 : n-1]
		}
		m[key] = val
	}
	return m, sc.Err()
}
