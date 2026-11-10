package yahrzeit

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dustin/go-humanize"
	"github.com/hebcal/gematriya"
	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// The kinds of entry a calendar holds.
const (
	typeYahrzeit    = "Yahrzeit"
	typeBirthday    = "Birthday"
	typeAnniversary = "Anniversary"
	typeOther       = "Other"
)

// How many years of observances a calendar carries.
const (
	minYears     = 2
	maxYears     = 50
	defaultYears = 20
)

// The Hebrew years an explicit start= may name. Dates far outside the
// Gregorian calendar's own range are nobody's subscription.
const (
	minStartYear = 1
	maxStartYear = 9999
)

const (
	emojiCandle   = "🕯️"
	emojiBirthday = "🎂✡️"
	yahrzeitHe    = "יארצייט"
)

// typeHe is the Hebrew for each kind of entry, used in the title of an entry
// whose name is written in Hebrew.
var typeHe = map[string]string{
	typeYahrzeit:    yahrzeitHe,
	typeBirthday:    "יום הולדת",
	typeAnniversary: "יום נישואין",
}

// entry is one numbered person or event of a calendar.
type entry struct {
	id   int
	typ  string
	name string
	// orig is the Hebrew date of the original event, already moved to the
	// next day when it happened after sunset.
	orig hdate.HDate
}

// calEvent is one generated event, before it becomes a VEVENT.
type calEvent struct {
	date  hdate.HDate
	subj  string
	emoji string
	memo  string
	uid   string
	alarm *icalendar.Alarm
	// A reminder is the one timed event: it starts at hour:minute on date.
	timed        bool
	hour, minute int
}

// numYears reads the years= option: twenty by default, at least two, at most
// fifty.
func numYears(s string) int {
	y, ok := jsutil.ParseInt(s)
	switch {
	case !ok:
		return defaultYears
	case y < minYears:
		return minYears
	case y > maxYears:
		return maxYears
	}
	return y
}

// dateRange is the span of Hebrew years a calendar covers.
type dateRange struct {
	startYear, endYear, years int
}

// rangeFor works out the span from the start=, end= and years= options. With
// no start it begins this Hebrew year -- or last year, during the first four
// months, so an observance from last spring is still on the calendar.
func rangeFor(q *Query, today hdate.HDate) (dateRange, error) {
	years := numYears(q.Get("years"))
	start, ok := jsutil.ParseInt(q.Get("start"))
	if !ok || start == 0 {
		start = today.Year()
		if m := today.Month(); m >= hdate.Tishrei && m <= hdate.Tevet {
			start--
		}
	} else if start < minStartYear || start > maxStartYear {
		return dateRange{}, model.BadRequest("Hebrew year out of range: %s", q.Get("start"))
	}
	end, ok := jsutil.ParseInt(q.Get("end"))
	if !ok || end == 0 {
		end = start + years - 1
	}
	return dateRange{startYear: start, endYear: end, years: years}, nil
}

// includeNumber reports whether titles carry the anniversary number ("3rd").
// It is on unless explicitly turned off: none of the URLs written before the
// option existed mention it.
func includeNumber(q *Query) bool {
	an := q.Get("an")
	return an != "off" && an != "0"
}

