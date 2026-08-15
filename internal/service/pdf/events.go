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
	// Learning marks a daily-learning row fetched from hebcal-web because no
	// Go schedule generates it. hebcal-go has no flag for these, so colour and
	// ordering key off this instead of Flags.
	Learning bool
	// AltDate marks a HEBREW_DATE alternate-date event (from d=on / D=on). It is
	// not drawn as an event row; the renderer prints its brief form on the
	// day-number line via renderAltDateOnLine, matching src/pdf.js.
	AltDate bool
	// FastEnds marks the "Fast ends" timed event, which carries the same fast
	// flag as "Fast begins" but sorts after the fast day itself rather than
	// before it (eventOrder). @hebcal/core emits begins, holiday, ends.
	FastEnds bool
	// URL is the canonical hebcal.com page for this event, or empty. The
	// renderer turns it into the short, tracked form and attaches a link
	// annotation over the drawn text.
	URL string
	// HebrewBrief is the event's brief Hebrew name, set only when the request
	// asked for appendHebrewToSubject (lg=ah / lg=sh). The renderer draws it
	// after the transliterated subject.
	HebrewBrief string
}

// Timed reports whether the event has a clock time.
func (e *Event) Timed() bool { return e.TimeStr != "" }

// Generate produces the events for a calendar, in date order.
//
// Everything comes from hebcal-go in-process. hebcal-web reaches this same
// result through @hebcal/core; the two libraries share the holiday tables, so
// the event sets agree for the options this service accepts. Requests naming a
// daily-learning series hebcal-go cannot generate are rejected upstream in the
// handler rather than silently rendered without those rows.
func Generate(p *Params) ([]Event, error) {
	opts := p.Opts
	events, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		return nil, fmt.Errorf("hebcal: %w", err)
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
			HD:       hd,
			Greg:     hd.Gregorian(),
			Subject:  subject,
			Flags:    flags,
			TimeStr:  timeStr,
			URL:      event.URL(ev),
			AltDate:  flags&event.HEBREW_DATE != 0,
			FastEnds: isFastEnds(ev),
		}
		// appendHebrewToSubject draws the Hebrew name after the transliteration.
		// It is the brief Hebrew rendering, computed here because the renderer
		// only sees the flattened Event, not the source CalEvent.
		if p.AppendHebrew && !e.AltDate {
			e.HebrewBrief = model.FixMonthSpelling(renderSubject(ev, flags, "he"))
		}
		out = append(out, e)
	}
	// Hebrew-month calendars show the Gregorian date as the alternate date, which
	// hebcal-go does not generate; synthesize it the way src/calendar.js does.
	if p.MonthMode != GregorianArabic && (p.AddAltDates || p.AddAltDatesForEvents) {
		out = addGregorianAltDates(out, p)
	}
	return out, nil
}

// addGregorianAltDates inserts the Gregorian alternate-date events a Hebrew-month
// calendar draws on its day-number line -- the GregorianDateEvent path in
// src/calendar.js. AddAltDates covers every day from the first event to the
// last; AddAltDatesForEvents covers only days that already have events. The
// events carry AltDate so the renderer prints them on the day line, not as rows.
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

// nonEnglishDateLocales mirrors localeMap in src/lang.js: the resolved locales
// dayjs formats as "D MMM" rather than the English "MMM D". Anything not listed
// falls through to the English template, matching `localeMap[locale] || 'en'`.
var nonEnglishDateLocales = map[string]bool{
	"de": true, "es": true, "fi": true, "fr": true, "he": true,
	"he-x-nonikud": true, "hu": true, "nl": true, "pl": true, "pt": true,
	"ru": true, "ro": true, "uk": true,
}

// gregorianAltText renders the Gregorian date shown on a Hebrew-month calendar's
// day-number line, matching GregorianDateEvent.render(): "Jun 12" in English,
// "12 Jun" (localized month) elsewhere. No year, as in production.
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
	case hebcal.TimedEvent:
		return e.Desc
	}
	return ev.Render("en")
}

// eventCategories is the port of getEventCategories() in @hebcal/rest-api.
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

// keepEvent applies the two filters hebcal-web applies after generating a
// calendar (src/calendar.js). Both work on categories rather than flags, which
// is what makes the Purim and Chanukah special case above take effect.
func keepEvent(ev event.CalEvent, flags event.HolidayFlags, p *Params) bool {
	switch {
	case p.YomTovOnly:
		cats := eventCategories(ev)
		return len(cats) > 0 && cats[0] == "holiday" && flags&event.CHAG != 0
	case p.NoMinorHolidays:
		cats := eventCategories(ev)
		return len(cats) < 2 || cats[1] != "minor"
	}
	return true
}

// hour12Countries are the country codes that default to a 12-hour clock,
// matching hour12cc in @hebcal/core's reformatTimeStr.js. Every other country --
// including Israel, which is deliberately absent -- defaults to 24-hour, so a
// Ghana or Reykjavik calendar shows "17:49" where a US one shows "5:49p".
var hour12Countries = map[string]bool{
	"US": true, "CA": true, "BR": true, "AU": true, "NZ": true, "DO": true,
	"PR": true, "GR": true, "IN": true, "KR": true, "NP": true, "ZA": true,
}

// use12Hour reports whether clock times render in 12-hour form. hour12 forces it
// either way (1 = force 12-hour, 2 = force 24-hour); otherwise it follows the
// location's country, mirroring reformatTimeStr() in @hebcal/core, which falls
// back to "IL" or "US" when no country is known.
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
	return hour12Countries[cc]
}

