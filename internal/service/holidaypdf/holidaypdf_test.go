package holidaypdf

import (
	"errors"
	"net/url"
	"testing"

	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
)

// The URL shapes holidayPdf.js accepts, and the three ways it refuses one. Note
// that hebcal-2026-2027.pdf is a *Hebrew* year: the year-index pages name a
// Hebrew year by the two Gregorian years it spans, and holidayPdf.js keys on
// the hyphen -- which it still sees because it never strips the extension.
func TestParseHolidayPDF(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantYear     int
		wantHebrew   bool
		wantErrIs    func(error) bool
		wantErrLabel string
	}{
		{name: "gregorian", path: "/holidays/hebcal-2026.pdf", wantYear: 2026},
		{name: "hebrew", path: "/holidays/hebcal-5787.pdf", wantYear: 5787, wantHebrew: true},
		{name: "gregorian span is a hebrew year", path: "/holidays/hebcal-2026-2027.pdf",
			wantYear: 5787, wantHebrew: true},
		{name: "earliest gregorian", path: "/holidays/hebcal-100.pdf", wantYear: 100},
		{name: "latest hebrew", path: "/holidays/hebcal-6759.pdf", wantYear: 6759, wantHebrew: true},
		{name: "gregorian il suffix", path: "/holidays/hebcal-2999-il.pdf", wantYear: 2999},
		{name: "hebrew il suffix", path: "/holidays/hebcal-5787-il.pdf", wantYear: 5787, wantHebrew: true},
		{name: "gregorian span il suffix is a hebrew year",
			path: "/holidays/hebcal-2026-2027-il.pdf", wantYear: 5787, wantHebrew: true},

		{name: "not a hebcal calendar", path: "/holidays/sukkot-2026.pdf",
			wantErrIs: isNotFound, wantErrLabel: "404"},
		{name: "non-numeric year", path: "/holidays/hebcal-foo.pdf",
			wantErrIs: isNotFound, wantErrLabel: "404"},
		{name: "year zero", path: "/holidays/hebcal-0.pdf",
			wantErrIs: isBadRequest, wantErrLabel: "400"},
		{name: "year past 32000", path: "/holidays/hebcal-32001.pdf",
			wantErrIs: isBadRequest, wantErrLabel: "400"},
		{name: "gregorian too early", path: "/holidays/hebcal-99.pdf",
			wantErrIs: isOutOfRange, wantErrLabel: "410"},
		{name: "gregorian too late", path: "/holidays/hebcal-3000.pdf",
			wantErrIs: isOutOfRange, wantErrLabel: "410"},
		{name: "hebrew too late", path: "/holidays/hebcal-6760.pdf",
			wantErrIs: isOutOfRange, wantErrLabel: "410"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.path, nil)
			if tt.wantErrIs != nil {
				if err == nil {
					t.Fatalf("Parse(%q) succeeded, want %s", tt.path, tt.wantErrLabel)
				}
				if !tt.wantErrIs(err) {
					t.Fatalf("Parse(%q) = %v, want %s", tt.path, err, tt.wantErrLabel)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.path, err)
			}
			if p.Opts.Year != tt.wantYear || p.Opts.IsHebrewYear != tt.wantHebrew {
				t.Errorf("year = %d (hebrew %v), want %d (hebrew %v)",
					p.Opts.Year, p.Opts.IsHebrewYear, tt.wantYear, tt.wantHebrew)
			}
			// Every one of these calendars carries the Hebrew date on the day
			// line and paginates by Gregorian month, whatever the year is.
			if !p.Opts.AddHebrewDates || p.MonthMode != pdf.GregorianArabic {
				t.Errorf("AddHebrewDates = %v, MonthMode = %v", p.Opts.AddHebrewDates, p.MonthMode)
			}
		})
	}
}

func isNotFound(err error) bool {
	var e *pdf.NotFoundError
	return errors.As(err, &e)
}

func isBadRequest(err error) bool {
	var e *BadRequestError
	return errors.As(err, &e)
}

func isOutOfRange(err error) bool {
	var e *pdf.OutOfRangeError
	return errors.As(err, &e)
}

// i=on is the only query parameter these calendars take. holidayPdf.js also
// resolves `lg`, but nothing on the website links a localized holiday PDF, so
// it is ignored and every calendar renders in English.
func TestHolidayQueryParams(t *testing.T) {
	tests := []struct {
		query  string
		wantIL bool
	}{
		{"", false},
		{"i=on", true},
		{"i=off", false},
		{"lg=h", false},
		{"lg=de&i=on", true},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			q, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Parse("/holidays/hebcal-2026.pdf", q)
			if err != nil {
				t.Fatal(err)
			}
			if p.Opts.IL != tt.wantIL {
				t.Errorf("IL = %v, want %v", p.Opts.IL, tt.wantIL)
			}
			if p.Locale != "en" || p.RTL {
				t.Errorf("locale = %q (rtl %v), want \"en\" (rtl false)", p.Locale, p.RTL)
			}
		})
	}
}

// The "-il" filename suffix is hebcal-web's newer spelling of the Israel
// schedule, and it must combine with "?i=on" rather than override it: a
// legacy "?i=on" link to the plain filename still has to work.
func TestHolidayPathILSuffix(t *testing.T) {
	tests := []struct {
		path   string
		query  string
		wantIL bool
	}{
		{"/holidays/hebcal-2026.pdf", "", false},
		{"/holidays/hebcal-2026-il.pdf", "", true},
		{"/holidays/hebcal-2026-il.pdf", "i=off", true},
		{"/holidays/hebcal-2026.pdf", "i=on", true},
	}
	for _, tt := range tests {
		t.Run(tt.path+"?"+tt.query, func(t *testing.T) {
			q, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Parse(tt.path, q)
			if err != nil {
				t.Fatal(err)
			}
			if p.Opts.IL != tt.wantIL {
				t.Errorf("IL = %v, want %v", p.Opts.IL, tt.wantIL)
			}
		})
	}
}

// The leading-digits parse is load-bearing: holidayPdf.js hands parseInt the
// filename with the extension still attached.
func TestLeadingInt(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"2026.pdf", 2026, true},
		{"5787.pdf", 5787, true},
		{"2026-2027.pdf", 2026, true},
		{"0.pdf", 0, true},
		{"", 0, false},
		{".pdf", 0, false},
		{"foo.pdf", 0, false},
	}
	for _, tt := range tests {
		got, ok := leadingInt(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("leadingInt(%q) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
