// Package csv renders the Outlook-compatible CSV calendars served from
// download.hebcal.com/v4/<data>/<name>.csv, and from its legacy /v2/h/ and
// /hebcal/index.cgi/ spellings.
//
// A request is the same base64 protobuf a PDF download carries, decoded by the
// download package into the same Params and the same hebcal-go events; only the
// last step differs. Each event becomes one CSV row of nine columns, written
// with encoding/csv; newlines in a description are flattened to " / " first.
package csv

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
	"github.com/hebcal/hebcal-api-go/internal/service/shabbat"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// Header is the header row of an Outlook-compatible CSV document.
var Header = []string{"Subject", "Start Date", "Start Time", "End Date", "End Time", "All day event", "Description", "Show time as", "Location"}

// bom is the UTF-8 byte order mark every document starts with. Without it
// Excel decodes the file as Windows-1252, and even an English calendar is not
// ASCII: its subjects carry typographic apostrophes ("Sukkot III (CH’’M)").
const bom = "\xEF\xBB\xBF"

// Service holds everything a CSV request needs beyond the request itself.
type Service struct {
	// Geo resolves geonameid/ZIP locations. It may be nil, as it may be for
	// the other location-dependent routes.
	Geo *geodb.DB
	// Learning fetches the daily-learning series no Go schedule generates. Nil
	// when no readings-svc is configured, in which case those requests are
	// refused rather than rendered incomplete.
	Learning *download.LearningFetcher
}

// Calendar is a decoded request together with the events it generated.
type Calendar struct {
	Params *download.Params
	Events []event.CalEvent
	// Learning holds the daily-learning rows fetched from readings-svc for the
	// series hebcal-go cannot generate.
	Learning []readings.Item
}

// Prepare decodes a .csv download URL in any of its three shapes and
// generates the calendar's events. Its errors carry the same statuses as
// pdf.Service.Prepare's: a *download.NotFoundError is 404,
// *download.OutOfRangeError 410, *download.UnsupportedSeriesError 501 or 503,
// and anything else 400. Unlike a PDF, a calendar with no events is not an
// error: it is a header row alone.
func (s *Service) Prepare(ctx context.Context, u *url.URL) (*Calendar, error) {
	msg, err := download.Decode(ctx, u, ".csv")
	if err != nil {
		return nil, err
	}
	params, err := download.ParamsFromMessage(msg, s.Geo)
	if err != nil {
		return nil, err
	}
	missing := download.UnsupportedSeries(msg)
	params.Opts.NumYears = download.NumYears(msg, params, len(missing))
	events, err := download.GenerateCalEvents(params)
	if err != nil {
		return nil, model.Internal("render: %s", err.Error())
	}
	cal := &Calendar{Params: params, Events: events}
	if len(missing) > 0 {
		if s.Learning == nil {
			return nil, &download.UnsupportedSeriesError{Series: missing}
		}
		start, end := download.LearningRange(params)
		items, err := s.Learning.FetchItems(ctx, missing, params.LG, start, end)
		if err != nil {
			return nil, &download.UnsupportedSeriesError{Series: missing, Retryable: true, Err: err}
		}
		cal.Learning = items
	}
	return cal, nil
}

// row is one rendered line, with the keys that put it in calendar order.
type row struct {
	abs  int64
	rank int
	line []string
}

// Render writes the whole CSV document: the byte order mark, the header row,
// and one row per event, each line ending in CRLF.
func Render(cal *Calendar) []byte {
	p := cal.Params
	rows := make([]row, 0, len(cal.Events)+len(cal.Learning))
	for _, ev := range cal.Events {
		rank := hebcal.TimeOfDay(ev)
		if ev.GetFlags().Has(event.HEBREW_DATE) {
			rank = -1 // the date leads its day
		}
		rows = append(rows, row{
			abs:  ev.GetDate().Abs(),
			rank: rank,
			line: eventToCSV(ev, p),
		})
	}
	for i := range cal.Learning {
		it := &cal.Learning[i]
		if day, err := time.Parse("2006-01-02", it.Date); err == nil {
			hd := hdate.FromTime(day)
			rows = append(rows, row{abs: hd.Abs(), rank: hebcal.TimeOfDayLearning, line: learningToCSV(it, hd, p)})
		}
	}
	// A Hebrew-month calendar shows the Gregorian date as its alternate date,
	// which hebcal-go does not generate, ahead of each day's other events.
	if p.MonthMode != download.GregorianArabic && (p.AddAltDates || p.AddAltDatesForEvents) && len(rows) > 0 {
		rows = addGregorianDates(rows, p)
	}
	// hebcal-go already orders each day by time of day; the stable sort keeps
	// that and moves the fetched learning rows into the learning slot.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].abs != rows[j].abs {
			return rows[i].abs < rows[j].abs
		}
		return rows[i].rank < rows[j].rank
	})

	lines := make([][]string, len(rows))
	for i, r := range rows {
		lines[i] = r.line
	}
	return Document(lines)
}

