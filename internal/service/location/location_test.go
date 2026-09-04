package location

import (
	"net/url"
	"testing"
)

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

// TestFromLatLongTzidLeniency pins the two url-decoding hacks for a
// latitude/longitude request's tzid, ported from the "hack for client who
// passes" comments in hebcal-web src/location.js: a raw UTC offset like
// "+03:00" (whose "+" url-decodes to " "), and "Etc/GMT+5" (whose "+"
// likewise url-decodes to " ", landing as "Etc/GMT 5").
func TestFromLatLongTzidLeniency(t *testing.T) {
	cases := []struct{ tzid, want string }{
		{"Etc/GMT+5", "Etc/GMT+5"},   // untouched, already valid
		{"Etc/GMT 5", "Etc/GMT+5"},   // "+" url-decoded to " "
		{"Etc/GMT 12", "Etc/GMT+12"}, // two-digit offset
		{" 03:00", "Etc/GMT+3"},      // "+03:00" url-decoded to " 03:00"
		{"-02:00", "Etc/GMT-2"},
	}
	for _, tc := range cases {
		q := url.Values{
			"latitude":  {"41.85"},
			"longitude": {"-87.65"},
			"tzid":      {tc.tzid},
		}
		loc, err := FromQuery(nil, q)
		if err != nil {
			t.Errorf("FromQuery(tzid=%q) unexpected error: %v", tc.tzid, err)
			continue
		}
		if loc == nil || loc.TimeZoneID != tc.want {
			t.Errorf("FromQuery(tzid=%q).TimeZoneID = %v, want %q", tc.tzid, loc, tc.want)
		}
	}
}