// entryIDs returns the numbers of the entries that have a date, ascending.
func entryIDs(q *Query) []int {
	seen := make(map[int]bool)
	var ids []int
	for _, key := range q.Keys() {
		id, ok := keyID(key)
		if !ok || id < 1 || seen[id] {
			continue
		}
		n := strconv.Itoa(id)
		if (key[0] == 'x' && q.Get(key) != "") ||
			(q.Get("y"+n) != "" && q.Get("m"+n) != "" && q.Get("d"+n) != "") ||
			(q.Get("hy"+n) != "" && q.Get("hm"+n) != "" && q.Get("hd"+n) != "") {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

// keyID splits a numbered parameter name -- one letter and the entry number,
// "n3" -- and returns the number.
func keyID(key string) (int, bool) {
	if len(key) < 2 || len(key) > 10 {
		return 0, false
	}
	for i := 1; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return 0, false
		}
	}
	id, err := strconv.Atoi(key[1:])
	return id, err == nil
}

// isNumKey reports whether key is a numbered parameter of the given letter.
func isNumKey(key string, letter byte) bool {
	return len(key) >= 2 && key[0] == letter && key[1] >= '0' && key[1] <= '9'
}

// anniversaryType reads a t<N> value by its first letter; anything
// unrecognized, or nothing, is a Yahrzeit.
func anniversaryType(s string) string {
	if s != "" {
		switch s[0] | 0x20 {
		case 'b':
			return typeBirthday
		case 'a':
			return typeAnniversary
		case 'o':
			return typeOther
		}
	}
	return typeYahrzeit
}

// entryFor reads entry id from the query. ok is false when it has no date;
// an invalid date is an error.
func entryFor(q *Query, id int) (e entry, ok bool, err error) {
	n := strconv.Itoa(id)
	e.id = id
	e.typ = anniversaryType(q.Get("t" + n))
	e.name = strings.TrimSpace(q.Get("n" + n))
	if e.name == "" {
		if e.typ == typeOther {
			e.name = "Untitled" + n
		} else {
			e.name = "Person" + n
		}
	}

	// A Hebrew date, when given, is used as is.
	if hy, hm, hd := q.Get("hy"+n), q.Get("hm"+n), q.Get("hd"+n); hy != "" && hm != "" && hd != "" {
		e.orig, err = model.MakeHebDate(hy, hm, hd)
		return e, err == nil, err
	}

	yy, mm, dd := q.Get("y"+n), q.Get("m"+n), q.Get("d"+n)
	if x := q.Get("x" + n); len(x) == 10 {
		// The compact YYYY-MM-DD form.
		yy, mm, dd = x[0:4], strings.TrimPrefix(x[5:7], "0"), strings.TrimPrefix(x[8:10], "0")
	}
	if yy == "" || mm == "" || dd == "" {
		return e, false, nil
	}
	dt, err := model.MakeGregDate(yy, mm, dd)
	if err != nil {
		return e, false, err
	}
	e.orig = hdate.FromProlepticGregorian(dt.Year, dt.Month, dt.Day)
	if q.on("s" + n) {
		// After sunset is already the next Hebrew day.
		e.orig = e.orig.Next()
	}
	return e, true, nil
}

// eventOptions are the settings every event of one calendar shares.
type eventOptions struct {
	calendarID    string
	includeURL    bool
	appendHebDate bool
	includeNumber bool
}

// makeEvents generates the calendar's events in date order: for each entry
// its observances over the range and, when reminder is set, an afternoon
// reminder on the eve of each Yahrzeit; then the Yizkor days when asked for.
// A calendar that is not a feed (a CSV, imported once) never links back to the
// page where a saved calendar is edited.
func makeEvents(q *Query, r dateRange, reminder, feed bool) ([]calEvent, error) {
	calendarID := q.Get("ulid")
	opts := eventOptions{
		calendarID:    calendarID,
		includeURL:    feed && calendarID != "" && q.Get("dl") != "1",
		appendHebDate: q.on("hebdate"),
		includeNumber: includeNumber(q),
	}
	var events []calEvent
	for _, id := range entryIDs(q) {
		e, ok, err := entryFor(q, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		// An entry gets as many observances as the calendar has years, the
		// first in the starting year or the first year it has one.
		hyear := r.startYear
		if hyear < e.orig.Year() {
			hyear = e.orig.Year()
		}
		for n := 0; n < r.years; hyear++ {
			ev, ok := makeEvent(e, hyear, opts)
			if !ok {
				continue
			}
			n++
			events = append(events, ev)
			if reminder && e.typ == typeYahrzeit {
				events = append(events, makeReminder(e, ev, hashFor(e, opts)))
			}
		}
	}
	if q.on("yizkor") {
		yizkorID := calendarID
		if !feed {
			yizkorID = ""
		}
		events = append(events, makeYizkorEvents(r, q.Get("i") == "on", yizkorID)...)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].date.Abs() < events[j].date.Abs()
	})
	return events, nil
}

// hashFor is the part of an entry's UIDs that tells it apart from other
// calendars' entries: the saved calendar's id, or a hash of the name.
func hashFor(e entry, opts eventOptions) string {
	if opts.calendarID != "" {
		return opts.calendarID
	}
	return icalendar.Murmur32Hex(e.name)
}

