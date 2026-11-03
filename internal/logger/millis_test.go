package logger

import (
	"testing"
	"time"
)

func TestMillis(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{321 * time.Microsecond, "0.321"},
		{321499 * time.Nanosecond, "0.321"},
		{321500 * time.Nanosecond, "0.322"},
		{3500 * time.Microsecond, "3.5"},
		{7 * time.Millisecond, "7"},
		{0, "0"},
		{12345678 * time.Microsecond, "12345.678"},
	} {
		if got := string(Millis(tc.d)); got != tc.want {
			t.Errorf("Millis(%v) = %s, want %s", tc.d, got, tc.want)
		}
	}
}
