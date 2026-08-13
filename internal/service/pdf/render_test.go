package pdf

import (
	"testing"
	"time"

	"github.com/hebcal/hebcal-go/event"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// rowsFor is src/pdf.js's rule, not a computed ceiling: the grid stays five
// rows unless the month genuinely cannot fit, which keeps cell heights
// consistent from page to page.
func TestRowsFor(t *testing.T) {
	tests := []struct {
		days, startDow, want int
	}{
		{31, 5, 6}, // 31 days starting Friday needs six rows
		{31, 6, 6}, // ... and starting Saturday
		{31, 4, 5}, // starting Thursday still fits in five
		{30, 6, 6}, // 30 days starting Saturday needs six
		{30, 5, 5}, // starting Friday fits
		{28, 0, 5}, // a short February
		{29, 6, 5},
	}
	for _, tt := range tests {
		if got := rowsFor(tt.days, tt.startDow); got != tt.want {
			t.Errorf("rowsFor(%d, %d) = %d, want %d", tt.days, tt.startDow, got, tt.want)
		}
	}
}

// Every month must fit: the last day cannot spill past the final row.
func TestEveryMonthFitsItsRows(t *testing.T) {
	for y := 2020; y <= 2030; y++ {
		for m := time.January; m <= time.December; m++ {
			first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
			days := first.AddDate(0, 1, -1).Day()
			startDow := int(first.Weekday())
			rows := rowsFor(days, startDow)
			if need := (startDow + days + 6) / 7; need > rows {
				t.Errorf("%d-%02d: %d days from dow %d needs %d rows, rowsFor gave %d",
					y, m, days, startDow, need, rows)
			}
		}
	}
}

// The grid rectangle runs from BMARGIN down to HEIGHT-TMARGIN in pdfkit's
// top-down coordinates, which is the opposite of what the constant names
// suggest. Anchoring it to the wrong edge put every day number in the wrong
// place.
func TestGridGeometry(t *testing.T) {
	if got, want := yLine(pdfBMargin), 540.0; got != want {
		t.Errorf("grid top in PDF coordinates = %v, want %v", got, want)
	}
	if got, want := yLine(pdfHeight-pdfTMargin), 32.0; got != want {
		t.Errorf("grid bottom in PDF coordinates = %v, want %v", got, want)
	}
	if got, want := pdfColWidth*pdfColumns, pdfWidth-pdfLMargin-pdfRMargin; got != want {
		t.Errorf("columns span %v, want %v", got, want)
	}
}

// cellOrigin is what src/pdf.js passes to renderPdfEvent: the cell's left edge
// less the cell margin. Right-to-left calendars run the columns backwards.
func TestCellOrigin(t *testing.T) {
	ltr0 := cellOrigin(false, 0)
	ltr6 := cellOrigin(false, 6)
	if ltr0 >= ltr6 {
		t.Errorf("left-to-right columns should increase: dow0=%v dow6=%v", ltr0, ltr6)
	}
	rtl0 := cellOrigin(true, 0)
	rtl6 := cellOrigin(true, 6)
	if rtl0 <= rtl6 {
		t.Errorf("right-to-left columns should decrease: dow0=%v dow6=%v", rtl0, rtl6)
	}
	// Sunday is the rightmost column in a Hebrew calendar and the leftmost in
	// an English one, so the two extremes should mirror across the page.
	if got, want := rtl0+pdfColWidth, pdfWidth-pdfRMargin-pdfCellMargin; !approx(got, want, 0.001) {
		t.Errorf("RTL Sunday right edge = %v, want %v", got, want)
	}
}

func approx(a, b, tol float64) bool {
	d := a - b
	return d < tol && d > -tol
}

// The Hebrew-month subtitle names the start year only when the month spans a
// Hebrew year boundary, and the end month only when it differs from the start.
func TestHebMonthRange(t *testing.T) {
	tests := []struct {
		year  int
		month time.Month
		want  string
	}{
		{2026, time.August, "Av – Elul 5786"},
		{2026, time.September, "Elul 5786 – Tishrei 5787"},
	}
	for _, tt := range tests {
		mp := MonthPage{Year: tt.year, Month: tt.month}
		got := hebMonthRange(mp, &Params{Locale: "en"})
		if got != tt.want {
			t.Errorf("hebMonthRange(%d-%02d) = %q, want %q", tt.year, tt.month, got, tt.want)
		}
	}
}

// Sh'vat is spelled with a typewriter apostrophe in the tables and a right
// single quote in the rendered calendar.
func TestHebMonthRangeUsesSmartApostrophe(t *testing.T) {
	got := hebMonthRange(MonthPage{Year: 2027, Month: time.January}, &Params{Locale: "en"})
	for _, bad := range []string{"'"} {
		if contains(got, bad) {
			t.Errorf("hebMonthRange() = %q, should not contain a typewriter apostrophe", got)
		}
	}
}

// The Hebrew-month title year follows rtl, not gematriyaNumerals (mm=2): a
// non-Hebrew locale draws plain digits even in mm=2, matching pdfkit's
// `const yearStr = rtl ? gematriya(year) : year`. Keying it on useGematriya()
// put Hebrew year letters into the Latin title font as tofu boxes. The day
// numbers below the title stay in gematriya; only the title year changed.
func TestHebTitleYear(t *testing.T) {
	// mm=2 with lg=s: gematriya day numbers, but a plain-digit title year.
	if got := hebTitleYear(&Params{MonthMode: HebrewHebrew, RTL: false}, 5788); got != "5788" {
		t.Errorf("hebTitleYear(mm=2, non-RTL) = %q, want %q", got, "5788")
	}
	if !(&Params{MonthMode: HebrewHebrew}).useGematriya() {
		t.Fatal("mm=2 should still request gematriya day numbers")
	}
	// A Hebrew locale renders the year in gematriya: 5788 = תשפ״ח.
	if got := hebTitleYear(&Params{MonthMode: HebrewHebrew, RTL: true}, 5788); got != "תשפ״ח" {
		t.Errorf("hebTitleYear(RTL) = %q, want gematriya תשפ״ח", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// The Gregorian span under a Hebrew-month title has three shapes.
func TestGregRange(t *testing.T) {
	en := model.NamesFor("en")
	tests := []struct {
		start, end time.Time
		want       string
	}{
		{
			time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			"Dec 3 – 31, 2026",
		},
		{
			time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 12, 27, 0, 0, 0, 0, time.UTC),
			"Nov 28 – Dec 27, 2026",
		},
	}
	for _, tt := range tests {
		if got := gregRange(tt.start, tt.end, en); got != tt.want {
			t.Errorf("gregRange() = %q, want %q", got, tt.want)
		}
	}
}

// eventColor's tests are ordered: an event can carry several flags and the
// first match wins, which is what keeps a Rosh Chodesh that falls on a fast
// day from being coloured as a fast.
func TestEventColorPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		flags event.HolidayFlags
		want  string
	}{
		{"daily learning", event.DAF_YOMI, "#666666"},
		{"rosh chodesh", event.ROSH_CHODESH, "#660099"},
		{"minor fast", event.MINOR_FAST, "#FF3300"},
		{"parsha", event.PARSHA_HASHAVUA, "#009900"},
		{"special shabbat", event.SPECIAL_SHABBAT, "#006699"},
		{"chag", event.CHAG, "#990000"},
		{"plain", 0, "#000000"},
		{"learning wins over rosh chodesh", event.DAF_YOMI | event.ROSH_CHODESH, "#666666"},
		{"rosh chodesh wins over chag", event.ROSH_CHODESH | event.CHAG, "#660099"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := eventColor(tt.flags), rgb(tt.want); got != want {
				t.Errorf("eventColor(0x%x) = %v, want %v (%s)", tt.flags, got, want, tt.want)
			}
		})
	}
}

