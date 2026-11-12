package ical

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/twmb/murmur3"
	"golang.org/x/sync/singleflight"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// MaxSubscriptionEvents is the longest a subscription may be. Calendar
// applications fail on longer feeds, so a longer one is cut to this many
// events and ends with a notice saying so.
const MaxSubscriptionEvents = 2399

// lookBackDays is how much past a too-long subscription keeps: twelve weeks.
const lookBackDays = 12 * 7

// defaultColor is the calendar colour Apple Calendar shows unless the
// subscription asks for another.
const defaultColor = "#800002"

// TorahReadings supplies the finished Torah and Haftarah text of the events
// that have a reading. *readings.Client implements it.
type TorahReadings interface {
	TorahMemos(ctx context.Context, start, end time.Time, il bool) ([]readings.TorahMemo, error)
}

// ReadingsError is a Torah reading the feed needs and could not be had. The
// feed is never served without the readings it should carry: the caller
// answers 501 when no readings-svc is configured and 503 when one did not
// answer, so the cached copy a proxy holds keeps serving meanwhile.
type ReadingsError struct {
	// Retryable is true when a configured readings-svc failed, false when
	// there is none.
	Retryable bool
	Err       error
}

func (e *ReadingsError) Error() string {
	if e.Err == nil {
		return "Torah readings are not available"
	}
	return "Torah readings are not available: " + e.Err.Error()
}

func (e *ReadingsError) Unwrap() error { return e.Err }

