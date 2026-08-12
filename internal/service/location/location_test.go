package location

import "testing"

// TestLegacyTzToTzid pins the legacy numeric-timezone plus DST-rule mapping,
// including the reversed Etc/GMT sign convention.
func TestLegacyTzToTzid(t *testing.T) {
	cases := []struct{ tz, dst, want string }{
		{"2", "israel", "Asia/Jerusalem"},
		{"0", "none", "UTC"},
		{"-5", "none", "Etc/GMT-5"}, // reversed sign convention (UTC+5)
		{"3", "none", "Etc/GMT+3"},
		{"0", "eu", "Europe/London"},
		{"1", "eu", "Europe/Paris"},
		{"-5", "usa", "America/New_York"}, // tz*-1 => 5
		{"-8", "usa", "America/Los_Angeles"},
		{"99", "bogus", ""},
	}
	for _, tc := range cases {
		if got := legacyTzToTzid(tc.tz, tc.dst); got != tc.want {
			t.Errorf("legacyTzToTzid(%q,%q) = %q, want %q", tc.tz, tc.dst, got, tc.want)
		}
	}
}
