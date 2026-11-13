package yahrzeit

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/zmanim"
	"github.com/twmb/murmur3"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/service/csv"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// GoogleImporter is the User-Agent Google Calendar fetches a subscribed feed
// with. It does not understand floating times, so its copy of a calendar puts
// the reminders in a real time zone.
const GoogleImporter = "Google-Calendar-Importer"

// maxSubscriptionEvents caps a subscribed feed; a one-time download is whole.
const maxSubscriptionEvents = 1200

// lookBackDays is how far behind this week a subscribed feed still reaches.
const lookBackDays = 12 * 7

// Calendar is a personal calendar resolved from its request, ready to be
// rendered.
type Calendar struct {
	req   *Request
	rng   dateRange
	today hdate.HDate
	// RelCalID identifies a saved calendar (Request.ID); legacy URLs have
	// none.
	RelCalID string
}

// New resolves a request into a calendar as of today. It fails only on an
// out-of-range start year; the entries themselves are read by ICS.
func New(req *Request, today model.GregDate) (*Calendar, error) {
	hd := hdate.FromProlepticGregorian(today.Year, today.Month, today.Day)
	rng, err := rangeFor(req.Query, hd)
	if err != nil {
		return nil, err
	}
	return &Calendar{req: req, rng: rng, today: hd, RelCalID: req.ID}, nil
}

// Attachment reports whether the calendar is a one-time download (dl=1)
// rather than a subscription.
func (c *Calendar) Attachment() bool { return c.req.Query.Get("dl") == "1" }

// sunday is the day number of the Sunday that begins the current week.
func (c *Calendar) sunday() int64 {
	return c.today.OnOrBefore(time.Sunday).Abs()
}

// Variant names everything outside the URL that a rendering depends on: the
// saved calendar's version, the years it spans, the week it was made in (a subscription drops events as
// they recede into the past), and whether it is Google's copy. A cache
// validator has to include it.
//
// A .csv is a one-time export whatever its URL says: it holds every event and
// no reminders, so neither the week nor the client changes it.
func (c *Calendar) Variant(userAgent string) string {
	v := strconv.Itoa(c.rng.startYear) + "-" + strconv.Itoa(c.rng.endYear) + "/" + c.req.RawQuery
	if c.req.Version != "" {
		v += "/" + c.req.Version
	}
	if c.req.Ext() == ".csv" {
		return v
	}
	if !c.Attachment() {
		v += "/" + strconv.FormatInt(c.sunday(), 10)
	}
	if userAgent == GoogleImporter {
		v += "/google"
	}
	return v
}

// Result is a rendered calendar.
type Result struct {
	Body []byte
	// Empty is true when the calendar had no events and Body holds a single
	// placeholder event instead. Such a response is worth a day, not a week:
	// it describes a moment, not a calendar.
	Empty bool
}

// ICS renders the calendar as an iCalendar feed for the given client. zones
// supplies the VTIMEZONE for the copies that carry one; now stamps the events.
func (c *Calendar) ICS(userAgent string, zones *icalendar.ZoneInfo, now time.Time) (*Result, error) {
	q := c.req.Query
	reminder := q.Get("yrem") != "0" && q.Get("yrem") != "off"
	events, err := makeEvents(q, c.rng, reminder, true)
	if err != nil {
		return nil, err
	}
	attachment := c.Attachment()
	if !attachment {
		// A subscription is refreshed forever, so it keeps only what is
		// recent or still to come, and never grows without bound.
		startAbs := c.sunday() - lookBackDays
		kept := events[:0]
		for _, ev := range events {
			if ev.date.Abs() >= startAbs {
				kept = append(kept, ev)
			}
		}
		events = kept
		if len(events) > maxSubscriptionEvents {
			events = events[:maxSubscriptionEvents]
		}
	}

	title := calendarTitle(q, 64)
	desc := "Personal anniversaries from Hebcal.com"
	if full := calendarTitle(q, math.MaxInt); full != title {
		desc = "Hebcal " + full
	}
	cal := &icalendar.Calendar{
		Title:        title,
		Description:  desc,
		PublishedTTL: "P1D",
		Zones:        zones,
		Stamp:        now,
	}
	if c.RelCalID != "" {
		cal.RelCalID = "hebcal-" + c.RelCalID
	}
	// A URL may override the calendar's own metadata.
	if v := q.Get("title"); v != "" {
		cal.Title = v
	}
	if v := q.Get("caldesc"); v != "" {
		cal.Description = v
	}
	if v := q.Get("publishedTTL"); v != "" {
		cal.PublishedTTL = v
	}
	if v := q.Get("relcalid"); v != "" {
		cal.RelCalID = v
	}
	cal.Color = strings.ToUpper(q.Get("color"))
	emoji := true
	if v := q.Get("emoji"); v == "0" || v == "off" {
		emoji = false
	}

	// The reminders are floating times -- 4:30pm wherever the subscriber is.
	// Two kinds of calendar pin them to a zone instead: one that follows the
	// Israeli schedule, and Google's copy, which is told the zone the browser
	// reported when the calendar was made (tzo, in minutes west of UTC).
	var loc *zmanim.Location
	if reminder && anniversaryTypes(q)[typeYahrzeit] {
		if q.Get("i") == "on" {
			loc = zmanim.LookupCity("Jerusalem")
		} else if userAgent == GoogleImporter {
			loc = googleLocation(q.Get("tzo"))
		}
	}
	var geo *icalendar.Geo
	if loc != nil {
		cal.TZID = loc.TimeZoneID
		geo = &icalendar.Geo{Latitude: loc.Latitude, Longitude: loc.Longitude}
	}

	res := &Result{}
	if len(events) == 0 {
		events = []calEvent{c.placeholder(now)}
		cal.NoPublishedTTL = true
		res.Empty = true
	}
	sequence := sequenceOf(q.Get("seq"))
	out := make([]icalendar.Event, len(events))
	for i, ev := range events {
		subj := ev.subj
		if emoji && ev.emoji != "" {
			subj = ev.emoji + " " + subj
		}
		out[i] = icalendar.Event{
			UID:         ev.uid,
			Summary:     subj,
			Date:        isoDate(ev.date),
			Timed:       ev.timed,
			Hour:        ev.hour,
			Minute:      ev.minute,
			Sequence:    sequence,
			Category:    "Personal",
			Private:     true,
			Description: ev.memo,
			Alarm:       ev.alarm,
		}
		if ev.timed {
			out[i].TZID = cal.TZID
			out[i].Geo = geo
		}
	}
	res.Body, err = cal.Marshal(out)
	return res, err
}