// makeEvent builds entry e's observance in Hebrew year hyear. ok is false
// when it has none: a Yahrzeit is first observed the year after the death.
func makeEvent(e entry, hyear int, opts eventOptions) (calEvent, bool) {
	var hd hdate.HDate
	var err error
	if e.typ == typeYahrzeit {
		hd, err = hdate.GetYahrzeit(hyear, e.orig)
	} else {
		hd, err = hdate.GetBirthdayOrAnniversary(hyear, e.orig)
	}
	if err != nil {
		return calEvent{}, false
	}
	yearNumber := hyear - e.orig.Year()
	ev := calEvent{
		date: hd,
		subj: makeSubject(e, hd, yearNumber, opts),
		uid:  strings.ToLower(e.typ) + "-" + strconv.Itoa(hyear) + "-" + hashFor(e, opts) + "-" + strconv.Itoa(e.id),
		// A personal event is announced at noon the day before. A Yahrzeit is
		// not: it has its own reminder event, on its eve.
		alarm: icalendar.AlarmUserEvent,
	}
	switch e.typ {
	case typeYahrzeit:
		ev.emoji = emojiCandle
		ev.alarm = nil
	case typeBirthday:
		ev.emoji = emojiBirthday
	}
	editURL := ""
	if opts.includeURL {
		editURL = "https://www.hebcal.com/yahrzeit/edit/" + opts.calendarID + "#row" + strconv.Itoa(e.id)
	}
	if !opts.includeNumber {
		yearNumber = 0
	}
	ev.memo = makeMemo(e, hd, yearNumber, editURL)
	return ev, true
}

// hasHebrew reports whether s contains a Hebrew letter.
func hasHebrew(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r >= 'א' && r <= 'ת' }) >= 0
}

// hasLatin reports whether s contains an unaccented Latin letter.
func hasLatin(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return r <= unicode.MaxASCII && (r|0x20) >= 'a' && (r|0x20) <= 'z'
	}) >= 0
}

// makeSubject is the event title: "Ploni’s 3rd Yahrzeit", or its Hebrew
// counterpart for a name written only in Hebrew, with the Hebrew day and
// month in parentheses when the calendar asks for them.
func makeSubject(e entry, hd hdate.HDate, yearNumber int, opts eventOptions) string {
	subj := e.name
	hebrewName := hasHebrew(e.name) && !hasLatin(e.name)
	if e.typ != typeOther {
		isYahrzeit := e.typ == typeYahrzeit
		nth := strconv.Itoa(yearNumber)
		switch {
		case hebrewName && isYahrzeit && opts.includeNumber:
			subj = typeHe[e.typ] + " ה-" + nth + " של " + e.name
		case hebrewName && isYahrzeit:
			subj = typeHe[e.typ] + " של " + e.name
		case hebrewName && opts.includeNumber:
			subj = typeHe[e.typ] + " " + nth + " ל" + e.name
		case hebrewName:
			subj = typeHe[e.typ] + " ל" + e.name
		default:
			typeStr := e.typ
			if !isYahrzeit {
				typeStr = "Hebrew " + e.typ
			}
			if opts.includeNumber {
				subj = e.name + "’s " + humanize.Ordinal(yearNumber) + " " + typeStr
			} else {
				subj = e.name + "’s " + typeStr
			}
		}
	}
	if opts.appendHebDate {
		if hebrewName {
			subj += " (" + gematriya.Gematriya(hd.Day()) + " " + hd.MonthName("he-x-NoNikud") + ")"
		} else {
			subj += " (" + dayAndMonth(hd) + ")"
		}
	}
	return subj
}

// dayAndMonth renders a Hebrew date without its year: "20th of Cheshvan".
func dayAndMonth(hd hdate.HDate) string {
	return humanize.Ordinal(hd.Day()) + " of " + jsutil.SmartApostrophe(model.HDMonthNameEn(hd))
}

// longDate renders a Gregorian date as "Wednesday, February 25".
func longDate(hd hdate.HDate) string {
	y, m, d := hd.ProlepticGreg()
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC).Format("Monday, January 2")
}

// candlesWhen says when the memorial candle is lit on the eve of a Yahrzeit:
// before Shabbat begins, after it ends, or otherwise as night falls.
func candlesWhen(erev time.Weekday) string {
	switch erev {
	case time.Friday:
		return "before sundown"
	case time.Saturday:
		return "at nightfall"
	}
	return "at dusk"
}