// timeStringOf formats the clock time for a timed event.
//
// hebcal-go renders these into the description as "Havdalah: 8:51" with no
// meridiem, but the PDF wants hebcal-web's compact form: reformatTimeStr(…,
// 'p', …) produces "8:51p" in 12-hour countries and keeps 24-hour ones as
// "20:51". The time itself comes from TimedEvent.EventTime rather than from
// parsing the description back apart.
func timeStringOf(ev event.CalEvent, p *Params) string {
	te, ok := ev.(hebcal.TimedEvent)
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

// learningFlags are the daily-learning series, which @hebcal/rest-api groups as
// LEARNING_MASK.
const learningFlags = event.DAF_YOMI | event.MISHNA_YOMI |
	event.NACH_YOMI | event.YERUSHALMI_YOMI

// renderSubject renders an event the way hebcal-web's renderPdfEvent does:
// shouldRenderBrief() in @hebcal/rest-api decides between render() and
// renderBrief(), and a calendar cell is narrow enough that the difference
// matters.
//
// Only the cases this service can generate are handled. Timed events are
// already brief by construction -- Generate splits the clock time out of the
// subject, which is what TimedEvent.renderBrief does.
func renderSubject(ev event.CalEvent, flags event.HolidayFlags, locale string) string {
	full := ev.Render(locale)
	switch {
	case flags&event.SHABBAT_MEVARCHIM != 0:
		// "Shabbat Mevarchim Chodesh Sh'vat" -> "Mevarchim Chodesh Sh'vat".
		// MevarchimChodeshEvent.renderBrief drops everything up to the first
		// space, in whatever language, so this does the same rather than
		// matching on the English word.
		if i := strings.Index(full, " "); i >= 0 {
			return full[i+1:]
		}
	case flags&learningFlags != 0:
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

// isFastEnds reports whether ev is the "Fast ends" timed event. hebcal-go emits
// "Fast begins" and "Fast ends" as TimedEvents carrying the same fast flag, so
// only the untranslated description tells them apart; the localized Subject
// would not survive a non-English locale.
func isFastEnds(ev event.CalEvent) bool {
	te, ok := ev.(hebcal.TimedEvent)
	return ok && te.Desc == "Fast ends"
}

// eventOrder gives the sort position of an event within a single day.
//
// @hebcal/core emits a day's events by walking the holidays that fall on it
// and, for each, pushing its related events around it: the Erev Pesach chametz
// deadlines, then the fast start, then the holiday itself, then the fast end;
// afterwards come the parsha, daily learning, the Omer, Molad, and finally
// candle lighting and Havdalah. hebcal-go walks the same holidays in a
// different order, so a fast day that is also Erev Pesach came out with the
// chametz times above "Fast begins" instead of below it.
//
// Note that @hebcal/core pushes the fast end (endEvent) inside the fast
// holiday's block, so "Fast ends" lands after the fast day itself but still
// before the parsha, learning and candle lighting of the main loop -- e.g. on
// Asara B'Tevet the sequence is "Fast begins", "Asara B'Tevet", "Fast ends".
// "Fast begins" and "Fast ends" share the fast flag, so FastEnds tells them
// apart (isFastEnds).
//
// Ordering by kind reproduces the published sequence without depending on
// either library's internal walk. Note that a holiday can carry LIGHT_CANDLES
// itself -- Erev Pesach does -- so the timed events are told apart by having a
// clock time, not by that flag.
func eventOrder(ev *Event) int {
	timed := ev.Timed()
	f := ev.Flags
	isFast := f&(event.MINOR_FAST|event.MAJOR_FAST) != 0
	switch {
	case ev.Learning:
		return 6 // alongside the daily-learning series generated locally
	case timed && isFast && !ev.FastEnds:
		return 0 // "Fast begins"
	case !timed && isFast:
		return 1 // the fast day itself
	case timed && isFast && ev.FastEnds:
		return 2 // "Fast ends" -- after the fast day, before parsha/candles
	case timed && f == 0:
		return 3 // the Erev Pesach chametz deadlines carry no flags
	case f&event.PARSHA_HASHAVUA != 0:
		return 5
	// DAILY_LEARNING is the generic flag; only four schedules have one of their
	// own. Testing just those four left the other nine -- Psalms, both Rambam
	// cycles, Daf-a-Week, Perek Yomi, Tanakh Yomi, 929 and Pirkei Avot -- to
	// fall through to the holiday slot below, where they sorted *above* Daf
	// Yomi and Mishna Yomi instead of beside them.
	case f&(event.DAF_YOMI|event.MISHNA_YOMI|event.NACH_YOMI|event.YERUSHALMI_YOMI|
		event.DAILY_LEARNING) != 0:
		return 6
	case f&event.OMER_COUNT != 0:
		return 6
	case f&(event.MOLAD|event.SHABBAT_MEVARCHIM) != 0:
		return 6
	case timed:
		return 7 // candle lighting, Havdalah
	}
	return 4 // holidays, Rosh Chodesh, special Shabbatot
}

// sortDay orders one day's events. The sort is stable so that events sharing a
// position keep the order hebcal-go produced them in.
func sortDay(evs []Event) {
	sort.SliceStable(evs, func(i, j int) bool {
		return eventOrder(&evs[i]) < eventOrder(&evs[j])
	})
}

// SplitByGregorianMonth groups events into one bucket per Gregorian month, in
// chronological order, inserting empty months so a gap does not silently drop a
// page. Mirrors eventsToCells() in hebcal-web's src/pdf.js.
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
