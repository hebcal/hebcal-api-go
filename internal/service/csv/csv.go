// Package csv renders the Outlook-compatible CSV calendars served from
// download.hebcal.com/v4/<data>/<name>.csv.
//
// A request is the same base64 protobuf a PDF download carries, decoded by the
// download package into the same Params and the same hebcal-go events; only the
// last step differs. Each event becomes one CSV row of nine quoted columns,
// built by hand rather than with encoding/csv: commas are dropped from the
// subject and turned into semicolons in the description, double quotes become
// two single quotes, and every field is quoted, so no row ever needs the
// escaping a CSV writer would add.
package csv

import (
	"bytes"
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/sedra"
	"github.com/hebcal/locales"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
	"github.com/hebcal/hebcal-api-go/internal/service/shabbat"
	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// Header is the header row of an Outlook-compatible CSV document.
const Header = `"Subject","Start Date","Start Time","End Date","End Time","All day event","Description","Show time as","Location"`

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

// Prepare decodes a /v4/<data>/<name>.csv path and generates the calendar's
// events. Its errors carry the same statuses as pdf.Service.Prepare's: a
// *download.NotFoundError is 404, *download.OutOfRangeError 410,
// *download.UnsupportedSeriesError 501 or 503, and anything else 400. Unlike a PDF,
// a calendar with no events is not an error: it is a header row alone.
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
	params.Opts.NumYears = numYears(msg, params, len(missing))
	events, err := download.GenerateCalEvents(params)
	if err != nil {
		return nil, model.Internal("render: %s", err.Error())
	}
	cal := &Calendar{Params: params, Events: events}
	if len(missing) > 0 {
		if s.Learning == nil {
			return nil, &download.UnsupportedSeriesError{Series: missing}
		}
		start, end := learningRange(params)
		items, err := s.Learning.FetchItems(ctx, missing, params.LG, start, end)
		if err != nil {
			return nil, &download.UnsupportedSeriesError{Series: missing, Retryable: true, Err: err}
		}
		cal.Learning = items
	}
	return cal, nil
}

// numYears is how many years a CSV calendar spans. A download that names no
// span gets several years, fewer the more events each year carries, so that
// the file stays a reasonable size: 7 by default, down to 4 with
// candle-lighting, 2 with alternate dates every day or with any daily
// learning, and 1 with two or more daily series. An explicit span is honoured
// up to 10 years. The PDF calendars do not do this; they draw one year unless
// asked for more.
func numYears(msg *downloadpb.Download, p *download.Params, numUnsupported int) int {
	if n := msg.GetNumYears(); n > 0 {
		return min(int(n), 10)
	}
	o := &p.Opts
	n := 7
	if o.Omer && o.Sedrot {
		n = 4 // omer + sedrot adds 101 events
	}
	if o.CandleLighting {
		if p.AddAltDates {
			n = 2
		} else if p.AddAltDatesForEvents {
			n = 3
		}
	}
	numLearning := len(o.DailyLearning) + numUnsupported
	for _, cap := range []struct {
		on bool
		ny int
	}{
		{o.CandleLighting, 4},
		{o.Sedrot, 5},
		{o.Omer, 5},
		{p.AddAltDatesForEvents, 4},
		{p.AddAltDates, 2},
		{numLearning > 0, 2},
	} {
		if cap.on && cap.ny < n {
			n = cap.ny
		}
	}
	daily := numLearning
	if p.AddAltDates {
		daily++
	}
	if daily > 1 {
		n = 1
	}
	return n
}

// learningRange is the span to fetch daily learning for: the calendar's own
// date range, the same days hebcal-go generates the other series over. A
// whole Hebrew year starts on Erev Rosh Hashana, a Gregorian one may be a
// single month, and a Hebrew-month calendar also carries the following
// Tishrei.
func learningRange(p *download.Params) (time.Time, time.Time) {
	o := &p.Opts
	if o.Start.Abs() != 0 && o.End.Abs() != 0 {
		return o.Start.Gregorian(), o.End.Gregorian()
	}
	year := o.Year
	if year == 0 {
		now := time.Now()
		if o.IsHebrewYear {
			year = hdate.FromTime(now).Year()
		} else {
			year = now.Year()
		}
	}
	n := max(o.NumYears, 1)
	if o.IsHebrewYear {
		first := hdate.New(year, hdate.Tishrei, 1).Gregorian()
		if year > 1 {
			first = first.AddDate(0, 0, -1)
		}
		last := hdate.New(year+n, hdate.Tishrei, 1).Gregorian().AddDate(0, 0, -1)
		if p.MonthMode != download.GregorianArabic {
			last = hdate.New(year+n, hdate.Tishrei, 30).Gregorian()
		}
		return first, last
	}
	if o.Month >= time.January && o.Month <= time.December {
		first := time.Date(year, o.Month, 1, 0, 0, 0, 0, time.UTC)
		return first, first.AddDate(0, 1, -1)
	}
	first := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(year+n-1, time.December, 31, 0, 0, 0, 0, time.UTC)
	return first, last
}