// CSV renders the calendar as an Outlook-compatible CSV document, in the
// format of the holiday CSV calendars. Unlike a feed it is a file to import
// once: it holds every observance in the range however long ago, has no
// reminder events, and with no events at all is a header row alone.
func (c *Calendar) CSV() ([]byte, error) {
	q := c.req.Query
	events, err := makeEvents(q, c.rng, false, false)
	if err != nil {
		return nil, err
	}
	// Any value turns on day/month/year, even euro=0 or euro=off.
	euro := q.Get("euro") != ""
	rows := make([][]string, len(events))
	for i, ev := range events {
		y, m, d := ev.date.ProlepticGreg()
		rows[i] = []string{
			ev.subj,
			csv.FormatDate(y, m, d, euro),
			"", "", "", "true",
			csv.FlattenNewlines(ev.memo),
			"3", // free, not busy: nothing here is a festival
			"Personal",
		}
	}
	return csv.Document(rows), nil
}

// sequenceOf reads the seq= revision number; anything but a nonzero integer
// is revision 1.
func sequenceOf(s string) int {
	if n, ok := jsutil.ParseInt(s); ok && n != 0 && strconv.Itoa(n) == strings.TrimSpace(s) {
		return n
	}
	return 1
}

// placeholder is the single event of a calendar that would otherwise be
// empty, which several applications refuse to subscribe to.
func (c *Calendar) placeholder(now time.Time) calEvent {
	id := c.RelCalID
	memo := "To create a new Hebcal Yahrzeit + Anniversary Calendar, visit https://www.hebcal.com/yahrzeit"
	if id != "" {
		memo = editMemo(id)
	} else {
		h1, h2 := murmur3.StringSum128(c.req.Path + "?" + c.req.RawQuery)
		id = fmt.Sprintf("%016x%016x", h1, h2)
	}
	return calEvent{
		date: c.today,
		subj: "Calendar contains no events",
		memo: memo,
		uid:  "yahrzeit-" + isoDate(c.today).String() + "-" + id + "-dummy",
	}
}

// anniversaryTypes returns the set of entry types the calendar names, with
// "Other" counted as an anniversary.
func anniversaryTypes(q *Query) map[string]bool {
	types := make(map[string]bool)
	for _, key := range q.Keys() {
		if isNumKey(key, 't') {
			typ := anniversaryType(q.Get(key))
			if typ == typeOther {
				typ = typeAnniversary
			}
			types[typ] = true
		}
	}
	return types
}

// calendarTitle names the calendar after what it holds and whom:
// "Yahrzeit: Ploni Almoni, Plonit Almonit". A title longer than maxLen
// UTF-16 code units -- the unit the limit has always been counted in -- is
// cut to fit and ends in an ellipsis.
func calendarTitle(q *Query, maxLen int) string {
	types := anniversaryTypes(q)
	title := typeYahrzeit
	switch {
	case len(types) > 1:
		title = "Hebrew Anniversaries"
	case types[typeAnniversary]:
		title = "Hebrew Anniversary"
	case types[typeBirthday]:
		title = "Hebrew Birthday"
	}
	var names []string
	for _, key := range q.Keys() {
		if isNumKey(key, 'n') && q.Get(key) != "" {
			names = append(names, q.Get(key))
		}
	}
	if len(names) > 0 {
		title += ": " + strings.Join(names, ", ")
	}
	if len(title) > maxLen { // bytes >= code units, so a short title skips the encoding
		if units := utf16.Encode([]rune(title)); len(units) > maxLen {
			title = string(utf16.Decode(units[:maxLen-3])) + "..."
		}
	}
	return title
}

// googleLocation turns the browser's time zone offset into a zone for
// Google's copy of the calendar: the United States zones across North
// America's offsets, the European ones around Greenwich, and a fixed offset
// anywhere else. With no offset on record it is New York.
func googleLocation(tzo string) *zmanim.Location {
	minutes, ok := jsutil.ParseInt(tzo)
	if !ok {
		return zmanim.LookupCity("New York")
	}
	tz := float64(minutes) / -60
	hours, whole := int(tz), tz == math.Trunc(tz)
	var tzid string
	switch {
	case tz <= -4 && tz >= -10:
		if whole {
			tzid = geodb.ZipcodesTzMap[-hours]
		}
	case tz <= 2 && tz >= -2:
		if whole {
			tzid = [...]string{"Atlantic/Cape_Verde", "Atlantic/Azores", "Europe/London", "Europe/Paris", "Europe/Athens"}[hours+2]
		}
	default:
		// The Etc zones count westward: Etc/GMT-5 is five hours east.
		tzid = "Etc/GMT"
		if tz > 0 {
			tzid += "+"
		}
		tzid += strconv.FormatFloat(tz, 'f', -1, 64)
	}
	return &zmanim.Location{TimeZoneID: tzid}
}
