// Package icalendar writes RFC 5545 iCalendar feeds: VEVENTs with their
// VALARMs, wrapped in a VCALENDAR that can carry a VTIMEZONE.
//
// It is a serializer and knows nothing about the Jewish calendar. A caller
// resolves each of its events into an [Event] -- summary, date, optional
// wall-clock time, UID, description, alarm -- and a [Calendar] writes them
// out, folding long lines (see [FoldLine]) and ending every line with CRLF.
package icalendar

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/config"
	"github.com/twmb/murmur3"
)

// Date is a calendar date with no time of day and no time zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the calendar date of t in t's own location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// AddDays returns the date n days later (earlier when n is negative).
func (d Date) AddDays(n int) Date {
	return DateOf(time.Date(d.Year, d.Month, d.Day+n, 12, 0, 0, 0, time.UTC))
}

// String formats the date as YYYYMMDD.
func (d Date) String() string {
	var buf [8]byte
	return string(d.append(buf[:0]))
}

func (d Date) append(b []byte) []byte {
	b = appendPadded(b, d.Year, 4)
	b = appendPadded(b, int(d.Month), 2)
	return appendPadded(b, d.Day, 2)
}

func appendPadded(b []byte, n, width int) []byte {
	s := strconv.Itoa(n)
	for i := len(s); i < width; i++ {
		b = append(b, '0')
	}
	return append(b, s...)
}

// Alarm is the trigger of an event's VALARM.
type Alarm struct {
	trigger string
}

// The reminders the feeds use by default.
var (
	// AlarmUserEvent fires at noon on the day before a personal all-day event.
	AlarmUserEvent = RelativeAlarm("-P0DT12H0M0S")
	// AlarmOmer fires at 8:30pm on the evening before an Omer day.
	AlarmOmer = RelativeAlarm("-P0DT3H30M0S")
	// AlarmCandleLighting fires ten minutes before candle lighting.
	AlarmCandleLighting = RelativeAlarm("-P0DT0H10M0S")
)

// RelativeAlarm returns an alarm that fires at an RFC 5545 duration relative
// to the start of its event, such as "-P0DT12H0M0S" for twelve hours before
// or "P0DT0H0M0S" for the moment it starts.
func RelativeAlarm(duration string) *Alarm {
	return &Alarm{trigger: "TRIGGER:" + duration}
}

// AbsoluteAlarm returns an alarm that fires at a fixed instant, truncated to
// the minute.
func AbsoluteAlarm(t time.Time) *Alarm {
	return &Alarm{trigger: "TRIGGER;VALUE=DATE-TIME:" + FormatUTC(t.Truncate(time.Minute))}
}

// Geo is the position a timed event's GEO property reports.
type Geo struct {
	Latitude, Longitude float64
}

// Event is one VEVENT.
type Event struct {
	// UID identifies the event across refreshes of a subscribed feed, so it
	// has to be the same every time the event is generated. See [DefaultUID].
	UID string
	// Summary is the event's title, as plain text.
	Summary string
	// SummarySuffix is written after the escaped Summary as it is, unescaped:
	// the " / " and Hebrew name an English-and-Hebrew calendar appends.
	SummarySuffix string
	// Date is the day the event falls on.
	Date Date
	// Timed marks an event that starts at Hour:Minute on Date rather than
	// lasting all day. It ends Duration minutes later (at once, when zero).
	Timed    bool
	Hour     int
	Minute   int
	Duration int
	// TZID names the time zone a timed event's wall-clock time is in. Empty
	// makes it a floating time, the same wall-clock time wherever it is read.
	TZID string
	// Sequence is the event's revision number; zero omits it.
	Sequence int
	// Category is the CATEGORIES value; empty omits it.
	Category string
	// Busy marks an all-day event that blocks the day out (a festival on
	// which work is forbidden). Everything else is transparent.
	Busy bool
	// Private omits CLASS:PUBLIC, for events about the calendar's owner
	// rather than the public calendar.
	Private bool
	// Description is the event's notes, as plain text; a newline becomes the
	// escaped "\n". Empty omits it.
	Description string
	// Location is the LOCATION value; empty omits it.
	Location string
	// Geo is the event's position, written for timed events only.
	Geo *Geo
	// URL is the RFC 5545 URL property; empty omits it.
	URL string
	// RRule repeats the event, an RFC 5545 recurrence rule such as
	// "FREQ=YEARLY;INTERVAL=1;BYMONTH=3"; empty omits it.
	RRule string
	// Alarm adds a display reminder; nil means none.
	Alarm *Alarm
}

