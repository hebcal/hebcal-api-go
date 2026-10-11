package pdf

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
)

// Event is one rendered line in a calendar cell: the flattened form of a
// hebcal-go CalEvent plus the presentation bits the renderer needs.
type Event struct {
	// HD is the Hebrew date of the event.
	HD hdate.HDate
	// Greg is the Gregorian date of the event.
	Greg time.Time
	// Subject is the rendered description in the requested locale.
	Subject string
	// Flags is the hebcal-go bitmask, used for colour and font weight.
	Flags event.HolidayFlags
	// TimeStr is the formatted clock time for timed events (candle lighting,
	// havdalah, fast start/end), or empty for all-day events.
	TimeStr string
	// Learning marks a daily-learning row fetched from the readings-svc sidecar
	// because no Go schedule generates it. hebcal-go has no flag for these, so
	// colour and ordering key off this instead of Flags.
	Learning bool
	// AltDate marks a HEBREW_DATE alternate-date event (from d=on / D=on). It is
	// not drawn as an event row; the renderer prints its brief form on the
	// day-number line via renderAltDateOnLine.
	AltDate bool
	// rank is the event's position within its day, hebcal.TimeOfDay for the
	// events hebcal-go generates. sortDay orders a day by it alone, which keeps
	// hebcal-go's order and puts the fetched learning rows in the learning
	// slot.
	rank int
	// URL is the canonical hebcal.com page for this event, or empty. The
	// renderer turns it into the short, tracked form and attaches a link
	// annotation over the drawn text.
	URL string
	// HebrewBrief is the event's brief Hebrew name, set only when the request
	// asked for the Hebrew name alongside the transliteration (lg=ah / lg=sh).
	// The renderer draws it after the transliterated subject.
	HebrewBrief string
}

// Timed reports whether the event has a clock time.
func (e *Event) Timed() bool { return e.TimeStr != "" }

// Generate produces the events for a calendar, in date order.
//
// Everything comes from hebcal-go in-process. Requests naming a daily-learning
// series hebcal-go cannot generate are rejected upstream in the handler rather
// than silently rendered without those rows.
func Generate(p *Params) ([]Event, error) {
	opts := p.Opts
	events, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		return nil, fmt.Errorf("hebcal: %w", err)
	}
	// A whole-Hebrew-year calendar in Hebrew-month mode intentionally carries
	// Tishrei of the year after the requested span too, a head start for next
	// year's calendar. Mirrors hebcal-web's makeHebrewCalendar. Anchored at
	// Year+NumYears, not Year+1: NumYears>1 already covers later years in the
	// main call, and duplicating one of those months here -- out of date
	// order, since it's appended -- would make SplitByHebrewMonth's
	// last-event-as-range-end logic stop short and drop real later pages.
	if p.MonthMode != GregorianArabic && opts.IsHebrewYear && opts.Year != 0 &&
		opts.Start.Abs() == 0 && opts.End.Abs() == 0 {
		nextYear := opts.Year + 1
		if opts.NumYears > 1 {
			nextYear = opts.Year + opts.NumYears
		}
		extraOpts := opts
		extraOpts.IsHebrewYear = false
		extraOpts.NumYears = 0
		extraOpts.Start = hdate.New(nextYear, hdate.Tishrei, 1)
		extraOpts.End = hdate.New(nextYear, hdate.Tishrei, 30) // Tishrei always has 30 days
		extra, err := hebcal.HebrewCalendar(&extraOpts)
		if err != nil {
			return nil, fmt.Errorf("hebcal: next Tishrei: %w", err)
		}
		events = append(events, extra...)
	}
	out := make([]Event, 0, len(events))
	for _, ev := range events {
		flags := ev.GetFlags()
		if !keepEvent(ev, flags, p) {
			continue
		}
		hd := ev.GetDate()
		subject := model.FixMonthSpelling(jsutil.SmartApostrophe(renderSubject(ev, flags, p.Locale)))
		if !p.Emoji {
			subject = stripEmoji(subject)
		}
		// hebcal-go renders timed events as "Candle lighting: 6:56pm", but the
		// PDF draws the time separately in bold at a smaller size, so it has to
		// come out of the subject or it appears twice.
		timeStr := timeStringOf(ev, p)
		if timeStr != "" {
			if i := strings.LastIndex(subject, ": "); i >= 0 {
				subject = subject[:i]
			}
		}
		e := Event{
			HD:      hd,
			Greg:    hd.Gregorian(),
			Subject: subject,
			Flags:   flags,
			TimeStr: timeStr,
			URL:     event.URL(ev),
			AltDate: flags.Has(event.HEBREW_DATE),
			rank:    hebcal.TimeOfDay(ev),
		}
		if e.AltDate {
			e.rank = altDateRank
		}
		// The Hebrew name drawn after the transliteration (lg=ah / lg=sh). It
		// is the brief Hebrew rendering, computed here because the renderer only
		// sees the flattened Event, not the source CalEvent. Timed events need
		// the same clock-time trim as the English subject above, or the time is
		// drawn twice -- once in bold by the renderer, once baked into the
		// Hebrew text.
		if p.AppendHebrew && !e.AltDate {
			hebrewBrief := model.FixMonthSpelling(renderSubject(ev, flags, "he"))
			if timeStr != "" {
				if i := strings.LastIndex(hebrewBrief, ": "); i >= 0 {
					hebrewBrief = hebrewBrief[:i]
				}
			}
			e.HebrewBrief = hebrewBrief
		}
		out = append(out, e)
	}
	// Hebrew-month calendars show the Gregorian date as the alternate date, which
	// hebcal-go does not generate; synthesize it here.
	if p.MonthMode != GregorianArabic && (p.AddAltDates || p.AddAltDatesForEvents) {
		out = addGregorianAltDates(out, p)
	}
	return out, nil
}

