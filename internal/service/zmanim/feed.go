package zmanim

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	zman "github.com/hebcal/hebcal-go/zmanim"
	"github.com/hebcal/locales"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// FeedKind selects which of the three zmanim iCalendar feeds a path names.
type FeedKind int

const (
	// FeedZmanim is /zmanim: every zman as its own timed event, for 60 days.
	FeedZmanim FeedKind = iota
	// FeedZmanimAllDay is /zmanim2: one all-day event per date, titled with
	// the Hebrew date, whose description lists that day's times; 90 days.
	FeedZmanimAllDay
	// FeedSunriseSunset is /sunrs: sunrise and sunset only, for a year.
	FeedSunriseSunset
)

// FeedKindFor returns the feed a request path names. Anything under /zmanim2
// is the all-day feed and anything under /sunrs sunrise and sunset; the rest
// is the timed zmanim feed.
func FeedKindFor(path string) FeedKind {
	switch {
	case strings.HasPrefix(path, "/zmanim2"):
		return FeedZmanimAllDay
	case strings.HasPrefix(path, "/sunrs"):
		return FeedSunriseSunset
	}
	return FeedZmanim
}

// days is how far past today the feed runs. Every feed starts yesterday, so
// a client refreshing just after midnight still sees the evening's times.
func (k FeedKind) days() int {
	switch k {
	case FeedZmanimAllDay:
		return 90
	case FeedSunriseSunset:
		return 365
	}
	return 60
}

// Feed is one zmanim calendar: a location, a locale and the date it is
// generated on.
type Feed struct {
	Kind FeedKind
	Loc  *geodb.Location
	// Today is the date in the location's own time zone.
	Today model.GregDate
	// locale is the catalog the titles are translated with; lang the PRODID
	// language.
	locale string
	lang   string
}

// NewFeed prepares a feed for a location. now is read in the location's time
// zone to find today, so the calendar rolls over at the location's midnight.
func NewFeed(kind FeedKind, q url.Values, loc *geodb.Location, now time.Time) *Feed {
	if tz, err := zman.LoadLocation(loc.TimeZoneID); err == nil {
		now = now.In(tz)
	}
	y, m, d := now.Date()
	lg := q.Get("lg")
	return &Feed{
		Kind:   kind,
		Loc:    loc,
		Today:  model.GregDate{Year: y, Month: m, Day: d},
		locale: model.AliasLocale(lg),
		lang:   prodidLang(lg),
	}
}

// prodidLang is the two-letter language the calendar's PRODID names: the
// locale itself when it is two letters, the base language of the few longer
// locale names, and English otherwise -- so "a" (Ashkenazi) and the default
// Sephardic transliteration are EN, and any unknown two-letter lg is named
// as given.
func prodidLang(lg string) string {
	switch lg {
	case "h":
		return "he"
	case "", "a", "ah", "s", "sh":
		return "en"
	}
	if len(lg) == 2 {
		return lg
	}
	switch lg {
	case "he-x-NoNikud", "he-x-nonikud":
		return "he"
	case "ashkenazi_romanian":
		return "ro"
	}
	return "en"
}

// zmanTitle is a zman's English event title, untranslated: the key its
// translation is looked up by and the string its UID hashes.
func zmanTitle(name string) string {
	s, _ := locales.LookupTranslation("TITLE:"+name, "en")
	return s
}

// zmanMemo is a zman's English explanation, an event's DESCRIPTION; empty
// when the catalog has none.
func zmanMemo(name string) string {
	if s, ok := locales.LookupTranslation("MEMO:"+name, "en"); ok {
		return s
	}
	return ""
}

// Variant is what the feed depends on beyond its URL, for the ETag: the date
// it was generated on.
func (f *Feed) Variant() string { return f.Today.String() }

// dayTimes is one date of the feed and the zmanim that occur on it.
type dayTimes struct {
	date  model.GregDate
	times []namedTime
}

// compute returns the zmanim from yesterday to Days() days from today, each
// rounded to the minute. A time that does not occur is dropped.
func (f *Feed) compute() []dayTimes {
	names := allTimesOrder
	if f.Kind == FeedSunriseSunset {
		names = []string{"sunrise", "sunset"}
	}
	start := f.Today.RD() - 1
	end := f.Today.RD() + int64(f.Kind.days())
	out := make([]dayTimes, 0, end-start+1)
	for rd := start; rd <= end; rd++ {
		d := model.GregFromRD(rd)
		all := computeTimes(d, f.Loc, names, true, false, true)
		times := all[:0]
		for _, nt := range all {
			if !nt.t.IsZero() {
				times = append(times, nt)
			}
		}
		out = append(out, dayTimes{d, times})
	}
	return out
}

// hasSolarTimes reports whether any day has a time other than the molad.
func hasSolarTimes(days []dayTimes) bool {
	for _, day := range days {
		for _, nt := range day.times {
			if nt.name != "molad" {
				return true
			}
		}
	}
	return false
}

// place is what distinguishes the location in a UID: its geonameid, its ZIP
// code, or failing both (a latitude/longitude) an anchor made of its name.
func (f *Feed) place() string {
	switch {
	case f.Loc.GeonameID != 0:
		return strconv.Itoa(f.Loc.GeonameID)
	case f.Loc.Zip != "":
		return f.Loc.Zip
	}
	return jsutil.MakeAnchor(f.Loc.Name)
}