// Document writes a whole CSV document from its rows: the byte order mark,
// the header row, then each row, every line ending in CRLF. A field is quoted
// only when it needs to be.
func Document(rows [][]string) []byte {
	var buf bytes.Buffer
	buf.WriteString(bom)
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	w.Write(Header)
	w.WriteAll(rows)
	return buf.Bytes()
}

// FormatDate writes a date the way the Start Date and End Date columns want
// it: month/day/year ("3/5/2027"), or day/month/year when euro is set, with
// the year padded to four digits.
func FormatDate(y int, m time.Month, d int, euro bool) string {
	year := strconv.Itoa(y)
	if len(year) < 4 {
		year = strings.Repeat("0", 4-len(year)) + year
	}
	if euro {
		return strconv.Itoa(d) + "/" + strconv.Itoa(int(m)) + "/" + year
	}
	return strconv.Itoa(int(m)) + "/" + strconv.Itoa(d) + "/" + year
}

// FlattenNewlines turns each newline in a description into " / ", which a
// CSV writer would otherwise have to quote across lines.
func FlattenNewlines(s string) string {
	return strings.ReplaceAll(s, "\n", " / ")
}

// addGregorianDates adds a Gregorian-date row on every day from the first row
// to the last (AddAltDates), or on every day that has a row
// (AddAltDatesForEvents).
func addGregorianDates(rows []row, p *download.Params) []row {
	days := make(map[int64]bool, len(rows))
	lo, hi := rows[0].abs, rows[0].abs
	for _, r := range rows {
		days[r.abs] = true
		lo, hi = min(lo, r.abs), max(hi, r.abs)
	}
	for abs := lo; abs <= hi; abs++ {
		if p.AddAltDates || days[abs] {
			hd := hdate.FromRD(abs)
			subj := download.GregorianDateText(hd, p.Locale)
			hebrew := ""
			if p.AppendHebrew {
				hebrew = download.GregorianDateText(hd, "he")
			}
			rows = append(rows, row{abs: abs, rank: -1, line: formatRow(hd, subj, hebrew, "", false, "", 3, "Jewish Holidays", p)})
		}
	}
	return rows
}

// category maps an event category to the Location column of an all-day row.
// A category mapped to "" leaves the column empty; one not listed falls
// through to the next category, and finally to "Jewish Holidays".
var category = map[string]string{
	"dafyomi":     "Daf Yomi",
	"mishnayomi":  "Mishna Yomi",
	"nachyomi":    "Nach Yomi",
	"yerushalmi":  "Yerushalmi Yomi",
	"hebdate":     "Hebrew Date",
	"holiday":     "Jewish Holidays",
	"mevarchim":   "",
	"molad":       "",
	"omer":        "",
	"parashat":    "Torah Reading",
	"roshchodesh": "Jewish Holidays",
	"user":        "Personal",
	"zmanim":      "",
}

// eventToCSV renders one event as the fields of a CSV row.
func eventToCSV(ev event.CalEvent, p *download.Params) []string {
	flags := ev.GetFlags()
	timed, isTimed := hebcal.AsTimedEvent(ev)
	subj := download.RenderSubject(ev, p.Locale, download.ShouldRenderBrief(ev, flags, isTimed))
	hebrew := ""
	if p.AppendHebrew {
		hebrew = download.RenderSubject(ev, "he", true)
	}

	timeStr := ""
	loc := "Jewish Holidays"
	if isTimed {
		timeStr = formatTime(timed.EventTime, p)
		if p.LocationShortName != "" {
			loc = p.LocationShortName
		}
	} else {
		for _, cat := range download.EventCategories(ev) {
			if l, ok := category[cat]; ok {
				loc = l
				break
			}
		}
	}

	showTimeAs := 3
	if isTimed || flags.Has(event.CHAG) {
		showTimeAs = 4
	}
	return formatRow(ev.GetDate(), subj, hebrew, timeStr, isTimed, memo(ev, flags, p), showTimeAs, loc, p)
}

// learningToCSV renders a daily-learning row fetched from readings-svc. Its
// title is already the brief form, its category names the series (never one
// the Location column maps), and its memo is the sidecar's.
func learningToCSV(it *readings.Item, hd hdate.HDate, p *download.Params) []string {
	subj := it.Title
	if subj == "" {
		subj = it.Hebrew
	}
	hebrew := ""
	if p.AppendHebrew {
		hebrew = it.Hebrew
	}
	return formatRow(hd, subj, hebrew, "", false, it.Memo, 3, "Jewish Holidays", p)
}