// addGregorianAltDates inserts the Gregorian alternate-date events a Hebrew-month
// calendar draws on its day-number line. AddAltDates covers every day from the
// first event to the last; AddAltDatesForEvents covers only days that already
// have events. The events carry AltDate so the renderer prints them on the day
// line, not as rows.
func addGregorianAltDates(events []Event, p *Params) []Event {
	if len(events) == 0 {
		return events
	}
	alt := func(hd hdate.HDate) Event {
		return Event{HD: hd, Greg: hd.Gregorian(), AltDate: true}
	}
	var alts []Event
	if p.AddAltDates {
		for abs := events[0].HD.Abs(); abs <= events[len(events)-1].HD.Abs(); abs++ {
			alts = append(alts, alt(hdate.FromRD(abs)))
		}
	} else {
		seen := make(map[int64]bool, len(events))
		for i := range events {
			if a := events[i].HD.Abs(); !seen[a] {
				seen[a] = true
				alts = append(alts, alt(events[i].HD))
			}
		}
	}
	out := append(events, alts...)
	// SplitByHebrewMonth reads the first and last events as the date range, so
	// the list has to stay in date order after the insert.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].HD.Abs() < out[j].HD.Abs()
	})
	return out
}

// nonEnglishDateLocales are the resolved locales that format a short date as
// "D MMM" rather than the English "MMM D". Anything not listed falls through to
// the English template.
var nonEnglishDateLocales = map[string]bool{
	"de": true, "es": true, "fi": true, "fr": true, "he": true,
	"he-x-nonikud": true, "hu": true, "nl": true, "pl": true, "pt": true,
	"ru": true, "ro": true, "uk": true,
}

// gregorianAltText renders the Gregorian date shown on a Hebrew-month calendar's
// day-number line: "Jun 12" in English, "12 Jun" (localized month) elsewhere.
// No year.
func gregorianAltText(hd hdate.HDate, locale string) string {
	g := hd.Gregorian()
	mon := model.NamesFor(locale).MonthsShort[int(g.Month())-1]
	if nonEnglishDateLocales[strings.ToLower(locale)] {
		return fmt.Sprintf("%d %s", g.Day(), mon)
	}
	return fmt.Sprintf("%s %d", mon, g.Day())
}

