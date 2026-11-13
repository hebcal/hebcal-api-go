// Package ical decides what a holiday-calendar event says in an iCalendar
// feed -- its title, emoji, category, description, alarm and UID -- and
// resolves it into the icalendar.Event that pkg/icalendar writes.
//
// It covers the all-day events -- holidays, Rosh Chodesh, weekly Torah
// portions, Hebrew dates, the Omer, Shabbat Mevarchim, Yom Kippur Katan,
// Yizkor and daily learning, including the rows readings-svc supplies for the
// series hebcal-go has no schedule for -- and, for a calendar with a
// location, the timed events: candle lighting, Havdalah, fast times, Chanukah
// candles and Kiddush Levana.
package ical

import (
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/omer"
	"github.com/hebcal/hebcal-go/zmanim"

	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
	"github.com/hebcal/hebcal-api-go/internal/service/shabbat"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// utmMedium tags the external links a feed carries.
const utmMedium = "icalendar"

// Options are the calendar-wide choices that shape each event.
type Options struct {
	// Locale renders titles and picks the language of descriptions.
	Locale string
	// IL is the Israel schedule; it also asks every hebcal.com link for it.
	IL bool
	// Emoji puts the holiday's emoji before its title (after it for the
	// Omer).
	Emoji bool
	// AppendHebrew adds " / " and the Hebrew title after the title.
	AppendHebrew bool
	// Campaign tags every link: uc= on hebcal.com links, utm_campaign= on
	// external ones.
	Campaign string
	// UTMSource and UTMMedium override the source and medium of a link's
	// tracking; the defaults are "ical" and "icalendar".
	UTMSource, UTMMedium string
	// Location is the calendar's location, nil when it has none. Its zone,
	// position and name go on every timed event.
	Location *zmanim.Location
	// Latitude and Longitude are the position GEO reports. They are the
	// location's own unless the request gave coordinates, which are reported
	// as it spelled them rather than rounded to the single precision a /v4/
	// URL stores them in.
	Latitude, Longitude float64
	// GeoID is the location's geonameid or ZIP code, empty for a latitude and
	// longitude.
	GeoID string
	// Place is what tells the location apart in a timed event's UID: GeoID,
	// or failing that an anchor made of its name.
	Place string
	// ShortName is the location's display name, the LOCATION of a timed event.
	ShortName string
	// Clock forces 12-hour or 24-hour clock times in descriptions;
	// event.ClockAuto follows the location's country.
	Clock event.ClockFormat
	// UseElevation makes the Omer's evening alarm account for the location's
	// elevation.
	UseElevation bool
	// CandleLighting is the calendar's candle-lighting option. With it, an
	// Omer day whose evening has no time of its own is reminded at tzeit.
	CandleLighting bool
	// TorahMemos holds the Torah and Haftarah readings to append to the
	// description of each event that has one.
	TorahMemos TorahMemos
}

// TorahMemos holds readings-svc's Torah memos by date and untranslated
// description.
type TorahMemos map[torahMemoKey]string

type torahMemoKey struct{ date, desc string }

// NewTorahMemos indexes the rows readings-svc returned.
func NewTorahMemos(rows []readings.TorahMemo) TorahMemos {
	m := make(TorahMemos, len(rows))
	for _, r := range rows {
		m[torahMemoKey{r.Date, r.Desc}] = r.Memo
	}
	return m
}

func (m TorahMemos) lookup(hd hdate.HDate, desc string) string {
	if m == nil {
		return ""
	}
	return m[torahMemoKey{hd.Gregorian().Format(time.DateOnly), desc}]
}

// categories maps an event's primary category to its CATEGORIES value. A
// category mapped to "" has none; so does one not listed.
var categories = map[string]string{
	"candles":     "Holiday",
	"dafyomi":     "Daf Yomi",
	"mishnayomi":  "Mishna Yomi",
	"nachyomi":    "Nach Yomi",
	"yerushalmi":  "Yerushalmi Yomi",
	"havdalah":    "Holiday",
	"holiday":     "Holiday",
	"parashat":    "Parsha",
	"roshchodesh": "Holiday",
	"user":        "Personal",
}

// seriesNames names each daily-learning series by its event category, both
// those hebcal-go generates and those readings-svc supplies. The name is the
// event's CATEGORIES value and, translated, its LOCATION.
var seriesNames = map[string]string{
	"929":                 "929",
	"arukhHaShulchanYomi": "Arukh HaShulchan Yomi",
	"chofetzChaim":        "Chofetz Chaim",
	"dafWeekly":           "Daf Weekly",
	"dafyomi":             "Daf Yomi",
	"dailyPsalms":         "Psalms",
	"dirshuAmudYomi":      "Dirshu Amud HaYomi",
	"dirshuDafHalacha":    "Daf HaYomi B'Halacha",
	"kitzurShulchanAruch": "Kitzur Shulchan Arukh",
	"mishnayomi":          "Mishna Yomi",
	"nachyomi":            "Nach Yomi",
	"perekYomi":           "Perek Yomi",
	"pirkeiAvotSummer":    "Pirkei Avot",
	"rambam1":             "Daily Rambam",
	"rambam3":             "Daily Rambam",
	"seferHaMitzvot":      "Sefer Hamitzvot",
	"shemiratHaLashon":    "Shemirat HaLashon",
	"tanakhYomi":          "Tanakh Yomi",
	"tanyaYomi":           "Tanya Yomi",
	"yerushalmi":          "Yerushalmi Yomi",
}

// Event resolves one hebcal-go event: an all-day one, or -- when the event
// carries a time -- one that starts at that time in the calendar's location.
func Event(ev event.CalEvent, o *Options) icalendar.Event {
	flags := ev.Flags()
	hd := ev.Date()
	timed, isTimed := ev.(event.Timed)
	isTimed = isTimed && o.Location != nil
	subj := download.RenderSubject(ev, o.Locale, download.ShouldRenderBrief(ev, flags, isTimed))
	if o.Emoji {
		emoji := ev.Emoji()
		if emoji != "" {
			if flags.Has(event.OmerCount) {
				subj += " " + emoji
			} else {
				subj = emoji + " " + subj
			}
		}
	}
	e := icalendar.Event{
		Summary:     subj,
		Date:        icalendar.DateOf(hd.Gregorian()),
		Busy:        flags.Has(event.Chag),
		Private:     flags.Has(event.User),
		Description: Memo(ev, o),
	}
	if o.AppendHebrew {
		if he := download.RenderSubject(ev, "he", true); he != "" {
			e.SummarySuffix = " / " + he
		}
	}
	place := ""
	if isTimed {
		place = o.Place
		e.Timed = true
		e.Hour, e.Minute, _ = timed.Time().Clock()
		e.TZID = o.Location.TimeZoneID
		e.Geo = &icalendar.Geo{Latitude: o.Latitude, Longitude: o.Longitude}
		e.Location = o.ShortName
		if kind, _ := hebcal.TimedKindOf(ev); kind == hebcal.CandleLighting {
			e.Alarm = icalendar.AlarmCandleLighting
		}
	}
	e.UID = icalendar.DefaultUID(e.Date, uidDesc(ev), place)
	cat := download.EventCategories(ev)[0]
	if name, ok := seriesNames[cat]; ok && flags.HasAny(download.LearningFlags) {
		e.Category = name
		e.Location = model.Gettext(name, o.Locale)
	} else {
		e.Category = categories[cat]
	}
	switch {
	case flags.Has(event.OmerCount):
		e.Alarm = icalendar.AlarmOmer
		if oe, ok := ev.(omer.OmerEvent); ok && o.Location != nil {
			o.omerAlarm(&e, oe)
		}
	case flags.Has(event.User):
		e.Alarm = icalendar.AlarmUserEvent
	}
	return e
}

// omerAlarm moves an Omer day's reminder to the evening it begins, for a
// calendar with a location: half an hour before sunset on a Friday, nightfall
// on a Saturday and dusk on any other day. The evening's own date and the
// location's geonameid or ZIP name the event, so a subscriber's reminder is
// kept apart from the same day's in another city.
//
// Where the evening has no such time (a summer night when the sun stays
// above 8.5° or the civil-dusk angle), a calendar with candle-lighting falls
// back to tzeit at 7.083°, ignoring elevation, and the event keeps its
// ordinary UID and no LOCATION; failing that too (a polar summer), the generic
// reminder stays.
func (o *Options) omerAlarm(e *icalendar.Event, oe omer.OmerEvent) {
	eve := oe.Date().Prev()
	y, m, d := eve.Greg()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	// The calendar's events were generated for this location, so it is
	// known to be usable.
	z := zmanim.MustNew(o.Location, day)
	z.UseElevation = o.UseElevation
	var at time.Time
	switch eve.Weekday() {
	case time.Friday:
		at = z.SunsetOffset(-30, true)
	case time.Saturday:
		at = z.Tzeit(zmanim.Tzeit3SmallStars)
	default:
		at = z.Dusk()
	}
	if at.IsZero() {
		if o.CandleLighting {
			z := zmanim.MustNew(o.Location, day)
			if tzeit := z.Tzeit(zmanim.Tzeit3MediumStars); !tzeit.IsZero() {
				e.Alarm = icalendar.AbsoluteAlarm(tzeit)
			}
		}
		return
	}
	e.Alarm = icalendar.AbsoluteAlarm(at)
	e.Location = o.ShortName
	if o.GeoID != "" {
		// The Hebrew year of the evening, whatever the name suggests.
		e.UID = "hebcal-omer-" + strconv.Itoa(eve.Year()) + "-day" + strconv.Itoa(oe.OmerDay) + "-" + o.GeoID
	}
}

// Memo is an event's description. In order of precedence: the Omer's count
// and sefira; the event's own memo (the molad announced on Shabbat Mevarchim,
// the month a Yom Kippur Katan precedes, a learning reading's detail); a
// weekly portion's prose summary; the holiday's description; the event a
// Yizkor belongs to. Then, each after a blank line, the Torah reading and the
// tracked link.
func Memo(ev event.CalEvent, o *Options) string {
	flags := ev.Flags()
	if kind, _ := hebcal.TimedKindOf(ev); kind == hebcal.Havdalah || kind == hebcal.CandleLighting {
		// Candle lighting names the coming portion, and that is all it says.
		return ownMemo(ev, flags, o)
	}
	if flags.Has(event.KiddushLevana) {
		return kiddushLevanaMemo(ev, o)
	}
	if oe, ok := ev.(omer.OmerEvent); ok {
		return oe.TodayIs("en") + "\n\n" + oe.TodayIs("he") + "\n\n" +
			oe.Sefira("en") + "\n" + oe.Sefira("he") + "\n" + oe.Sefira("translit")
	}
	memo := ownMemo(ev, flags, o)
	if memo == "" {
		memo = download.ParshaSummary(ev, o.Locale)
	}
	if memo == "" {
		memo = download.HolidayDescription(ev, flags, o.Locale, false)
		if memo == "" {
			// A holiday without a Hebrew description has its English one.
			memo = download.HolidayDescription(ev, flags, "en", false)
		}
	}
	if memo == "" {
		if linked := download.LinkedEvent(ev); linked != nil && uidDesc(linked) != uidDesc(ev) {
			memo = download.RenderSubject(linked, o.Locale, false)
		}
	}
	if torah := torahMemo(ev, flags, o); torah != "" {
		memo = appendParagraph(memo, torah)
	}
	if flags.Has(event.HebrewDate) {
		// The date converter page is not linked from a feed.
		return memo
	}
	hd := ev.Date()
	return appendParagraph(memo, o.link(ev.URL(), hd.Year()))
}

// link tags an event's page the way every feed link is tagged. A page of
// hebcal.com is credited to "ical" unless the subscription says otherwise.
func (o *Options) link(rawURL string, hyear int) string {
	source, medium := o.UTMSource, o.UTMMedium
	if source == "" && strings.HasPrefix(rawURL, "https://www.hebcal.com/") {
		source = "ical"
	}
	if medium == "" {
		medium = utmMedium
	}
	return download.EventLinkFrom(rawURL, hyear, o.Campaign, source, medium, o.IL)
}

// ownMemo is the memo an event carries by itself.
func ownMemo(ev event.CalEvent, flags event.Flags, o *Options) string {
	switch {
	case flags.Has(event.ShabbatMevarchim):
		cc := ""
		if o.Location != nil {
			cc = o.Location.CountryCode
		}
		return shabbat.MevarchimMoladMemo(ev.Date(), strings.ToLower(o.Locale), cc, o.IL, o.Clock)
	}
	return model.FixMonthSpelling(event.Memo(ev, o.Locale))
}

// noReading are the events that never have a Torah reading of their own.
const noReading = event.OmerCount | event.ShabbatMevarchim | event.Molad |
	event.User | event.HebrewDate | download.LearningFlags

func torahMemo(ev event.CalEvent, flags event.Flags, o *Options) string {
	if !hasReading(ev) {
		return ""
	}
	return o.TorahMemos.lookup(ev.Date(), uidDesc(ev))
}

// hasReading reports whether an event can have a Torah reading to append:
// the weekly portion and the holidays, but not the other events, nor anything
// with a time.
func hasReading(ev event.CalEvent) bool {
	if ev.Flags().HasAny(noReading) {
		return false
	}
	_, timed := ev.(event.Timed)
	return !timed
}

// uidDesc is the untranslated description an event's UID is a hash of. It
// must never change: a different UID duplicates the event for every
// subscriber.
func uidDesc(ev event.CalEvent) string {
	if ev.Flags().HasAny(download.LearningFlags) {
		// A reading without its series name, and for Tanakh Yomi without its
		// verses: "Bava Batra 21", "Daniel Seder 1".
		desc := ev.Basename()
		if download.EventCategories(ev)[0] == "tanakhYomi" {
			desc, _, _ = strings.Cut(desc, " (")
		}
		return desc
	}
	if ev.Flags().Has(event.HebrewDate) {
		return model.HDateString(ev.Date()) // "30 Av 5786"
	}
	// "Pesach II", "Omer 7", "Yizkor", "Parashat Re'eh" -- never the
	// rendered title, whose apostrophes are typographic.
	return model.FixMonthSpelling(ev.Desc())
}

func appendParagraph(memo, s string) string {
	switch {
	case s == "":
		return memo
	case memo == "":
		return s
	}
	return memo + "\n\n" + s
}

// LearningItem resolves a daily-learning reading readings-svc supplied, for a
// series hebcal-go has no schedule for. Its title is already the brief form
// in the requested locale.
func LearningItem(it *readings.Item, o *Options) (icalendar.Event, bool) {
	day, err := time.Parse(time.DateOnly, it.Date)
	if err != nil {
		return icalendar.Event{}, false
	}
	date := icalendar.DateOf(day)
	subj := it.Title
	if subj == "" {
		subj = it.Hebrew
	}
	if o.AppendHebrew && it.Hebrew != "" {
		subj += " / " + it.Hebrew
	}
	e := icalendar.Event{
		UID:         icalendar.DefaultUID(date, it.Desc(), ""),
		Summary:     subj,
		Date:        date,
		Description: appendParagraph(it.Memo, LearningLink(it.Link, o.Campaign)),
	}
	if name, ok := seriesNames[it.Category]; ok {
		e.Category = name
		e.Location = model.Gettext(name, o.Locale)
	}
	return e, true
}

// LearningLink re-tags a link readings-svc supplied with a feed's campaign in
// place of the sidecar's own tracking parameters.
func LearningLink(link, campaign string) string {
	return download.EventLink(download.CanonicalLearningURL(link), 0, campaign, utmMedium, false)
}