// ICS renders the feed. stamp is every event's DTSTAMP.
func (f *Feed) ICS(zones *icalendar.ZoneInfo, stamp time.Time) ([]byte, error) {
	days := f.compute()
	if !hasSolarTimes(days) {
		// The molad alone does not make a zmanim calendar, e.g. in a polar
		// winter or summer.
		return nil, model.BadRequest("No zmanim available for %s", f.Loc.Name)
	}
	var events []icalendar.Event
	if f.Kind == FeedZmanimAllDay {
		events = f.allDayEvents(days)
	} else {
		events = f.timedEvents(days)
	}
	title, desc := "Hebcal Zmanim ", "Halachic times for "
	if f.Kind == FeedSunriseSunset {
		title, desc = "Sunrise and Sunset ", "Times for "
	}
	cal := icalendar.Calendar{
		Title:        title + f.Loc.ShortName(),
		Description:  desc + f.Loc.Name,
		Lang:         f.lang,
		PublishedTTL: "P7D",
		TZID:         f.Loc.TimeZoneID,
		Zones:        zones,
		Stamp:        stamp,
	}
	return cal.Marshal(events)
}

// timedEvents makes each zman its own event, in time order. Its date is the
// civil date the zman was computed for, which for Chatzot HaLailah after
// midnight is the day before the one it falls on.
func (f *Feed) timedEvents(days []dayTimes) []icalendar.Event {
	type timed struct {
		date model.GregDate
		nt   namedTime
	}
	var all []timed
	// Name-major, as the times are tabulated, so that the stable sort
	// leaves two zmanim at the same minute in table order.
	names := allTimesOrder
	if f.Kind == FeedSunriseSunset {
		names = []string{"sunrise", "sunset"}
	} else {
		names = append(append([]string{}, names...), moladTimeNames()...)
	}
	for _, name := range names {
		for _, day := range days {
			for _, nt := range day.times {
				if nt.name == name {
					all = append(all, timed{day.date, nt})
				}
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].nt.t.Before(all[j].nt.t) })

	tz, _ := zman.LoadLocation(f.Loc.TimeZoneID)
	place := f.place()
	shortName := f.Loc.ShortName()
	geo := &icalendar.Geo{Latitude: f.Loc.Latitude, Longitude: f.Loc.Longitude}
	events := make([]icalendar.Event, len(all))
	for i, tt := range all {
		title := zmanTitle(tt.nt.name)
		t := tt.nt.t
		if tz != nil {
			t = t.In(tz)
		}
		date := icalendar.Date{Year: tt.date.Year, Month: tt.date.Month, Day: tt.date.Day}
		events[i] = icalendar.Event{
			UID:         icalendar.DefaultUID(date, title, place),
			Summary:     model.Gettext(title, f.locale),
			Date:        date,
			Timed:       true,
			Hour:        t.Hour(),
			Minute:      t.Minute(),
			TZID:        f.Loc.TimeZoneID,
			Category:    tt.nt.name,
			Private:     true,
			Description: zmanMemo(tt.nt.name),
			Location:    shortName,
			Geo:         geo,
		}
	}
	return events
}

func moladTimeNames() []string {
	names := make([]string, len(moladTimes))
	for i, mt := range moladTimes {
		names[i] = mt.name
	}
	return names
}

// allDayEvents makes one event per date, titled with the Hebrew date, listing
// that day's times in order -- in 12-hour form where the location's country
// uses it, right-aligned on the colon -- followed by the location's name.
func (f *Feed) allDayEvents(days []dayTimes) []icalendar.Event {
	tz, _ := zman.LoadLocation(f.Loc.TimeZoneID)
	place := f.place()
	shortName := f.Loc.ShortName()
	events := make([]icalendar.Event, len(days))
	for i, day := range days {
		times := append([]namedTime{}, day.times...)
		sort.SliceStable(times, func(a, b int) bool { return times[a].t.Before(times[b].t) })
		lines := make([]string, len(times))
		for j, nt := range times {
			t := nt.t
			if tz != nil {
				t = t.In(tz)
			}
			str := event.ReformatTimeStr(t.Format("15:04"), "pm", f.Loc.CC, nil)
			prefix := ""
			if h, _, _ := strings.Cut(str, ":"); len(h) != 2 {
				prefix = " "
			}
			lines[j] = prefix + str + " " + model.Gettext(zmanTitle(nt.name), f.locale)
		}
		date := icalendar.Date{Year: day.date.Year, Month: day.date.Month, Day: day.date.Day}
		hd := hdate.FromGregorian(day.date.Year, day.date.Month, day.date.Day)
		ev := event.NewHebrewDateEvent(hd)
		var summary string
		if hd.Day() == 1 {
			summary = ev.Render(f.locale)
		} else {
			summary = event.RenderBrief(ev, f.locale)
		}
		events[i] = icalendar.Event{
			UID:         "hebcal-zmanim2-" + date.String() + "-" + place,
			Summary:     summary,
			Date:        date,
			Description: strings.Join(lines, "\n") + "\n\n" + f.Loc.Name,
			Location:    shortName,
		}
	}
	return events
}