// untranslatedDesc returns the event's English description, which is what the
// category rules key on.
func untranslatedDesc(ev event.CalEvent) string {
	switch e := ev.(type) {
	case event.HolidayEvent:
		return e.Desc
	}
	if te, ok := asTimedEvent(ev); ok {
		return te.Desc
	}
	return ev.Render("en")
}

// asTimedEvent returns the TimedEvent behind ev. KiddushLevanaEvent embeds a
// TimedEvent but is a type of its own, so a plain type assertion misses it and
// its time would stay baked into the subject instead of being drawn like every
// other clock time.
func asTimedEvent(ev event.CalEvent) (hebcal.TimedEvent, bool) {
	switch e := ev.(type) {
	case hebcal.TimedEvent:
		return e, true
	case hebcal.KiddushLevanaEvent:
		return e.TimedEvent, true
	}
	return hebcal.TimedEvent{}, false
}

// eventCategories returns an event's [category, subcategory].
//
// Purim and Chanukah are filed as major even though their flags say
// MINOR_HOLIDAY, which is what keeps them in a calendar that asked only for
// major holidays while Tu BiShvat and Rosh Hashana LaBehemot drop out.
func eventCategories(ev event.CalEvent) []string {
	d := untranslatedDesc(ev)
	if d == "Purim" || d == "Erev Purim" || strings.HasPrefix(d, "Chanukah: ") {
		return []string{"holiday", "major"}
	}
	return ev.GetCategories()
}

// keepEvent applies the two post-generation filters (yom-tov-only and
// no-minor-holidays). Both work on categories rather than flags, which is what
// makes the Purim and Chanukah special case above take effect.
func keepEvent(ev event.CalEvent, flags event.HolidayFlags, p *Params) bool {
	switch {
	case p.YomTovOnly:
		cats := eventCategories(ev)
		return len(cats) > 0 && cats[0] == "holiday" && flags.Has(event.CHAG)
	case p.NoMinorHolidays:
		cats := eventCategories(ev)
		return len(cats) < 2 || cats[1] != "minor"
	}
	return true
}

// use12Hour reports whether clock times render in 12-hour form. hour12 forces it
// either way (1 = force 12-hour, 2 = force 24-hour); otherwise it follows the
// location's country, falling back to "IL" or "US" when no country is known.
func (p *Params) use12Hour() bool {
	switch p.Hour12 {
	case 1:
		return true
	case 2:
		return false
	}
	cc := ""
	if p.Opts.Location != nil {
		cc = p.Opts.Location.CountryCode
	}
	if cc == "" {
		if p.Opts.IL {
			cc = "IL"
		} else {
			cc = "US"
		}
	}
	// Every country outside the 12-hour set -- Israel included -- defaults to
	// 24-hour, so a Ghana or Reykjavik calendar shows "17:49" where a US one
	// shows "5:49p".
	return event.Uses12HourClock(cc)
}

// timeStringOf formats the clock time for a timed event in the compact form the
// PDF uses: "8:51p" in 12-hour countries, "20:51" in 24-hour ones. The time
// comes from TimedEvent.EventTime rather than from parsing hebcal-go's
// "Havdalah: 8:51" description back apart.
func timeStringOf(ev event.CalEvent, p *Params) string {
	te, ok := asTimedEvent(ev)
	if !ok {
		return ""
	}
	t := te.EventTime
	if t.IsZero() {
		return ""
	}
	if !p.use12Hour() {
		return t.Format("15:04")
	}
	h, m := t.Hour(), t.Minute()
	suffix := "a"
	if h >= 12 {
		suffix = "p"
	}
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%d:%02d%s", h12, m, suffix)
}

