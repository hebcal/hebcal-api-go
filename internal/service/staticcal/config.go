package staticcal

import (
	"github.com/hebcal/hebcal-go/dailylearning"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/webconfig"
	// The dailylearning registry learningFeed consults is filled by this
	// package's init functions.
	_ "github.com/hebcal/learning"
)

// Feed describes one of the static calendars: what it holds, how far ahead it
// reaches, and how it names itself to a calendar application.
type Feed struct {
	// File is the name of the .ics and .csv files, without extension, and
	// the campaign its links are tagged with ("ical-" + File).
	File string
	// Title, Desc, Color and RelCalID are the calendar's X-WR-CALNAME,
	// X-WR-CALDESC, X-APPLE-CALENDAR-COLOR and X-WR-RELCALID. A RelCalID
	// must never change once published: it identifies the subscribed
	// calendar.
	Title    string
	Desc     string
	Color    string
	RelCalID string
	// Locale renders the events; IL selects the Israel schedule.
	Locale string
	IL     bool
	// Emoji puts each holiday's emoji in its title.
	Emoji bool
	// Years is how far ahead the feed reaches (see Window).
	Years int

	// Opts chooses the events hebcal-go generates, for a feed that is not a
	// readings-svc series. Start, End, IL and Locale are filled in.
	Opts hebcal.Options
	// NoMinorHolidays drops the minor holidays (Tu BiShvat, Lag BaOmer...)
	// from a holiday feed, keeping Purim and Chanukah.
	NoMinorHolidays bool
	// Series names a daily-learning series hebcal-go has no schedule for,
	// whose readings come from readings-svc instead.
	Series string
	// ChofetzChaim makes the one feed whose every day pairs a Chofetz Chaim
	// reading with a Shemirat HaLashon one.
	ChofetzChaim bool
}

// Feeds lists every static calendar, in the order they are generated: one
// per staticCalendars.json entry, then one per daily-learning series that has
// a downloadSlug in dailyLearningConfig.json. Both files are hebcal-web's,
// embedded by internal/webconfig.
var Feeds = buildFeeds(webconfig.StaticCalendars(), webconfig.Learning())

func buildFeeds(static []webconfig.StaticCalendar, learning []webconfig.LearningSeries) []Feed {
	feeds := make([]Feed, 0, len(static)+len(learning))
	for i := range static {
		feeds = append(feeds, staticFeed(&static[i]))
	}
	for i := range learning {
		if f, ok := learningFeed(&learning[i]); ok {
			feeds = append(feeds, f)
		}
	}
	return feeds
}

// staticFeed is the feed of one staticCalendars.json entry, whose options
// are @hebcal/core calendar() options.
func staticFeed(c *webconfig.StaticCalendar) Feed {
	return Feed{
		File: c.DownloadSlug, Title: c.Title, Desc: c.CalDesc,
		Color: c.CalendarColor, RelCalID: c.RelCalID,
		Locale: c.Locale, IL: c.IL, Emoji: c.Emoji, Years: c.Years,
		NoMinorHolidays: c.NoMinorHolidays,
		Opts: hebcal.Options{
			Mask:             event.Flags(c.Mask),
			NoHolidays:       c.NoHolidays,
			NoRoshChodesh:    c.NoRoshChodesh,
			NoModern:         c.NoModern,
			NoMinorFast:      c.NoMinorFast,
			NoSpecialShabbat: c.NoSpecialShabbat,
			AddHebrewDates:   c.AddHebrewDates,
			Omer:             c.Omer,
			Sedrot:           c.Sedrot,
			YomKippurKatan:   c.YomKippurKatan,
			ShabbatMevarchim: c.ShabbatMevarchim,
			Yizkor:           c.Yizkor,
		},
	}
}

// chofetzChaimFile is the one learning feed that pairs two series: each day's
// Chofetz Chaim reading with the Shemirat HaLashon one.
const chofetzChaimFile = "chofetz-chaim"

// learningFeed is the feed of one daily-learning series, and false for an
// entry with no feed of its own (Shemirat HaLashon, which rides in the
// Chofetz Chaim feed) or that is not a daily-learning series (the Torah
// readings, which staticCalendars.json covers). A series hebcal-go generates
// is named as its dailylearning registry has it; any other comes from
// readings-svc.
func learningFeed(c *webconfig.LearningSeries) (Feed, bool) {
	if c.DownloadSlug == "" || c.DailyLearningOptName == "" {
		return Feed{}, false
	}
	f := Feed{
		File: c.DownloadSlug, Title: c.ShortName, Desc: c.FeedDesc(),
		Color: c.Color, RelCalID: c.RelCalID,
		Locale: "en", Years: c.Years,
	}
	switch {
	case c.DownloadSlug == chofetzChaimFile:
		f.ChofetzChaim = true
	case dailylearning.Has(c.DailyLearningOptName):
		f.Opts = hebcal.Options{NoHolidays: true, DailyLearning: []string{c.DailyLearningOptName}}
	default:
		f.Series = c.EventCategory
	}
	return f, true
}
