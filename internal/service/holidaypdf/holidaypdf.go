// Package holidaypdf resolves www.hebcal.com's /holidays/hebcal-<year>.pdf
// URLs.
//
// It is a much smaller request than a /v4/ download: there is no protobuf, no
// location and no daily learning, only a year and an Israel flag. Everything
// after those is the same generator and the same renderer as the /v4/
// calendars, so this package is URL parsing plus a handful of CalOptions --
// Parse hands back a *pdf.Params and the service/pdf package does the rest.
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
// it.
const hebrewYearOffset = 3761

// BadRequestError marks input that is well-formed enough to parse but out of
// bounds, answered with 400 rather than a 404 or 410.
type BadRequestError struct{ msg string }

func (e *BadRequestError) Error() string { return e.msg }

// leadingInt reads the digits at the front of s. The extension is never
// stripped, so the string parsed here is "2026.pdf". ok is false when s does
// not start with a digit.
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
// generator and renderer take.
//
// It returns three kinds of error: pdf.NotFoundError for a URL that is not a
// holiday calendar (404), BadRequestError for a year outside 1..32000 (400),
// and pdf.OutOfRangeError for a year with no calendar (410).
//
// The Israel schedule is linked as a "-il" filename suffix (hebcal-2999-il.pdf)
// rather than a "?i=on" query parameter, so a browser bookmark or a shared link
// names the schedule in the path instead of a query string that a download
// manager or link-preview tool tends to drop. The "?i=on" spelling still
// reaches this handler from links published before that change, so it keeps
// setting IL too -- either one is enough.
func Parse(rpath string, query url.Values) (*pdf.Params, error) {
	base := path.Base(rpath)
	if !strings.HasPrefix(base, "hebcal-") {
		return nil, pdf.NotFoundf("Invalid PDF URL format: %s", base)
	}
	// Deliberately keeps the ".pdf" on the string: the year is read with
	// leadingInt, which stops at the dot, and the hyphen test below is
	// unaffected by the suffix.
	year := base[len("hebcal-"):]
	// A "-il" suffix just ahead of ".pdf" (hebcal-2999-il.pdf) is the newer
	// spelling of the Israel schedule, replacing the "?i=on" query parameter.
	// It is stripped before the year is parsed so it never confuses leadingInt
	// or the Hebrew-year hyphen test, and before il is known so the query
	// parameter -- which legacy links still carry -- is still honored.
	pathIL := false
	if trimmed, ok := strings.CutSuffix(year, "-il.pdf"); ok {
		pathIL = true
		year = trimmed + ".pdf"
	}
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
		return nil, &pdf.OutOfRangeError{Year: calendarYear, IsHebrewYear: isHebrewYear}
	}

	p := &pdf.Params{
		// One page per Gregorian month even for a Hebrew year, so a 5787
		// calendar paginates from Rosh Hashana through Elul across Gregorian
		// pages.
		MonthMode: pdf.GregorianArabic,
		// Always English. Nothing on www.hebcal.com links a localized holiday
		// calendar -- the holiday and year-index pages emit `hebcal-<year>.pdf`
		// with at most `?i=on` -- so `lg` is ignored here rather than carried
		// through the renderer. A stray `?lg=` still renders, in English.
		Locale: "en",
		// The Hebrew date is drawn on the day-number line in every one of these
		// calendars.
		AddAltDates: true,
		// No document-level campaign is set, so each event's link falls back to
		// the event's own Hebrew year for the campaign.
		PerEventCampaign: true,
	}
	p.Opts.Year = calendarYear
	p.Opts.IsHebrewYear = isHebrewYear
	p.Opts.IL = pathIL || query.Get("i") == "on"
	p.Opts.AddHebrewDates = true
	return p, nil
}