// learningFlags are the daily-learning series flags.
const learningFlags = event.DAF_YOMI | event.MISHNA_YOMI |
	event.NACH_YOMI | event.YERUSHALMI_YOMI | event.DAILY_LEARNING

// renderSubject renders an event's subject for a calendar cell, which is narrow
// enough that the brief form is wanted over the full one.
//
// Only the cases this service can generate are handled. Timed events are
// already brief by construction -- Generate splits the clock time out of the
// subject.
func renderSubject(ev event.CalEvent, flags event.HolidayFlags, locale string) string {
	full := ev.Render(locale)
	switch {
	case flags.Has(event.SHABBAT_MEVARCHIM):
		// "Shabbat Mevarchim Chodesh Sh'vat" -> "Mevarchim Chodesh Sh'vat".
		// Drop everything up to the first space, in whatever language, rather
		// than matching on the English word.
		if i := strings.Index(full, " "); i >= 0 {
			return full[i+1:]
		}
	case flags.HasAny(learningFlags):
		// The daily-learning series render as "Daf Yomi: Berakhot 2"; the cell
		// shows only the reading.
		if i := strings.Index(full, ": "); i >= 0 {
			return full[i+2:]
		}
	}
	return full
}

// stripEmoji removes trailing holiday emoji from a rendered description when
// the request did not ask for them.
func stripEmoji(s string) string {
	out := strings.Map(func(r rune) rune {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) || r == 0xFE0F {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(out)
}

// altDateRank puts an alternate-date event ahead of a day's other events, as
// hebcal-go emits it. It is drawn on the day-number line rather than as a row,
// so only consistency depends on it.
const altDateRank = -1

// sortDay orders one day's events by rank. hebcal-go already orders each day
// by time of day, so this changes nothing but the daily-learning rows
// mergeLearning appended at the end of the day, which move up to the learning
// slot, after any learning series hebcal-go generated itself. The sort is
// stable so that events of equal rank keep the order hebcal-go produced them
// in.
func sortDay(evs []Event) {
	sort.SliceStable(evs, func(i, j int) bool {
		return evs[i].rank < evs[j].rank
	})
}

// SplitByGregorianMonth groups events into one bucket per Gregorian month, in
// chronological order, inserting empty months so a gap does not silently drop a
// page.
func SplitByGregorianMonth(events []Event) []MonthPage {
	if len(events) == 0 {
		return nil
	}
	byKey := make(map[string]map[int][]Event)
	var order []string
	add := func(key string) map[int][]Event {
		m, ok := byKey[key]
		if !ok {
			m = make(map[int][]Event)
			byKey[key] = m
			order = append(order, key)
		}
		return m
	}
	for _, e := range events {
		key := e.Greg.Format("200601")
		add(key)[e.Greg.Day()] = append(add(key)[e.Greg.Day()], e)
	}
	// Fill in months with no events at all, so a quiet month still gets a page.
	first := events[0].Greg
	last := events[len(events)-1].Greg
	for d := time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC); !d.After(last); d = d.AddDate(0, 1, 0) {
		add(d.Format("200601"))
	}
	for _, days := range byKey {
		for _, evs := range days {
			sortDay(evs)
		}
	}
	pages := make([]MonthPage, 0, len(byKey))
	for key := range byKey {
		t, _ := time.Parse("200601", key)
		pages = append(pages, MonthPage{Year: t.Year(), Month: t.Month(), Days: byKey[key]})
	}
	sortMonthPages(pages)
	return pages
}

// MonthPage is one rendered page: a Gregorian month and its events by day.
type MonthPage struct {
	Year  int
	Month time.Month
	Days  map[int][]Event
}

// sortMonthPages orders pages chronologically.
func sortMonthPages(p []MonthPage) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0; j-- {
			a, b := p[j-1], p[j]
			if a.Year < b.Year || (a.Year == b.Year && a.Month <= b.Month) {
				break
			}
			p[j-1], p[j] = p[j], p[j-1]
		}
	}
}