// makeMemo is the event's description. yearNumber zero leaves the anniversary
// number out.
func makeMemo(e entry, hd hdate.HDate, yearNumber int, editURL string) string {
	isYahrzeit := e.typ == typeYahrzeit
	isOther := e.typ == typeOther
	typeStr := e.typ
	if !isYahrzeit {
		typeStr = "Hebrew " + e.typ
	}
	erev := hd.Prev()

	var b strings.Builder
	if isOther {
		b.WriteString(e.name)
	} else {
		verb := "honoring"
		if isYahrzeit {
			verb = "remembering"
		}
		b.WriteString("Hebcal joins you in " + verb + " " + e.name + ", whose ")
		if yearNumber != 0 {
			b.WriteString(humanize.Ordinal(yearNumber) + " ")
		}
		b.WriteString(typeStr)
	}
	b.WriteString(" occurs on " + longDate(hd) + ", corresponding to the " +
		dayAndMonth(hd) + ", " + strconv.Itoa(hd.Year()) + ".\n\n")
	if isOther {
		b.WriteString(e.name)
	} else {
		b.WriteString(e.name + "’s " + typeStr)
	}
	b.WriteString(" begins at sundown on " + longDate(erev) +
		" and continues until sundown on the day of observance.")
	switch {
	case isYahrzeit:
		b.WriteString(" It is customary to light a memorial candle " + candlesWhen(erev.Weekday()) +
			" as the Yahrzeit begins.\n\n" +
			"May your loved one’s soul be bound up in the bond of eternal life and may their memory " +
			"serve as a continued source of inspiration and comfort to you.")
	case e.typ == typeBirthday:
		b.WriteString("\n\nMazel Tov!")
	}
	if editURL != "" {
		b.WriteString("\n\n" + editURL)
	}
	return b.String()
}

// makeReminder builds the reminder for a Yahrzeit: a timed event on its eve,
// late enough in the afternoon to act on and before Shabbat when the eve is a
// Friday, whose alarm fires as it starts.
func makeReminder(e entry, ev calEvent, hash string) calEvent {
	hd := ev.date.Prev()
	subj := e.name + " Yahrzeit reminder"
	if hasHebrew(e.name) {
		subj = e.name + " " + yahrzeitHe + " תזכורת"
	}
	r := calEvent{
		date:  hd,
		subj:  subj,
		emoji: ev.emoji,
		memo:  ev.memo,
		uid:   "reminder-" + isoDate(hd).String() + "-" + hash + "-" + strconv.Itoa(e.id),
		alarm: icalendar.RelativeAlarm("P0DT0H0M0S"),
		timed: true,
	}
	switch hd.Weekday() {
	case time.Saturday:
		r.hour, r.minute = 20, 0
	case time.Friday:
		r.hour, r.minute = 14, 30
	default:
		r.hour, r.minute = 16, 30
	}
	return r
}

// isoDate is the Gregorian date a Hebrew date falls on.
func isoDate(hd hdate.HDate) icalendar.Date {
	y, m, d := hd.ProlepticGreg()
	return icalendar.Date{Year: y, Month: m, Day: d}
}

// makeYizkorEvents returns the four days a year on which Yizkor is recited,
// for each year of the range.
func makeYizkorEvents(r dateRange, il bool, calendarID string) []calEvent {
	pesachDay, pesachDesc := 22, "Yizkor (Pesach VIII)"
	shavuotDay, shavuotDesc := 7, "Yizkor (Shavuot II)"
	if il {
		pesachDay, pesachDesc = 21, "Yizkor (Pesach VII)"
		shavuotDay, shavuotDesc = 6, "Yizkor (Shavuot)"
	}
	memo := ""
	if calendarID != "" {
		memo = editMemo(calendarID)
	}
	var out []calEvent
	add := func(hd hdate.HDate, desc string) {
		out = append(out, calEvent{
			date:  hd,
			subj:  desc,
			emoji: emojiCandle,
			memo:  memo,
			uid:   "yizkor-" + isoDate(hd).String() + "-" + icalendar.Murmur32Hex(desc),
			alarm: icalendar.AlarmUserEvent,
		})
	}
	for hyear := r.startYear; hyear <= r.endYear && hyear-r.startYear < maxYears; hyear++ {
		add(hdate.New(hyear, hdate.Nisan, pesachDay), pesachDesc)
		add(hdate.New(hyear, hdate.Sivan, shavuotDay), shavuotDesc)
		add(hdate.New(hyear, hdate.Tishrei, 10), "Yizkor (Yom Kippur)")
		add(hdate.New(hyear, hdate.Tishrei, 22), "Yizkor (Shmini Atzeret)")
	}
	return out
}

// editMemo points at the page where a saved calendar is edited.
func editMemo(calendarID string) string {
	return "To edit this Hebcal Yahrzeit + Anniversary Calendar, visit https://www.hebcal.com/yahrzeit/edit/" +
		calendarID
}