// Calendar is the VCALENDAR an event list is wrapped in.
type Calendar struct {
	// Title is the calendar's display name (X-WR-CALNAME).
	Title string
	// Description is X-WR-CALDESC; empty omits it.
	Description string
	// Lang is the two-letter language of the default PRODID; empty means EN.
	Lang string
	// Prodid overrides the default PRODID.
	Prodid string
	// PublishedTTL is how often a subscriber should refresh, as an RFC 5545
	// duration; empty means a week. NoPublishedTTL omits the hint entirely.
	PublishedTTL   string
	NoPublishedTTL bool
	// RelCalID is X-WR-RELCALID, a stable identifier for the calendar.
	RelCalID string
	// Color is X-APPLE-CALENDAR-COLOR.
	Color string
	// TZID is the calendar's time zone (X-WR-TIMEZONE). When Zones has a
	// definition for it, the VTIMEZONE is included.
	TZID  string
	Zones *ZoneInfo
	// Stamp is every event's DTSTAMP; the zero value means now.
	Stamp time.Time
}

// ErrNoEvents is returned for an empty event list: a VCALENDAR with nothing
// in it is rejected by several calendar applications.
var ErrNoEvents = errors.New("events can not be empty")

// Marshal writes the calendar and its events as an iCalendar document.
func (c *Calendar) Marshal(events []Event) ([]byte, error) {
	if len(events) == 0 {
		return nil, ErrNoEvents
	}
	var buf bytes.Buffer
	buf.Grow(1024 + 640*len(events))
	c.writePreamble(&buf)
	if c.TZID != "" {
		buf.WriteString("X-WR-TIMEZONE;VALUE=TEXT:")
		buf.WriteString(c.TZID)
		buf.WriteString(crlf)
		if vtz, ok := c.Zones.VTimezone(c.TZID); ok {
			buf.WriteString(vtz)
			buf.WriteString(crlf)
		}
	}
	stamp := c.Stamp
	if stamp.IsZero() {
		stamp = time.Now()
	}
	dtstamp := "DTSTAMP:" + FormatUTC(stamp)
	for i := range events {
		events[i].write(&buf, dtstamp)
	}
	buf.WriteString("END:VCALENDAR\r\n")
	return buf.Bytes(), nil
}

const crlf = "\r\n"

// line writes one content line, folded. A line break inside the content --
// from a name or title a request supplied -- is written as the escaped "\n",
// so a value can never end its own line and begin another property.
func line(buf *bytes.Buffer, parts ...string) {
	content := parts[0]
	if len(parts) > 1 {
		content = strings.Join(parts, "")
	}
	if strings.IndexByte(content, '\r') >= 0 {
		content = carriageReturns.Replace(content)
	}
	if strings.IndexByte(content, '\n') >= 0 {
		content = strings.ReplaceAll(content, "\n", `\n`)
	}
	writeFolded(buf, content)
	buf.WriteString(crlf)
}

// carriageReturns turns the rarer line breaks into the plain newline.
var carriageReturns = strings.NewReplacer("\r\n", "\n", "\r", "\n")

func (c *Calendar) writePreamble(buf *bytes.Buffer) {
	prodid := c.Prodid
	if prodid == "" {
		lang := "EN"
		if len(c.Lang) == 2 {
			lang = strings.ToUpper(c.Lang)
		}
		prodid = "-//hebcal.com/NONSGML Hebcal Calendar v2" + strings.TrimPrefix(config.APIVersion, "v") + "//" + lang
	}
	line(buf, "BEGIN:VCALENDAR")
	line(buf, "VERSION:2.0")
	line(buf, "PRODID:", prodid)
	line(buf, "CALSCALE:GREGORIAN")
	line(buf, "METHOD:PUBLISH")
	line(buf, "X-LOTUS-CHARSET:UTF-8")
	if !c.NoPublishedTTL {
		ttl := c.PublishedTTL
		if ttl == "" {
			ttl = "P7D"
		}
		line(buf, "REFRESH-INTERVAL;VALUE=DURATION:", ttl)
		line(buf, "X-PUBLISHED-TTL:", ttl)
	}
	title := "Untitled"
	if c.Title != "" {
		title = Escape(c.Title)
	}
	line(buf, "X-WR-CALNAME:", title)
	if c.Description != "" {
		line(buf, "X-WR-CALDESC:", Escape(c.Description))
	}
	if c.RelCalID != "" {
		line(buf, "X-WR-RELCALID:", c.RelCalID)
	}
	if c.Color != "" {
		line(buf, "X-APPLE-CALENDAR-COLOR:", c.Color)
	}
}