// The break point is not the middle of the string. renderPdfEvent looks one
// element past the midpoint of a split that keeps its separators, which puts
// the extra word on the first line for an even number of words -- production
// draws "Yom HaAliyah School" / "Observance" -- while a right-to-left subject,
// rejoined with two spaces before the same split, breaks in the middle. With
// fewer than three words a left-to-right subject finds no break at all.
func TestSplitInTwo(t *testing.T) {
	tests := []struct {
		subject string
		rtl     bool
		want    []string
	}{
		{"Yom HaAliyah School Observance", false, []string{"Yom HaAliyah School", "  Observance"}},
		{"Rosh Chodesh Adar II", false, []string{"Rosh Chodesh Adar", "  II"}},
		{"Rosh Hashana LaBehemot", false, []string{"Rosh Hashana", "  LaBehemot"}},
		{"Yom HaZikaron", false, []string{"Yom HaZikaron"}},
		{"Chanukah", false, []string{"Chanukah"}},
		{"Yom HaAliyah School Observance", true, []string{"Yom HaAliyah", "  School Observance"}},
		{"Rosh Hashana LaBehemot", true, []string{"Rosh Hashana", "  LaBehemot"}},
		{"Yom HaZikaron", true, []string{"Yom", "  HaZikaron"}},
		{"Chanukah", true, []string{"Chanukah"}},
	}
	for _, tt := range tests {
		got := splitInTwo(tt.subject, tt.rtl)
		if len(got) != len(tt.want) {
			t.Errorf("splitInTwo(%q, rtl=%v) = %q, want %q", tt.subject, tt.rtl, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitInTwo(%q, rtl=%v) = %q, want %q", tt.subject, tt.rtl, got, tt.want)
				break
			}
		}
	}
}

// The footer names the location and its candle-lighting offset, or the
// schedule when there is no location.
func TestLeftFooterText(t *testing.T) {
	none := leftFooterText(&Params{})
	if none != "Diaspora holiday schedule" {
		t.Errorf("no location: %q", none)
	}
	var ilParams Params
	ilParams.Opts.IL = true
	if il := leftFooterText(&ilParams); il != "Israel holiday schedule" {
		t.Errorf("Israel: %q", il)
	}
}

// hdate spells one month differently from @hebcal/core, and the published
// calendars follow @hebcal/core.
func TestTamuzSpelling(t *testing.T) {
	// July 2027 spans Tamuz-Av 5787.
	got := hebMonthRange(MonthPage{Year: 2027, Month: time.July}, &Params{Locale: "en"})
	if !contains(got, "Tamuz") || contains(got, "Tammuz") {
		t.Errorf("hebMonthRange() = %q, want it to say Tamuz with one m", got)
	}
}