// row is one rendered line, with the keys that put it in calendar order.
type row struct {
	abs  int64
	rank int
	line string
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

	var buf bytes.Buffer
	buf.WriteString(bom)
	buf.WriteString(Header)
	buf.WriteString("\r\n")
	for _, r := range rows {
		buf.WriteString(r.line)
		buf.WriteString("\r\n")
	}
	return buf.Bytes()
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

// eventToCSV renders one event as a CSV row, without the line ending.
func eventToCSV(ev event.CalEvent, p *download.Params) string {
	flags := ev.GetFlags()
	timed, isTimed := download.AsTimedEvent(ev)
	subj := renderSubject(ev, p.Locale, shouldRenderBrief(ev, flags, isTimed))
	hebrew := ""
	if p.AppendHebrew {
		hebrew = renderSubject(ev, "he", true)
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
func learningToCSV(it *readings.Item, hd hdate.HDate, p *download.Params) string {
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
	showTimeAs int, loc string, p *download.Params) string {
	y, m, d := hd.Greg()
	year := strconv.Itoa(y)
	if len(year) < 4 {
		year = strings.Repeat("0", 4-len(year)) + year
	}
	var date string
	if p.Euro {
		date = `"` + strconv.Itoa(d) + "/" + strconv.Itoa(int(m)) + "/" + year + `"`
	} else {
		date = `"` + strconv.Itoa(int(m)) + "/" + strconv.Itoa(d) + "/" + year + `"`
	}

	startTime, endDate, endTime, allDay := "", "", "", `"true"`
	if timed {
		startTime = `"` + timeStr + `"`
		endTime = startTime
		endDate = date
		allDay = `"false"`
	}

	subj = strings.ReplaceAll(strings.ReplaceAll(subj, ",", ""), `"`, "''")
	if hebrew != "" {
		subj += " / " + hebrew
	}
	memo = strings.ReplaceAll(memo, ",", ";")
	memo = strings.ReplaceAll(memo, `"`, "''")
	memo = strings.ReplaceAll(memo, "\n", " / ")

	var b strings.Builder
	b.WriteString(`"` + subj + `",`)
	b.WriteString(date + ",")
	b.WriteString(startTime + ",")
	b.WriteString(endDate + ",")
	b.WriteString(endTime + ",")
	b.WriteString(allDay + ",")
	b.WriteString(`"` + memo + `",`)
	b.WriteString(`"` + strconv.Itoa(showTimeAs) + `",`)
	b.WriteString(`"` + loc + `"`)
	return b.String()
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

// learningFlags are the daily-learning series flags.
const learningFlags = event.DAF_YOMI | event.MISHNA_YOMI |
	event.NACH_YOMI | event.YERUSHALMI_YOMI | event.DAILY_LEARNING

// shouldRenderBrief reports whether an event's subject is its brief form:
// timed events (the time has a column of its own), Hebrew dates other than the
// first of a month, daily learning, Shabbat Mevarchim and Yom Kippur Katan.
func shouldRenderBrief(ev event.CalEvent, flags event.HolidayFlags, timed bool) bool {
	switch {
	case timed:
		return true
	case flags.Has(event.HEBREW_DATE):
		return ev.GetDate().Day() != 1
	case flags.HasAny(learningFlags | event.SHABBAT_MEVARCHIM):
		return true
	case flags.Has(event.MINOR_FAST):
		return strings.HasPrefix(untranslatedDesc(ev), "Yom Kippur Katan")
	}
	return false
}

// renderSubject renders an event's title in locale, in its brief or full form.
// Rosh Hashana renders the year as a number in every locale, Hebrew included.
func renderSubject(ev event.CalEvent, locale string, brief bool) string {
	var s string
	hd := ev.GetDate()
	switch he, isHoliday := ev.(event.HolidayEvent); {
	case isHoliday && hd.Month() == hdate.Tishrei && hd.Day() == 1 &&
		strings.HasPrefix(he.Desc, "Rosh Hashana"):
		s = model.Gettext("Rosh Hashana", locale) + " " + strconv.Itoa(hd.Year())
	case brief:
		s = event.RenderBrief(ev, locale)
	default:
		s = ev.Render(locale)
	}
	s = stripEmoji(s)
	// Daily-learning readings keep their typewriter apostrophes ("Fourth
	// Year's Fruit"); only the calendar's own names are typeset.
	if isHebrew(locale) || ev.GetFlags().HasAny(learningFlags) {
		return model.FixMonthSpelling(s)
	}
	return model.FixMonthSpelling(jsutil.SmartApostrophe(s))
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
	if s := holidayDescription(ev, flags, p.Locale); s != "" {
		return s
	}
	if linked := linkedEvent(ev); linked != nil {
		return renderSubject(linked, p.Locale, false)
	}
	return ""
}

// ownMemo is the memo an event carries by itself.
//
// The Kiddush Levana times are deliberately left out: that event's memo is on
// its way out of the website, so the CSV starts without it.
func ownMemo(ev event.CalEvent, flags event.HolidayFlags, p *download.Params) string {
	hd := ev.GetDate()
	switch {
	case flags.Has(event.SHABBAT_MEVARCHIM):
		cc := ""
		if p.Opts.Location != nil {
			cc = p.Opts.Location.CountryCode
		}
		return shabbat.MevarchimMoladMemo(hd, strings.ToLower(p.Locale), cc, p.Opts.IL, hour12(p))
	case flags.Has(event.YOM_KIPPUR_KATAN) && !isTimedEv(ev):
		// untranslated, as the website has it
		month := strings.TrimPrefix(untranslatedDesc(ev), "Yom Kippur Katan ")
		return "Minor Day of Atonement on the day preceeding Rosh Chodesh " + month
	}
	if timed, ok := ev.(hebcal.TimedEvent); ok && timed.Desc == "Candle lighting" &&
		hd.Weekday() == time.Friday && p.Opts.Sedrot {
		// Erev-Shabbat candle-lighting names the coming Shabbat's portion --
		// or, when a festival displaces it, the festival. Candle-lighting
		// before a Yom Tov that falls on Friday night links to the holiday
		// instead, and gets the holiday's name below.
		if timed.LinkedEvent == nil || isParsha(timed.LinkedEvent) {
			sy := sedra.New(hd.Year(), p.Opts.IL)
			parsha := sy.LookupByRD(hd.Abs())
			if parsha.Chag {
				return model.Gettext(parsha.Name[0], p.Locale)
			}
			if timed.LinkedEvent != nil {
				return renderSubject(timed.LinkedEvent, p.Locale, false)
			}
		}
	}
	if m, ok := ev.(interface{ Memo(string) string }); ok && !isKiddushLevana(ev) {
		return m.Memo(p.Locale)
	}
	return ""
}

// linkedEvent returns the event a timed event or Yizkor belongs to, or nil.
func linkedEvent(ev event.CalEvent) event.CalEvent {
	switch e := ev.(type) {
	case hebcal.TimedEvent:
		return e.LinkedEvent
	case hebcal.YizkorEvent:
		return e.LinkedEvent
	}
	return nil
}

func isTimedEv(ev event.CalEvent) bool {
	_, ok := download.AsTimedEvent(ev)
	return ok
}

func isParsha(ev event.CalEvent) bool {
	return ev.GetFlags() == event.PARSHA_HASHAVUA
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
			names = append(names, renderSubject(h, p.Locale, false))
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

// holidayDescription is the first sentence of the holiday's description from
// the MEMO catalog, in Hebrew for a Hebrew locale and English otherwise,
// looked up by the event's description and then by its basename.
func holidayDescription(ev event.CalEvent, flags event.HolidayFlags, locale string) string {
	localeName := "en"
	if isHebrew(locale) {
		localeName = "he"
	}
	key := untranslatedDesc(ev)
	if flags.Has(event.SHABBAT_MEVARCHIM) {
		key = "Shabbat Mevarchim Chodesh"
	}
	s := lookupMemo("MEMO:"+key, localeName)
	if s == "" {
		s = lookupMemo("MEMO:"+basename(ev), localeName)
	}
	if i := strings.Index(s, "."); i >= 0 {
		s = s[:i]
	}
	return s
}

func lookupMemo(key, localeName string) string {
	// An unknown English key comes back as itself, which means not found.
	if s, ok := locales.LookupTranslation(key, localeName); ok && s != key {
		return s
	}
	return ""
}

// basename is the event's description without the qualifiers that tell the
// days of one holiday apart ("Sukkot III (CH’’M)" => "Sukkot"). Chanukah
// candle-lighting is the holiday itself, so it takes the holiday's.
func basename(ev event.CalEvent) string {
	if timed, ok := ev.(hebcal.TimedEvent); ok && timed.LinkedEvent != nil &&
		timed.Flags.Has(event.CHANUKAH_CANDLES) {
		return timed.LinkedEvent.Basename()
	}
	return model.FixMonthSpelling(ev.Basename())
}

// untranslatedDesc returns the event's English description.
func untranslatedDesc(ev event.CalEvent) string {
	if he, ok := ev.(event.HolidayEvent); ok {
		return model.FixMonthSpelling(he.Desc)
	}
	if te, ok := download.AsTimedEvent(ev); ok {
		return te.Desc
	}
	return model.FixMonthSpelling(ev.Render("en"))
}

func isHebrew(locale string) bool {
	switch strings.ToLower(locale) {
	case "he", "he-x-nonikud", "h":
		return true
	}
	return false
}

// stripEmoji removes the holiday emoji hebcal-go appends to some titles.
func stripEmoji(s string) string {
	out := strings.Map(func(r rune) rune {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) || r == 0xFE0F {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(out)
}