// write appends the event's VEVENT.
func (e *Event) write(buf *bytes.Buffer, dtstamp string) {
	line(buf, "BEGIN:VEVENT")
	line(buf, dtstamp)
	if e.Sequence != 0 {
		line(buf, "SEQUENCE:", strconv.Itoa(e.Sequence))
	}
	if e.Category != "" {
		line(buf, "CATEGORIES:", e.Category)
	}
	line(buf, "SUMMARY:", Escape(e.Summary), e.SummarySuffix)

	start, end, args := e.startEnd()
	line(buf, "DTSTART", args, ":", start)
	line(buf, "DTEND", args, ":", end)
	line(buf, "UID:", e.UID)
	if e.Busy && !e.Timed {
		line(buf, "TRANSP:OPAQUE")
		line(buf, "X-MICROSOFT-CDO-BUSYSTATUS:OOF")
	} else {
		line(buf, "TRANSP:TRANSPARENT")
		line(buf, "X-MICROSOFT-CDO-BUSYSTATUS:FREE")
	}
	if !e.Timed {
		line(buf, "X-MICROSOFT-CDO-ALLDAYEVENT:TRUE")
	}
	if !e.Private {
		line(buf, "CLASS:PUBLIC")
	}
	if e.Description != "" {
		line(buf, "DESCRIPTION:", Escape(e.Description))
	}
	if e.Location != "" {
		line(buf, "LOCATION:", Escape(e.Location))
	}
	if e.Timed && e.Geo != nil {
		line(buf, "GEO:", formatFloat(e.Geo.Latitude), ";", formatFloat(e.Geo.Longitude))
	}
	if e.URL != "" {
		line(buf, "URL:", e.URL)
	}
	if e.RRule != "" {
		line(buf, "RRULE:", e.RRule)
	}
	if e.Alarm != nil {
		line(buf, "BEGIN:VALARM")
		line(buf, "ACTION:DISPLAY")
		line(buf, "DESCRIPTION:Event reminder")
		line(buf, e.Alarm.trigger)
		line(buf, "END:VALARM")
	}
	line(buf, "END:VEVENT")
}

// startEnd returns the DTSTART and DTEND values and the parameters both
// carry.
//
// An all-day event runs from its date to the next (DTEND;VALUE=DATE rather
// than DURATION:P1D, which fewer applications understand). A timed event's
// end is its start plus Duration, added on the wall clock so the result is in
// the same zone as the start whatever zone this process runs in.
func (e *Event) startEnd() (start, end, args string) {
	if !e.Timed {
		return e.Date.String(), e.Date.AddDays(1).String(), ";VALUE=DATE"
	}
	if e.TZID != "" {
		args = ";TZID=" + e.TZID
	}
	start = dateTime(e.Date, e.Hour, e.Minute)
	if e.Duration == 0 {
		return start, start, args
	}
	total := e.Hour*60 + e.Minute + e.Duration
	end = dateTime(e.Date.AddDays(total/1440), total/60%24, total%60)
	return start, end, args
}

// dateTime formats a local date-time as YYYYMMDDTHHMM00.
func dateTime(d Date, hour, minute int) string {
	var buf [15]byte
	b := d.append(buf[:0])
	b = append(b, 'T')
	b = appendPadded(b, hour, 2)
	b = appendPadded(b, minute, 2)
	return string(append(b, '0', '0'))
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Escape makes text safe for a TEXT property value by escaping the two
// characters that would otherwise separate values: the comma and the
// semicolon.
func Escape(s string) string {
	if !strings.ContainsAny(s, ",;") {
		return s
	}
	return escaper.Replace(s)
}

var escaper = strings.NewReplacer(",", `\,`, ";", `\;`)

// FormatUTC formats an instant as an iCalendar UTC date-time,
// YYYYMMDDTHHMMSSZ.
func FormatUTC(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// Murmur32Hex returns the 32-bit MurmurHash3 of s as eight lowercase hex
// digits. Event UIDs are built from it, so it must never change: a different
// hash would hand every subscriber a second copy of each event.
func Murmur32Hex(s string) string {
	const hex = "0123456789abcdef"
	h := murmur3.StringSum32(s)
	var buf [8]byte
	for i := 7; i >= 0; i-- {
		buf[i] = hex[h&0xf]
		h >>= 4
	}
	return string(buf[:])
}

// DefaultUID builds the UID of an event that has none of its own, from the
// day it falls on and its untranslated description: "hebcal-20260402-1a2b3c4d".
// A non-empty place, which a timed event's time depends on, is appended.
func DefaultUID(date Date, desc, place string) string {
	uid := "hebcal-" + date.String() + "-" + Murmur32Hex(desc)
	if place != "" {
		uid += "-" + place
	}
	return uid
}
