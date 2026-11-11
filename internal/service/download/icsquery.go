package download

import (
	"strconv"
	"strings"
)

// ICSQuery is what an iCalendar feed's query string says beyond the calendar
// itself, which the Download message has no room for: the title, colour and
// tracking a subscription link was given. A /v4/ URL carries them as ordinary
// query parameters beside its payload; the legacy forms carry them inside the
// query string that is the whole request.
type ICSQuery struct {
	// Title and Caldesc name and describe the calendar, overriding the
	// generated ones.
	Title, Caldesc string
	// PublishedTTL is how often a subscriber should refresh, as an RFC 5545
	// duration.
	PublishedTTL string
	// Relcalid is X-WR-RELCALID.
	Relcalid string
	// Color is X-APPLE-CALENDAR-COLOR, upper-cased.
	Color string
	// Subscribe is the subscribe parameter as given. Any value makes the
	// response an inline feed rather than a file to save; only "1" names the
	// calendar from its contents.
	Subscribe string
	// Emoji is the emoji parameter when it said yes or no, nil when it said
	// neither and the request's own choice stands.
	Emoji *bool
	// UTMSource, UTMMedium and UTMCampaign override the tracking every link
	// in the feed carries.
	UTMSource, UTMMedium, UTMCampaign string

	q v2Query
}

func newICSQuery(q v2Query) *ICSQuery {
	c := &ICSQuery{
		Title:        q["title"],
		Caldesc:      q["caldesc"],
		PublishedTTL: q["publishedTTL"],
		Relcalid:     q["relcalid"],
		Color:        strings.ToUpper(q["color"]),
		Subscribe:    q["subscribe"],
		UTMSource:    q["utm_source"],
		UTMMedium:    q["utm_medium"],
		UTMCampaign:  q["utm_campaign"],
		q:            q,
	}
	switch q["emoji"] {
	case "1", "on":
		t := true
		c.Emoji = &t
	case "0", "off":
		f := false
		c.Emoji = &f
	}
	return c
}

// Attachment reports whether the feed is a file to save: anything that is not
// a subscription.
func (c *ICSQuery) Attachment() bool { return c.Subscribe == "" }

// IsSubscription is a subscribe=1 request, the only value that lets the feed
// name itself.
func (c *ICSQuery) IsSubscription() bool { return c.Subscribe == "1" }

// optionNames pairs each calendar-selection parameter with its short and long
// description, in the order the calendar form lists them.
var optionNames = []struct{ param, name, long string }{
	{"maj", "Major Holidays", "Major Holidays"},
	{"min", "Minor Holidays", "Minor Holidays"},
	{"nx", "Rosh Chodesh", "Rosh Chodesh"},
	{"mod", "Modern Holidays", "Modern Holidays"},
	{"mf", "Minor Fasts", "Minor Fasts"},
	{"ss", "Special Shabbatot", "Special Shabbatot"},
	{"o", "Days of the Omer", "7 weeks from the second night of Pesach to the day before Shavuot"},
	{"ykk", "Yom Kippur Katan", "Minor day of atonement occurring monthly on the day preceding each Rosh Chodesh"},
	{"s", "Torah Readings", "Parashat ha-Shavua - Weekly Torah Portion"},
	{"d", "Hebrew Dates", "Daily Hebrew Dates"},
	{"D", "Hebrew Dates", "Hebrew Dates"},
	{"yzkr", "Yizkor", "Ashkenazi Jewish memorial prayer service for the dead recited in synagogue during four holidays yearly"},
	{"mvch", "Shabbat Mevarchim", "Shabbat before the start of Rosh Chodesh"},
	{"F", "Daf Yomi", "Daily regimen of learning the Babylonian Talmud"},
	{"myomi", "Mishna Yomi", "Two Mishnayot each day"},
	{"dpy", "Perek Yomi", "One chapter (perek) of Mishnah each day"},
	{"nyomi", "Nach Yomi", "Nevi’im (Prophets) and Ketuvim (Writings)"},
	{"dty", "Tanakh Yomi", "Prophets and Writings on weekdays according to the ancient Masoretic division of sedarim"},
	{"dps", "Daily Tehillim", "Daily study of few chapters from the book of Psalms"},
	{"d929", "929", "Chapter-a-day Tanakh reading, five days a week"},
	{"dr1", "Daily Rambam", "Maimonides’ Mishneh Torah legal code"},
	{"dr3", "Daily Rambam (3 Chapters)", "Maimonides’ Mishneh Torah legal code"},
	{"dsm", "Sefer HaMitzvot", "Maimonides’ list of the 613 commandments"},
	{"yyomi", "Yerushalmi Yomi (Vilna)", "Jerusalem Talmud (Vilna edition)"},
	{"yys", "Yerushalmi Yomi (Schottenstein)", "Jerusalem Talmud (Schottenstein edition)"},
	{"dcc", "Sefer Chofetz Chaim", "Jewish ethics and laws of speech"},
	// The website has no long description for this one, and a missing entry
	// still takes its place in the list.
	{"dshl", "Shemirat HaLashon", ""},
	{"ayd", "Amud HaYomi (Dirshu)", "One amud (page side) of Babylonian Talmud per day"},
	{"dw", "Daf-a-Week", "One page of Babylonian Talmud per week"},
	{"dpa", "Pirkei Avot", "Ethics of our Fathers, studied on Shabbat between Pesach and Rosh Hashana"},
	{"ahsy", "Arukh HaShulchan Yomi", "Summary of the sources for each chapter of the Shulchan Arukh and its commentaries"},
	{"dksa", "Kitzur Shulchan Arukh Yomi", "Simplified summary of the Shulchan Arukh, focusing on Orach Chayim and Yoreh De'ah"},
	{"ddh", "Daf HaYomi B’Halacha (Dirshu)", "Dirshu’s daily Mishnah Berurah program"},
	{"dtn", "Tanya Yomi", "Daily study of the Tanya by Rabbi Schneur Zalman of Liadi"},
}

// Selected reports whether the request switched a calendar option on, by the
// name of its parameter: "maj", "s". An absent option, "off" and "0" are not.
func (c *ICSQuery) Selected(param string) bool { return c.q[param] == "on" || c.q[param] == "1" }

// OptionNames lists the calendars the request selected by name, in the form's
// order: "Major Holidays, Torah Readings". A calendar is selected unless its
// parameter is absent, "off" or "0".
func (c *ICSQuery) OptionNames() string {
	return c.describe(func(i int) string { return optionNames[i].name })
}

// OptionDescriptions is OptionNames with each calendar's longer description.
func (c *ICSQuery) OptionDescriptions() string {
	return c.describe(func(i int) string { return optionNames[i].long })
}

func (c *ICSQuery) describe(text func(int) string) string {
	var strs []string
	for i, o := range optionNames {
		if !c.q.off(o.param) {
			strs = append(strs, text(i))
		}
	}
	return strings.Join(strs, ", ")
}

// Coordinates is the latitude and longitude the request spelled out, exactly as
// written, for a calendar that named its location by them. A /v4/ URL stores
// them in single precision and so spells them as that.
func (c *ICSQuery) Coordinates() (lat, long float64, ok bool) {
	lat, err1 := strconv.ParseFloat(c.q["latitude"], 64)
	long, err2 := strconv.ParseFloat(c.q["longitude"], 64)
	return lat, long, err1 == nil && err2 == nil
}
