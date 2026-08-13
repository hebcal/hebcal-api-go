// Package holidaypdf resolves www.hebcal.com's /holidays/hebcal-<year>.pdf
// URLs, a port of hebcal-web's src/holidayPdf.js.
//
// It is a much smaller request than a /v4/ download: there is no protobuf, no
// location and no daily learning, only a year, an Israel flag and a language.
// Everything after those three is the same generator and the same renderer as
// the /v4/ calendars, so this package is URL parsing plus the handful of
// CalOptions holidayPdf.js sets -- Parse hands back a *pdf.Params and the
// service/pdf package does the rest.
package holidaypdf

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
)

// hebrewYearOffset converts a Gregorian year to the Hebrew year that begins in
// it, the 3761 in holidayPdf.js's `yearNum + 3761`.
const hebrewYearOffset = 3761

// BadRequestError marks input that is well-formed enough to parse but out of
// bounds, which holidayPdf.js answers with 400 rather than its 404 or 410.
type BadRequestError struct{ msg string }

func (e *BadRequestError) Error() string { return e.msg }

// leadingInt reads the digits at the front of s, the part of
// Number.parseInt(s, 10) holidayPdf.js depends on: it never strips the
// extension, so the string it parses is "2026.pdf". ok is false where JS would
// produce NaN.
func leadingInt(s string) (n int, ok bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		// More digits than an int holds, and far outside the supported range
		// either way; hand back something the range checks reject.
		return 1 << 40, true
	}
	return n, true
}

// Parse turns a /holidays/hebcal-<year>.pdf request into the Params the
// generator and renderer take. Port of the first half of holidayPdf.js.
//
// The errors it returns are the three holidayPdf.js throws: pdf.NotFoundError
// for a URL that is not a holiday calendar (404), BadRequestError for a year
// outside 1..32000 (400), and pdf.OutOfRangeError for a year with no calendar
// (410).
func Parse(rpath string, query url.Values) (*pdf.Params, error) {
	base := path.Base(rpath)
	if !strings.HasPrefix(base, "hebcal-") {
		return nil, pdf.NotFoundf("Invalid PDF URL format: %s", base)
	}
	// Deliberately keeps the ".pdf" on the string, as holidayPdf.js does: the
	// year is read with parseInt, which stops at the dot, and the hyphen test
	// below is unaffected by the suffix.
	year := base[len("hebcal-"):]
	yearNum, ok := leadingInt(year)
	if !ok {
		return nil, pdf.NotFoundf("Invalid holiday year: %s", year)
	}
	if yearNum < 1 || yearNum > 32000 {
		return nil, &BadRequestError{msg: fmt.Sprintf("Invalid year number: %d", yearNum)}
	}
	// A year at or above 3761 is a Hebrew year; so is the Gregorian-span form
	// the year-index pages link from, e.g. hebcal-2026-2027.pdf, which names
	// the Hebrew year beginning in the first of the two.
	isHebrewYear := yearNum >= hebrewYearOffset || strings.Contains(year, "-")
	calendarYear := yearNum
	if isHebrewYear && yearNum < hebrewYearOffset {
		calendarYear = yearNum + hebrewYearOffset
	}
	if !pdf.YearIsSupported(calendarYear, isHebrewYear) {
		return nil, &pdf.OutOfRangeError{Year: calendarYear}
	}

	p := &pdf.Params{
		// One page per Gregorian month even for a Hebrew year: holidayPdf.js
		// never sets hebrewMonths, so a 5787 calendar paginates from Rosh
		// Hashana through Elul across Gregorian pages.
		MonthMode: pdf.GregorianArabic,
		// Always English. holidayPdf.js resolves a `lg` parameter, but nothing on
		// www.hebcal.com links a localized holiday calendar -- the holiday and
		// year-index pages emit `hebcal-<year>.pdf` with at most `?i=on` -- and
		// the access log has no other form, so `lg` is ignored here rather than
		// carried through the renderer. A stray `?lg=` still renders, in English.
		Locale: "en",
		// The Hebrew date is drawn on the day-number line in every one of these
		// calendars; holidayPdf.js hard-codes addHebrewDates.
		AddAltDates: true,
		// holidayPdf.js leaves options.utmCampaign unset, so renderPdfEvent
		// falls back to the event's own Hebrew year for the campaign.
		PerEventCampaign: true,
	}
	p.Opts.Year = calendarYear
	p.Opts.IsHebrewYear = isHebrewYear
	p.Opts.IL = query.Get("i") == "on"
	p.Opts.AddHebrewDates = true
	return p, nil
}
