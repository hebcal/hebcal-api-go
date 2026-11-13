package icalendar

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var stamp = time.Date(2026, time.November, 10, 17, 30, 32, 0, time.UTC)

func marshal(t *testing.T, c *Calendar, events ...Event) []string {
	t.Helper()
	if c.Stamp.IsZero() {
		c.Stamp = stamp
	}
	out, err := c.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasSuffix(s, "\r\n") || strings.Contains(strings.ReplaceAll(s, "\r\n", ""), "\n") {
		t.Fatalf("lines must end in CRLF: %q", s)
	}
	return strings.Split(strings.TrimSuffix(s, "\r\n"), "\r\n")
}

var prodidVersion = regexp.MustCompile(`Calendar v[^/]*//`)

func equal(t *testing.T, got, want []string) {
	t.Helper()
	// The PRODID names the build, which varies; compare it without the version.
	got = append([]string(nil), got...)
	for i, l := range got {
		if strings.HasPrefix(l, "PRODID:") {
			got[i] = prodidVersion.ReplaceAllString(l, "Calendar v//")
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAllDayEvent(t *testing.T) {
	got := marshal(t, &Calendar{Title: "Yahrzeit: Golda Meir"}, Event{
		UID:         "yahrzeit-5787-0a1da0e9-1",
		Summary:     "🕯️ Golda Meir’s 48th Yahrzeit",
		Date:        Date{2026, time.November, 18},
		Sequence:    1,
		Category:    "Personal",
		Private:     true,
		Description: "Line one, with a comma; and a semicolon.\n\nLine two.",
	})
	equal(t, got, []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//hebcal.com/NONSGML Hebcal Calendar v//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"X-LOTUS-CHARSET:UTF-8",
		"REFRESH-INTERVAL;VALUE=DURATION:P7D",
		"X-PUBLISHED-TTL:P7D",
		"X-WR-CALNAME:Yahrzeit: Golda Meir",
		"BEGIN:VEVENT",
		"DTSTAMP:20261110T173032Z",
		"SEQUENCE:1",
		"CATEGORIES:Personal",
		"SUMMARY:🕯️ Golda Meir’s 48th Yahrzeit",
		"DTSTART;VALUE=DATE:20261118",
		"DTEND;VALUE=DATE:20261119",
		"UID:yahrzeit-5787-0a1da0e9-1",
		"TRANSP:TRANSPARENT",
		"X-MICROSOFT-CDO-BUSYSTATUS:FREE",
		"X-MICROSOFT-CDO-ALLDAYEVENT:TRUE",
		`DESCRIPTION:Line one\, with a comma\; and a semicolon.\n\nLine two.`,
		"END:VEVENT",
		"END:VCALENDAR",
	})
}

func event(lines []string) []string {
	for i, ln := range lines {
		if ln == "BEGIN:VEVENT" {
			return lines[i : len(lines)-1]
		}
	}
	return nil
}

func TestPublicBusyEvent(t *testing.T) {
	got := event(marshal(t, &Calendar{}, Event{
		UID: "u", Summary: "Pesach I", Date: Date{2027, time.April, 22}, Busy: true,
		Category: "Holiday", Location: "Jerusalem, Israel", URL: "https://hebcal.com/h/pesach-2027",
		// A position is a timed event's; an all-day one has none.
		Geo: &Geo{31.76904, 35.21633},
	}))
	equal(t, got, []string{
		"BEGIN:VEVENT",
		"DTSTAMP:20261110T173032Z",
		"CATEGORIES:Holiday",
		"SUMMARY:Pesach I",
		"DTSTART;VALUE=DATE:20270422",
		"DTEND;VALUE=DATE:20270423",
		"UID:u",
		"TRANSP:OPAQUE",
		"X-MICROSOFT-CDO-BUSYSTATUS:OOF",
		"X-MICROSOFT-CDO-ALLDAYEVENT:TRUE",
		"CLASS:PUBLIC",
		`LOCATION:Jerusalem\, Israel`,
		"URL:https://hebcal.com/h/pesach-2027",
		"END:VEVENT",
	})
}

func TestTimedEvents(t *testing.T) {
	floating := event(marshal(t, &Calendar{}, Event{
		UID: "reminder-20260224-bb94a9e2-1", Summary: "Reminder", Date: Date{2026, time.February, 24},
		Timed: true, Hour: 16, Minute: 30, Private: true, Alarm: RelativeAlarm("P0DT0H0M0S"),
	}))
	equal(t, floating, []string{
		"BEGIN:VEVENT",
		"DTSTAMP:20261110T173032Z",
		"SUMMARY:Reminder",
		"DTSTART:20260224T163000",
		"DTEND:20260224T163000",
		"UID:reminder-20260224-bb94a9e2-1",
		"TRANSP:TRANSPARENT",
		"X-MICROSOFT-CDO-BUSYSTATUS:FREE",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:Event reminder",
		"TRIGGER:P0DT0H0M0S",
		"END:VALARM",
		"END:VEVENT",
	})

	zoned := event(marshal(t, &Calendar{}, Event{
		UID: "u", Summary: "Candle lighting", Date: Date{2026, time.December, 31},
		Timed: true, Hour: 23, Minute: 45, Duration: 30, TZID: "America/New_York",
		Geo: &Geo{40.71427, -74.00597}, Busy: true, Alarm: AlarmCandleLighting,
	}))
	equal(t, zoned, []string{
		"BEGIN:VEVENT",
		"DTSTAMP:20261110T173032Z",
		"SUMMARY:Candle lighting",
		"DTSTART;TZID=America/New_York:20261231T234500",
		// The end is on the wall clock, across midnight and the year.
		"DTEND;TZID=America/New_York:20270101T001500",
		"UID:u",
		// A timed event never blocks the day out.
		"TRANSP:TRANSPARENT",
		"X-MICROSOFT-CDO-BUSYSTATUS:FREE",
		"CLASS:PUBLIC",
		"GEO:40.71427;-74.00597",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:Event reminder",
		"TRIGGER:-P0DT0H10M0S",
		"END:VALARM",
		"END:VEVENT",
	})
}

func TestAlarms(t *testing.T) {
	trigger := func(a *Alarm) string {
		for _, ln := range event(marshal(t, &Calendar{}, Event{UID: "u", Summary: "s", Date: Date{2026, 1, 1}, Alarm: a})) {
			if strings.HasPrefix(ln, "TRIGGER") {
				return ln
			}
		}
		return ""
	}
	for want, a := range map[string]*Alarm{
		"":                     nil,
		"TRIGGER:-P0DT12H0M0S": AlarmUserEvent,
		"TRIGGER:-P0DT3H30M0S": AlarmOmer,
		"TRIGGER:-P0DT0H10M0S": AlarmCandleLighting,
		"TRIGGER:-P1D":         RelativeAlarm("-P1D"),
		// An absolute alarm is in UTC, and loses its seconds.
		"TRIGGER;VALUE=DATE-TIME:20240114T173000Z": AbsoluteAlarm(time.Date(2024, 1, 14, 12, 30, 45, 0, time.FixedZone("EST", -5*3600))),
	} {
		if got := trigger(a); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if n := strings.Count(strings.Join(marshal(t, &Calendar{}, Event{UID: "u", Date: Date{2026, 1, 1}}), "\n"), "VALARM"); n != 0 {
		t.Error("an event with no alarm wrote a VALARM")
	}
}

func TestCalendarOptions(t *testing.T) {
	ev := Event{UID: "u", Summary: "s", Date: Date{2026, 1, 1}}
	preamble := func(c *Calendar) []string {
		lines := marshal(t, c, ev)
		for i, ln := range lines {
			if ln == "BEGIN:VEVENT" {
				return lines[2:i]
			}
		}
		return nil
	}
	equal(t, preamble(&Calendar{}), []string{
		"PRODID:-//hebcal.com/NONSGML Hebcal Calendar v//EN",
		"CALSCALE:GREGORIAN", "METHOD:PUBLISH", "X-LOTUS-CHARSET:UTF-8",
		"REFRESH-INTERVAL;VALUE=DURATION:P7D", "X-PUBLISHED-TTL:P7D",
		"X-WR-CALNAME:Untitled",
	})
	equal(t, preamble(&Calendar{
		Title: "Family, friends; others", Description: "Hebcal Yahrzeit: A, B", Lang: "he",
		PublishedTTL: "P1D", RelCalID: "hebcal-01ABC", Color: "#FF0000",
	}), []string{
		"PRODID:-//hebcal.com/NONSGML Hebcal Calendar v//HE",
		"CALSCALE:GREGORIAN", "METHOD:PUBLISH", "X-LOTUS-CHARSET:UTF-8",
		"REFRESH-INTERVAL;VALUE=DURATION:P1D", "X-PUBLISHED-TTL:P1D",
		`X-WR-CALNAME:Family\, friends\; others`,
		`X-WR-CALDESC:Hebcal Yahrzeit: A\, B`,
		"X-WR-RELCALID:hebcal-01ABC",
		"X-APPLE-CALENDAR-COLOR:#FF0000",
	})
	equal(t, preamble(&Calendar{Title: "T", Prodid: "-//example//EN", NoPublishedTTL: true, Lang: "he-x-NoNikud"}), []string{
		"PRODID:-//example//EN",
		"CALSCALE:GREGORIAN", "METHOD:PUBLISH", "X-LOTUS-CHARSET:UTF-8",
		"X-WR-CALNAME:T",
	})
}

func TestStampDefaultsToNow(t *testing.T) {
	before := FormatUTC(time.Now())
	out, _ := (&Calendar{}).Marshal([]Event{{UID: "u", Date: Date{2026, 1, 1}}})
	i := strings.Index(string(out), "DTSTAMP:") + len("DTSTAMP:")
	if got := string(out[i : i+16]); got < before || got > FormatUTC(time.Now()) {
		t.Errorf("DTSTAMP %s", got)
	}
}

func TestNoEvents(t *testing.T) {
	if _, err := (&Calendar{}).Marshal(nil); err != ErrNoEvents {
		t.Errorf("got %v", err)
	}
}

func TestLongLinesAreFolded(t *testing.T) {
	name := strings.Repeat("יצחק בן אברהם ", 12)
	lines := marshal(t, &Calendar{Title: name}, Event{UID: "u", Summary: name, Description: name + "\n" + name, Date: Date{2026, 1, 1}})
	for _, ln := range lines {
		if len(ln) > 75 {
			t.Errorf("%d octets: %q", len(ln), ln)
		}
	}
	if unfolded := strings.Join(lines, "\r\n"); !strings.Contains(strings.ReplaceAll(unfolded, "\r\n ", ""), "SUMMARY:"+name) {
		t.Error("summary did not survive folding")
	}
}

func TestLineBreaksAreEscaped(t *testing.T) {
	lines := marshal(t, &Calendar{Title: "a\r\nb\rc\nd", RelCalID: "x\nBEGIN:VEVENT"},
		Event{UID: "u\nv", Summary: "one\ntwo", Location: "here\r\nthere", Date: Date{2026, 1, 1}})
	joined := strings.Join(lines, "\n")
	for _, want := range []string{`X-WR-CALNAME:a\nb\nc\nd`, `X-WR-RELCALID:x\nBEGIN:VEVENT`, `UID:u\nv`, `SUMMARY:one\ntwo`, `LOCATION:here\nthere`} {
		if !strings.Contains(joined, want+"\n") {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	if strings.Count(joined, "\nBEGIN:VEVENT\n") != 1 {
		t.Error("a value started a component")
	}
}

func TestDate(t *testing.T) {
	for want, d := range map[string]Date{
		"20260101": {2026, time.January, 1},
		"09991231": {999, time.December, 31},
		"20240229": Date{2024, time.February, 28}.AddDays(1),
		"20230301": Date{2023, time.February, 28}.AddDays(1),
		"20261231": Date{2027, time.January, 1}.AddDays(-1),
		"20261110": DateOf(time.Date(2026, 11, 10, 23, 59, 0, 0, time.FixedZone("x", -8*3600))),
	} {
		if got := d.String(); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

// The UIDs of every event already in a subscriber's calendar are built from
// this hash; these are values the feeds have published for years.
func TestMurmur32Hex(t *testing.T) {
	for in, want := range map[string]string{
		"":                    "00000000",
		"a":                   "3c2569b2",
		"Ploni Almoni":        "eefa572d",
		"Golda Meir":          "0a1da0e9",
		"גולדה":               "ad8ec8b6",
		"Yizkor (Yom Kippur)": "c1c09604",
		"Person1":             "b3e6056e",
		"🕯️ candle":           "6e6929fd",
	} {
		if got := Murmur32Hex(in); got != want {
			t.Errorf("Murmur32Hex(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDefaultUID(t *testing.T) {
	d := Date{2026, time.September, 21}
	if got, want := DefaultUID(d, "Yizkor (Yom Kippur)", ""), "hebcal-20260921-c1c09604"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got, want := DefaultUID(d, "Yizkor (Yom Kippur)", "5128581"), "hebcal-20260921-c1c09604-5128581"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestVTimezone(t *testing.T) {
	z := NewZoneInfo("testdata/zoneinfo")
	vtz, ok := z.VTimezone("America/Los_Angeles")
	if !ok {
		t.Fatal("not found")
	}
	if !strings.HasPrefix(vtz, "BEGIN:VTIMEZONE\r\nTZID:America/Los_Angeles\r\n") ||
		!strings.HasSuffix(vtz, "END:STANDARD\r\nEND:VTIMEZONE") || strings.Contains(vtz, "VCALENDAR") {
		t.Errorf("unexpected component:\n%s", vtz)
	}
	// Read once: the definition outlives its file.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Asia"), 0o755)
	file := filepath.Join(dir, "Asia", "Jerusalem.ics")
	// A file with bare newlines is normalized.
	os.WriteFile(file, []byte("BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VTIMEZONE\nTZID:Asia/Jerusalem\nEND:VTIMEZONE\nEND:VCALENDAR\n"), 0o644)
	z2 := NewZoneInfo(dir)
	first, _ := z2.VTimezone("Asia/Jerusalem")
	os.Remove(file)
	second, ok := z2.VTimezone("Asia/Jerusalem")
	if !ok || first != second || first != "BEGIN:VTIMEZONE\r\nTZID:Asia/Jerusalem\r\nEND:VTIMEZONE" {
		t.Errorf("cache: %q / %q", first, second)
	}
	// Flush forgets it: the removed file is now gone.
	z2.Flush()
	if _, ok := z2.VTimezone("Asia/Jerusalem"); ok {
		t.Error("Flush did not clear the cache")
	}
	(*ZoneInfo)(nil).Flush()

	for _, tzid := range []string{"", "Mars/Olympus_Mons", "../zoneinfo/America/Los_Angeles", "/etc/passwd",
		"America/../America/Los_Angeles", "America/Los_Angeles\x00", "America\\Los_Angeles", strings.Repeat("a", 100)} {
		if _, ok := z.VTimezone(tzid); ok {
			t.Errorf("%q should have no definition", tzid)
		}
	}
	var none *ZoneInfo
	if _, ok := none.VTimezone("America/Los_Angeles"); ok {
		t.Error("nil ZoneInfo")
	}
	// A file that is not a VTIMEZONE is not one.
	os.WriteFile(filepath.Join(dir, "Bad.ics"), []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"), 0o644)
	if _, ok := z2.VTimezone("Bad"); ok {
		t.Error("file without a VTIMEZONE")
	}
}

func TestCalendarTimezone(t *testing.T) {
	ev := Event{UID: "u", Summary: "s", Date: Date{2026, 1, 1}, Timed: true, Hour: 16, TZID: "America/Los_Angeles"}
	with := strings.Join(marshal(t, &Calendar{TZID: "America/Los_Angeles", Zones: NewZoneInfo("testdata/zoneinfo")}, ev), "\n")
	if !strings.Contains(with, "X-WR-CALNAME:Untitled\nX-WR-TIMEZONE;VALUE=TEXT:America/Los_Angeles\nBEGIN:VTIMEZONE\nTZID:America/Los_Angeles\n") ||
		!strings.Contains(with, "END:VTIMEZONE\nBEGIN:VEVENT\n") {
		t.Errorf("VTIMEZONE misplaced:\n%s", with)
	}
	// With no definition on hand the zone is still named.
	without := strings.Join(marshal(t, &Calendar{TZID: "Europe/London", Zones: NewZoneInfo("testdata/zoneinfo")}, ev), "\n")
	if !strings.Contains(without, "X-WR-TIMEZONE;VALUE=TEXT:Europe/London\nBEGIN:VEVENT") {
		t.Errorf("got:\n%s", without)
	}
	if nilZones := strings.Join(marshal(t, &Calendar{TZID: "Europe/London"}, ev), "\n"); nilZones != without {
		t.Error("nil Zones should behave like a missing definition")
	}
}

// A summary is escaped, its suffix is not: the " / " and Hebrew name of an
// English-and-Hebrew calendar go out as they are.
func TestSummarySuffix(t *testing.T) {
	got := marshal(t, &Calendar{Title: "t"}, Event{
		UID: "u", Summary: "Rosh Hashana, 5787", SummarySuffix: " / רֹאשׁ הַשָּׁנָה, א",
		Date: Date{2026, time.September, 12},
	})
	var summary string
	for _, l := range got {
		if strings.HasPrefix(l, "SUMMARY:") {
			summary = l
		}
	}
	if want := "SUMMARY:Rosh Hashana\\, 5787 / רֹאשׁ הַשָּׁנָה, א"; summary != want {
		t.Errorf("got %q, want %q", summary, want)
	}
}