// Service renders holiday calendars as iCalendar feeds.
type Service struct {
	// Geo resolves geonameid and ZIP locations.
	Geo *geodb.DB
	// Learning fetches the daily-learning series with no Go schedule; nil
	// refuses a feed that asks for one.
	Learning *download.LearningFetcher
	// Readings supplies Torah readings; nil refuses a feed that has any.
	Readings TorahReadings
	// Clock reads the current time; nil means time.Now.
	Clock func() time.Time

	cacheOnce sync.Once
	cache     *lru.Cache[memoKey, TorahMemos]
	flight    singleflight.Group
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

// Request is a decoded feed URL.
type Request struct {
	Msg    *downloadpb.Download
	Params *download.Params
	Query  *download.ICSQuery
	// Missing lists the daily-learning series readings-svc has to supply.
	Missing []string
}

// Parse decodes an .ics download URL in any of its shapes. Its errors carry
// the statuses of the PDF and CSV routes': *download.NotFoundError is 404,
// *download.OutOfRangeError 410, and anything else 400.
func (s *Service) Parse(ctx context.Context, u *url.URL) (*Request, error) {
	msg, q, err := download.DecodeICS(ctx, u)
	if err != nil {
		return nil, err
	}
	download.IgnoreMonthMode(msg)
	params, err := download.ParamsFromMessage(msg, s.Geo)
	if err != nil {
		return nil, err
	}
	missing := download.UnsupportedSeries(msg)
	params.Opts.NumYears = download.NumYears(msg, params, len(missing))
	return &Request{Msg: msg, Params: params, Query: q, Missing: missing}, nil
}

// Variant is what a feed depends on that its URL does not say: the year a
// request for "now" resolved to. It goes into the ETag.
func (r *Request) Variant() string {
	return strconv.Itoa(r.Params.Opts.Year)
}

// Attachment reports whether the feed is a file to save rather than a
// subscription.
func (r *Request) Attachment() bool { return r.Query.Attachment() }

// entry is one event with the keys that put it in calendar order.
type entry struct {
	abs  int64
	rank int
	e    icalendar.Event
}

// Feed is a calendar's events, resolved and in order.
type Feed struct {
	req     *Request
	entries []entry
	opts    *Options
	// span is the first and last day of the calendar before it is cut, which
	// the title is built from.
	span span
}

// span is the dates a calendar covers and how many events it has.
type span struct {
	first, last icalendar.Date
	n           int
}

// Events is how many events the calendar holds before any are cut.
func (f *Feed) Events() int { return len(f.entries) }

// Generate builds a calendar's events: hebcal-go's, the readings-svc rows for
// the series it has no schedule for, and the Torah reading of each event that
// has one.
func (s *Service) Generate(ctx context.Context, r *Request) (*Feed, error) {
	p := r.Params
	events, err := download.GenerateCalEvents(p)
	if err != nil {
		return nil, model.Internal("render: %s", err.Error())
	}
	var items []readings.Item
	if len(r.Missing) > 0 {
		if s.Learning == nil {
			return nil, &download.UnsupportedSeriesError{Series: r.Missing}
		}
		start, end := download.LearningRange(p)
		items, err = s.Learning.FetchItems(ctx, r.Missing, p.LG, start, end)
		if err != nil {
			return nil, &download.UnsupportedSeriesError{Series: r.Missing, Retryable: true, Err: err}
		}
	}

	opt := s.options(r, events)
	memos, err := s.torahMemos(ctx, events, opt.IL)
	if err != nil {
		return nil, err
	}
	opt.TorahMemos = memos

	f := &Feed{req: r, opts: opt}
	f.entries = make([]entry, 0, len(events)+len(items))
	for _, ev := range events {
		rank := hebcal.TimeOfDay(ev)
		if ev.GetFlags().Has(event.HEBREW_DATE) {
			rank = -1 // the date leads its day
		}
		f.entries = append(f.entries, entry{ev.GetDate().Abs(), rank, Event(ev, opt)})
	}
	for i := range items {
		if e, ok := LearningItem(&items[i], opt); ok {
			day, _ := time.Parse(time.DateOnly, items[i].Date)
			f.entries = append(f.entries, entry{hdate.FromTime(day).Abs(), hebcal.TimeOfDayLearning, e})
		}
	}
	// hebcal-go orders each day already; the stable sort keeps that and moves
	// the fetched learning rows into the learning slot.
	sort.SliceStable(f.entries, func(i, j int) bool {
		a, b := &f.entries[i], &f.entries[j]
		if a.abs != b.abs {
			return a.abs < b.abs
		}
		return a.rank < b.rank
	})
	if n := len(f.entries); n > 0 {
		f.span = span{f.entries[0].e.Date, f.entries[n-1].e.Date, n}
	}
	return f, nil
}

// options derives the per-event choices from the request.
func (s *Service) options(r *Request, events []event.CalEvent) *Options {
	p, q := r.Params, r.Query
	emoji := p.Emoji
	if q.Emoji != nil {
		emoji = *q.Emoji
	}
	o := &Options{
		Locale:       p.Locale,
		IL:           p.Opts.IL,
		Emoji:        emoji,
		AppendHebrew: p.AppendHebrew,
		UTMSource:    q.UTMSource,
		UTMMedium:    q.UTMMedium,
		Campaign:     q.UTMCampaign,
		UseElevation: p.Opts.UseElevation,
	}
	switch p.Hour12 {
	case 1:
		t := true
		o.Hour12 = &t
	case 2:
		f := false
		o.Hour12 = &f
	}
	if loc := p.Opts.Location; loc != nil {
		o.Location = loc
		o.Latitude, o.Longitude = loc.Latitude, loc.Longitude
		if lat, long, ok := q.Coordinates(); ok && r.Msg.GetGeoPos() {
			o.Latitude, o.Longitude = lat, long
		}
		o.GeoID = p.GeoID
		o.Place = p.Place
		o.ShortName = p.LocationShortName
	}
	if o.Campaign == "" {
		o.Campaign = "ical-" + campaignName(p, r.Query, events)
	}
	return o
}

// Result is a rendered feed.
type Result struct {
	Body []byte
	// Empty is true when the calendar had no events and Body holds one
	// placeholder instead. Such a response is worth a day, not two weeks.
	Empty bool
}

// Today is the Hebrew date a subscription's look-back is counted from.
func (s *Service) today() hdate.HDate { return hdate.FromTime(s.now().UTC()) }

// Week names the week a cut subscription belongs to, by the day of its Sunday.
// What a subscription keeps moves with the date, so what a client was sent last
// week is not what it should be sent this week.
func (s *Service) Week() int64 {
	t := s.today()
	return t.Abs() - int64(t.Weekday())
}

// Limit cuts a subscription over MaxSubscriptionEvents events down to size:
// first to what is recent or still to come, and if that is still too long to
// its first MaxSubscriptionEvents events followed by a notice. It reports
// whether anything was cut.
func (s *Service) Limit(f *Feed) bool {
	if f.req.Attachment() || len(f.entries) <= MaxSubscriptionEvents {
		return false
	}
	startAbs := s.today().Abs() - lookBackDays
	kept := f.entries[:0:0]
	for _, en := range f.entries {
		if en.abs >= startAbs {
			kept = append(kept, en)
		}
	}
	if len(kept) > MaxSubscriptionEvents {
		kept = kept[:MaxSubscriptionEvents]
		last := kept[len(kept)-1].abs + 1
		hd := hdate.FromRD(last)
		date := icalendar.DateOf(hd.Gregorian())
		const desc = "Hebcal calendar feed truncated"
		kept = append(kept, entry{last, 0, icalendar.Event{
			UID:      icalendar.DefaultUID(date, desc, ""),
			Summary:  desc,
			Date:     date,
			Category: "Personal",
			Private:  true,
			Description: "Your Hebcal calendar subscription exceeded the maximum number of events and has been truncated. " +
				"Visit https://www.hebcal.com/home/1398/number-of-years-in-calendar-feed-subscriptions to learn more about the feed length limit.",
		}})
	}
	f.entries = kept
	return true
}

// Marshal writes the feed, stamping every event with the current time. A
// subscription is cut to length with Limit first.
func (s *Service) Marshal(f *Feed, zones *icalendar.ZoneInfo, rawURL string) (*Result, error) {
	now := s.now()
	r := f.req
	p, q := r.Params, r.Query
	events := f.entries
	title, desc := calendarTitle(p, q, f.span, false), q.Caldesc
	if q.Title != "" {
		title = q.Title
	} else if p.Opts.Location == nil && q.IsSubscription() {
		title, desc = subscriptionTitle(p, q, desc)
	}
	color := defaultColor
	if q.Color != "" {
		color = q.Color
	}
	cal := &icalendar.Calendar{
		Title:        title,
		Description:  desc,
		Lang:         ProdidLang(p.Locale),
		PublishedTTL: q.PublishedTTL,
		RelCalID:     q.Relcalid,
		Color:        color,
		Zones:        zones,
		Stamp:        now,
	}
	if loc := p.Opts.Location; loc != nil {
		cal.TZID = loc.TimeZoneId
	}
	res := &Result{}
	var out []icalendar.Event
	if len(events) == 0 {
		out = []icalendar.Event{placeholder(now, rawURL)}
		cal.NoPublishedTTL = true
		res.Empty = true
	} else {
		out = make([]icalendar.Event, len(events))
		for i := range events {
			out[i] = events[i].e
		}
	}
	var err error
	res.Body, err = cal.Marshal(out)
	return res, err
}

// placeholder is the single event of a calendar that would otherwise be
// empty, which several applications refuse to subscribe to.
func placeholder(now time.Time, rawURL string) icalendar.Event {
	date := icalendar.DateOf(now.UTC())
	h1, h2 := murmur3.StringSum128(rawURL)
	return icalendar.Event{
		UID:         fmt.Sprintf("hebcal-%s-%016x%016x-dummy", date, h1, h2),
		Summary:     "Calendar contains no events",
		Date:        date,
		Category:    "Personal",
		Private:     true,
		Description: "To create a new Hebcal holiday calendar, visit https://www.hebcal.com/hebcal",
	}
}

// calendarTitle names the calendar: "Hebcal Palo Alto 2028", "Hebcal
// Diaspora 5787-5790" or "Hebcal Israel August 2026". A subscription has no
// dates in its name. preferAscii names the location by its plain-ASCII
// geonames name, which the tracking campaign wants.
func calendarTitle(p *download.Params, q *download.ICSQuery, sp span, preferAscii bool) string {
	title := "Hebcal"
	switch {
	case p.Opts.Location != nil:
		name := p.LocationShortName
		if preferAscii && p.CityNameAscii != "" {
			name = p.CityNameAscii
		}
		title += " " + name
	case p.Opts.IL:
		title += " Israel"
	default:
		title += " Diaspora"
	}
	if q.IsSubscription() {
		return title
	}
	o := &p.Opts
	year, numYears := o.Year, o.NumYears
	switch {
	case year != 0 && numYears > 1 && o.Month == 0:
		return title + " " + strconv.Itoa(year) + "-" + strconv.Itoa(year+numYears-1)
	case year != 0 && (o.IsHebrewYear || sp.n == 0):
		return title + " " + strconv.Itoa(year)
	case sp.n > 0:
		start, end := sp.first, sp.last
		switch {
		case start.Year != end.Year:
			return title + " " + strconv.Itoa(start.Year) + "-" + strconv.Itoa(end.Year)
		case start.Month == end.Month:
			return title + " " + start.Month.String() + " " + strconv.Itoa(start.Year)
		default:
			return title + " " + strconv.Itoa(start.Year)
		}
	}
	return title
}

// campaignName is what follows "ical-" in the tracking campaign: the title
// without its "Hebcal", slugified.
func campaignName(p *download.Params, q *download.ICSQuery, events []event.CalEvent) string {
	// The title's dates come from the events' first and last day.
	var sp span
	if len(events) > 0 {
		first, last := events[0], events[len(events)-1]
		sp = span{icalendar.DateOf(first.GetDate().Gregorian()), icalendar.DateOf(last.GetDate().Gregorian()), len(events)}
	}
	title := calendarTitle(p, q, sp, true)
	for i, c := range title {
		if c == ' ' {
			return jsutil.MakeAnchor(title[i+1:])
		}
	}
	return jsutil.MakeAnchor(title)
}

// subscriptionTitle names a subscription that has no location from what it
// subscribes to, and describes it when the calendar leaves out the major
// holidays: "Hebcal Israel - Torah Readings".
func subscriptionTitle(p *download.Params, q *download.ICSQuery, caldesc string) (title, desc string) {
	where := "Diaspora"
	if p.Opts.IL {
		where = "Israel"
	}
	noMajor := !q.Selected("maj")
	if noMajor {
		if name := q.OptionNames(); name != "" {
			if p.Opts.Sedrot || q.Selected("mod") {
				where += " - " + name
			} else {
				where = name
			}
		}
		if caldesc == "" {
			caldesc = q.OptionDescriptions()
		}
	}
	return "Hebcal " + where, caldesc
}

// ProdidLang is the language in the calendar's PRODID: the locale itself when
// it is two letters, otherwise the language of the locale's variant.
func ProdidLang(locale string) string {
	if len(locale) == 2 {
		return locale
	}
	switch locale {
	case "he-x-NoNikud", "he-x-nonikud", "h":
		return "he"
	case "ashkenazi_romanian":
		return "ro"
	}
	return "en"
}