// formatRow assembles the nine columns. A timed event starts and ends at the
// same minute on the same day; an all-day event leaves the time and end date
// columns empty.
func formatRow(hd hdate.HDate, subj, hebrew, timeStr string, timed bool, memo string,
	showTimeAs int, loc string, p *download.Params) []string {
	y, m, d := hd.Greg()
	date := FormatDate(y, m, d, p.Euro)

	startTime, endDate, endTime, allDay := "", "", "", "true"
	if timed {
		startTime = timeStr
		endTime = timeStr
		endDate = date
		allDay = "false"
	}

	if hebrew != "" {
		subj += " / " + hebrew
	}
	memo = FlattenNewlines(memo)

	return []string{subj, date, startTime, endDate, endTime, allDay, memo, strconv.Itoa(showTimeAs), loc}
}

// formatTime renders a clock time with a " PM" / " AM" suffix in a 12-hour
// country ("6:56 PM") and as-is elsewhere ("18:56"). The request's hour12
// choice overrides the country.
func formatTime(t time.Time, p *download.Params) string {
	return event.ReformatTimeStr(t.Format("15:04"), " PM", countryCode(p), hour12(p))
}

// countryCode is the location's country, or Israel or the United States by
// schedule when the calendar has none.
func countryCode(p *download.Params) string {
	if p.Opts.Location != nil && p.Opts.Location.CountryCode != "" {
		return p.Opts.Location.CountryCode
	}
	if p.Opts.IL {
		return "IL"
	}
	return "US"
}

// hour12 turns the protobuf's 0/1/2 into the override ReformatTimeStr takes:
// nil follows the country, true forces 12-hour, false 24-hour.
func hour12(p *download.Params) *bool {
	var b bool
	switch p.Hour12 {
	case 1:
		b = true
	case 2:
		b = false
	default:
		return nil
	}
	return &b
}

// memo is the Description column.
//
// In priority order: the special Shabbat or Rosh Chodesh a weekly Torah
// portion coincides with (parshaMemo); the event's own memo -- the molad
// announced on Shabbat Mevarchim, the coming month of a Yom Kippur Katan, the
// weekly portion for erev-Shabbat candle-lighting; the first sentence of the
// holiday's description; and for a timed event, the event it belongs to
// ("Fast begins" belongs to "Tzom Gedaliah", erev-Yom-Tov candle-lighting to
// "Erev Sukkot").
func memo(ev event.CalEvent, flags event.HolidayFlags, p *download.Params) string {
	if flags.Has(event.PARSHA_HASHAVUA) {
		if s := parshaMemo(ev.GetDate(), p); s != "" {
			return s
		}
	}
	if s := ownMemo(ev, flags, p); s != "" {
		return s
	}
	if s := download.HolidayDescription(ev, flags, p.Locale, true); s != "" {
		return s
	}
	if linked := download.LinkedEvent(ev); linked != nil {
		return download.RenderSubject(linked, p.Locale, false)
	}
	return ""
}

// ownMemo is the memo an event carries by itself.
//
// The Kiddush Levana times are deliberately left out: that event's memo is on
// its way out of the website, so the CSV starts without it.
func ownMemo(ev event.CalEvent, flags event.HolidayFlags, p *download.Params) string {
	switch {
	case flags.Has(event.SHABBAT_MEVARCHIM):
		cc := ""
		if p.Opts.Location != nil {
			cc = p.Opts.Location.CountryCode
		}
		return shabbat.MevarchimMoladMemo(ev.GetDate(), strings.ToLower(p.Locale), cc, p.Opts.IL, hour12(p))
	case isKiddushLevana(ev):
		return ""
	}
	// Yom Kippur Katan's meaning (untranslated, as the website has it), the
	// coming portion or festival reading for erev-Shabbat candle-lighting, a
	// daily-learning reading's details.
	return model.FixMonthSpelling(event.Memo(ev, p.Locale))
}

func isTimedEv(ev event.CalEvent) bool {
	_, ok := hebcal.AsTimedEvent(ev)
	return ok
}

func isKiddushLevana(ev event.CalEvent) bool {
	_, ok := ev.(hebcal.KiddushLevanaEvent)
	return ok
}

// parshaMemo names what else is read on a Shabbat with a weekly portion: the
// special Shabbatot and Rosh Chodesh falling on it, joined with " + "
// ("Shabbat Shekalim + Rosh Chodesh Adar"), or -- when the next day begins a
// month -- "Machar Chodesh". It is far shorter than the portion's prose
// summary an iCalendar feed carries, which would not fit a spreadsheet cell.
func parshaMemo(hd hdate.HDate, p *download.Params) string {
	var names []string
	for _, h := range hebcal.GetHolidaysOnDate(hd, p.Opts.IL) {
		if h.Flags.HasAny(event.SPECIAL_SHABBAT | event.ROSH_CHODESH) {
			names = append(names, download.RenderSubject(h, p.Locale, false))
		}
	}
	if len(names) > 0 {
		return strings.Join(names, " + ")
	}
	if d := hd.Next().Day(); d == 30 || d == 1 {
		// "Shabbat Machar Chodesh" without its first word, in any language
		str := model.Gettext("Shabbat Machar Chodesh", p.Locale)
		if i := strings.Index(str, " "); i >= 0 {
			return str[i+1:]
		}
		return str
	}
	return ""
}
