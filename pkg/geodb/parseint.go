package geodb

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// parseInt mimics JavaScript parseInt(str, 10) for the numeric columns and ZIP
// prefixes this package parses. It is a copy of the shared helper in
// internal/jsutil, duplicated so this package stays free of internal
// dependencies; the two must keep the same semantics.
func parseInt(s string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		if errors.Is(err, strconv.ErrRange) {
			if strings.HasPrefix(strings.TrimSpace(s), "-") {
				return math.MinInt, true
			}
			return math.MaxInt, true
		}
		return 0, false
	}
	return n, true
}
