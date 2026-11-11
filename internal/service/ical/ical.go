// Package ical decides what a holiday-calendar event says in an iCalendar
// feed -- its title, emoji, category, description, alarm and UID -- and
// resolves it into the icalendar.Event that pkg/icalendar writes.
//
// It covers the all-day events a feed with no location carries: holidays,
// Rosh Chodesh, weekly Torah portions, Hebrew dates, the Omer, Shabbat
// Mevarchim, Yom Kippur Katan, Yizkor and daily learning, including the rows
// readings-svc supplies for the series hebcal-go has no schedule for. Timed
// events (candle lighting, fast times, Kiddush Levana) need a location and
// are not handled yet.
package ical

import (
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/omer"

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

// Event resolves one all-day hebcal-go event.
func Event(ev event.CalEvent, o *Options) icalendar.Event {
	flags := ev.GetFlags()
	hd := ev.GetDate()
	subj := download.RenderSubject(ev, o.Locale, download.ShouldRenderBrief(ev, flags, false))
	if o.Emoji {
		if emoji := emojiOf(ev); emoji != "" {
			if flags.Has(event.OMER_COUNT) {
				subj += " " + emoji
			} else {
				subj = emoji + " " + subj
			}
		}
	}
	if o.AppendHebrew {
		if he := download.RenderSubject(ev, "he", true); he != "" {
			subj += " / " + he
		}
	}
	e := icalendar.Event{
		UID:         icalendar.DefaultUID(icalendar.DateOf(hd.Gregorian()), uidDesc(ev), ""),
		Summary:     subj,
		Date:        icalendar.DateOf(hd.Gregorian()),
		Busy:        flags.Has(event.CHAG),
		Private:     flags.Has(event.USER_EVENT),
		Description: Memo(ev, o),
	}
	cat := download.EventCategories(ev)[0]
	if name, ok := seriesNames[cat]; ok && flags.HasAny(download.LearningFlags) {
		e.Category = name
		e.Location = model.Gettext(name, o.Locale)
	} else {
		e.Category = categories[cat]
	}
	switch {
	case flags.Has(event.OMER_COUNT):
		e.Alarm = icalendar.AlarmOmer
	case flags.Has(event.USER_EVENT):
		e.Alarm = icalendar.AlarmUserEvent
	}
	return e
}

// Memo is an event's description. In order of precedence: the Omer's count
// and sefira; the event's own memo (the molad announced on Shabbat Mevarchim,
// the month a Yom Kippur Katan precedes, a learning reading's detail); a
// weekly portion's prose summary; the holiday's description; the event a
// Yizkor belongs to. Then, each after a blank line, the Torah reading and the
// tracked link.
func Memo(ev event.CalEvent, o *Options) string {
	flags := ev.GetFlags()
	if oe, ok := ev.(omer.OmerEvent); ok && oe.OmerDay >= 1 && oe.OmerDay <= len(omerText) {
		t := omerText[oe.OmerDay-1]
		return t.todayIsEn + "\n\n" + t.todayIsHe + "\n\n" +
			oe.Sefira("en") + "\n" + t.sefiraHe + "\n" + oe.Sefira("translit")
	}
	memo := ownMemo(ev, flags, o)
	if memo == "" {
		memo = download.ParshaSummary(ev, o.Locale)
	}
	if memo == "" {
		memo = download.HolidayDescription(ev, flags, o.Locale, false)
		if memo == "" && !download.IsHebrewLocale(o.Locale) {
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
	if flags.Has(event.HEBREW_DATE) {
		// The date converter page is not linked from a feed.
		return memo
	}
	hd := ev.GetDate()
	return appendParagraph(memo, download.EventLink(event.URL(ev), hd.Year(), o.Campaign, utmMedium, o.IL))
}

// ownMemo is the memo an event carries by itself.
func ownMemo(ev event.CalEvent, flags event.HolidayFlags, o *Options) string {
	switch {
	case flags.Has(event.SHABBAT_MEVARCHIM):
		return shabbat.MevarchimMoladMemo(ev.GetDate(), strings.ToLower(o.Locale), "", o.IL, nil)
	case flags.Has(event.YOM_KIPPUR_KATAN):
		month := strings.TrimPrefix(download.UntranslatedDesc(ev), "Yom Kippur Katan ")
		return "Minor Day of Atonement on the day preceeding Rosh Chodesh " + month
	}
	if m, ok := ev.(interface{ Memo(string) string }); ok {
		return m.Memo(o.Locale)
	}
	return ""
}

// noReading are the events that never have a Torah reading of their own.
const noReading = event.OMER_COUNT | event.SHABBAT_MEVARCHIM | event.MOLAD |
	event.USER_EVENT | event.HEBREW_DATE | download.LearningFlags

func torahMemo(ev event.CalEvent, flags event.HolidayFlags, o *Options) string {
	if flags.HasAny(noReading) {
		return ""
	}
	return o.TorahMemos.lookup(ev.GetDate(), uidDesc(ev))
}

// uidDesc is the untranslated description an event's UID is a hash of. It
// must never change: a different UID duplicates the event for every
// subscriber.
func uidDesc(ev event.CalEvent) string {
	switch e := ev.(type) {
	case event.HolidayEvent:
		return model.FixMonthSpelling(e.Desc)
	case hebcal.YizkorEvent:
		return "Yizkor"
	case omer.OmerEvent:
		return "Omer " + strconv.Itoa(e.OmerDay)
	}
	if ev.GetFlags().HasAny(download.LearningFlags) {
		// A reading without its series name, and for Tanakh Yomi without its
		// verses: "Bava Batra 21", "Daniel Seder 1".
		desc := ev.Basename()
		if download.EventCategories(ev)[0] == "tanakhYomi" {
			desc, _, _ = strings.Cut(desc, " (")
		}
		return desc
	}
	if ev.GetFlags().Has(event.HEBREW_DATE) {
		return model.HDateString(ev.GetDate()) // "30 Av 5786"
	}
	return model.FixMonthSpelling(ev.Render("en"))
}

// emojiOf is the emoji a title is decorated with. A night of Chanukah adds
// its candle count as a keycap digit ("🕎3️⃣").
func emojiOf(ev event.CalEvent) string {
	emoji := ev.GetEmoji()
	if he, ok := ev.(event.HolidayEvent); ok && strings.HasPrefix(he.Desc, "Chanukah: ") {
		if n := he.Desc[len("Chanukah: ")]; n >= '1' && n <= '8' && strings.Contains(he.Desc, "Candle") {
			emoji += string(n) + "\uFE0F\u20E3"
		}
	}
	return emoji
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
