package download

import (
	"strconv"
	"strings"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/locales"
	"golang.org/x/text/unicode/norm"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
)

// LearningFlags are the daily-learning series flags.
const LearningFlags = event.DAF_YOMI | event.MISHNA_YOMI |
	event.NACH_YOMI | event.YERUSHALMI_YOMI | event.DAILY_LEARNING

// ShouldRenderBrief reports whether an event's title is its brief form:
// timed events (the time is shown separately), Hebrew dates other than the
// first of a month, daily learning, Shabbat Mevarchim and Yom Kippur Katan.
func ShouldRenderBrief(ev event.CalEvent, flags event.HolidayFlags, timed bool) bool {
	switch {
	case timed:
		return true
	case flags.Has(event.HEBREW_DATE):
		return ev.GetDate().Day() != 1
	case flags.HasAny(LearningFlags | event.SHABBAT_MEVARCHIM):
		return true
	case flags.Has(event.MINOR_FAST):
		return strings.HasPrefix(UntranslatedDesc(ev), "Yom Kippur Katan")
	}
	return false
}

// RenderSubject renders an event's title in locale, in its brief or full form,
// without emoji. Rosh Hashana renders the year as a number in every locale,
// Hebrew included.
func RenderSubject(ev event.CalEvent, locale string, brief bool) string {
	var s string
	hd := ev.GetDate()
	switch he, isHoliday := ev.(event.HolidayEvent); {
	case isHoliday && hd.Month() == hdate.Tishrei && hd.Day() == 1 &&
		strings.HasPrefix(he.Desc, "Rosh Hashana"):
		s = model.Gettext("Rosh Hashana", locale) + " " + strconv.Itoa(hd.Year())
	case brief:
		s = event.RenderBrief(ev, locale)
	default:
		s = ev.Render(locale)
	}
	s = StripEmoji(s)
	// Daily-learning readings keep their typewriter apostrophes ("Fourth
	// Year's Fruit"); only the calendar's own names are typeset.
	if IsHebrewLocale(locale) || ev.GetFlags().HasAny(LearningFlags) {
		return model.FixMonthSpelling(s)
	}
	return model.FixMonthSpelling(jsutil.SmartApostrophe(s))
}

// HolidayDescription is the holiday's description from the MEMO catalog, in
// Hebrew for a Hebrew locale and English otherwise, looked up by the event's
// description and then by its basename. With firstSentence it stops at the
// first full stop. A weekly Torah portion has none: its MEMO entry is the
// portion's prose summary, which is a different thing (see ParshaSummary).
func HolidayDescription(ev event.CalEvent, flags event.HolidayFlags, locale string, firstSentence bool) string {
	if flags.Has(event.PARSHA_HASHAVUA) {
		return ""
	}
	localeName := "en"
	if IsHebrewLocale(locale) {
		localeName = "he"
	}
	key := UntranslatedDesc(ev)
	if flags.Has(event.SHABBAT_MEVARCHIM) {
		key = "Shabbat Mevarchim Chodesh"
	}
	s := lookupMemo("MEMO:"+key, localeName)
	if s == "" {
		s = lookupMemo("MEMO:"+Basename(ev), localeName)
	}
	s = norm.NFC.String(s)
	if firstSentence {
		if i := strings.Index(s, "."); i >= 0 {
			s = s[:i]
		}
	}
	return s
}

// ParshaSummary is the prose summary of a weekly Torah portion from the MEMO
// catalog: Hebrew for a Hebrew locale when there is one, English otherwise. A
// doubled portion joins its two halves' summaries, and has none unless both
// have one. Any other event has none.
func ParshaSummary(ev event.CalEvent, locale string) string {
	if !ev.GetFlags().Has(event.PARSHA_HASHAVUA) {
		return ""
	}
	name := strings.TrimPrefix(ev.Render("en"), "Parashat ")
	if IsHebrewLocale(locale) {
		if s := parshaSummary(name, "he"); s != "" {
			return s
		}
	}
	return parshaSummary(name, "en")
}

func parshaSummary(name, localeName string) string {
	// A hyphen does not make a pair: Lech-Lecha is one portion.
	if p1, p2, _ := strings.Cut(name, "-"); doubledSlugs[jsutil.MakeAnchor(name)] {
		s1, s2 := lookupMemo("MEMO:"+p1, localeName), lookupMemo("MEMO:"+p2, localeName)
		if s1 == "" || s2 == "" {
			return ""
		}
		return s1 + "\n " + s2
	}
	return lookupMemo("MEMO:"+name, localeName)
}

func lookupMemo(key, localeName string) string {
	// An unknown English key comes back as itself, which means not found.
	if s, ok := locales.LookupTranslation(key, localeName); ok && s != key {
		return s
	}
	return ""
}

// Basename is the event's description without the qualifiers that tell the
// days of one holiday apart ("Sukkot III (CH’’M)" => "Sukkot"). Chanukah
// candle-lighting is the holiday itself, so it takes the holiday's.
func Basename(ev event.CalEvent) string {
	if timed, ok := ev.(hebcal.TimedEvent); ok && timed.LinkedEvent != nil &&
		timed.Flags.Has(event.CHANUKAH_CANDLES) {
		return timed.LinkedEvent.Basename()
	}
	return model.FixMonthSpelling(ev.Basename())
}

// UntranslatedDesc returns the event's English description, with the
// website's one-m spelling of Tamuz.
func UntranslatedDesc(ev event.CalEvent) string {
	if he, ok := ev.(event.HolidayEvent); ok {
		return model.FixMonthSpelling(he.Desc)
	}
	if te, ok := AsTimedEvent(ev); ok {
		return te.Desc
	}
	return model.FixMonthSpelling(ev.Render("en"))
}

// LinkedEvent returns the event a timed event or Yizkor belongs to, or nil.
func LinkedEvent(ev event.CalEvent) event.CalEvent {
	switch e := ev.(type) {
	case hebcal.TimedEvent:
		return e.LinkedEvent
	case hebcal.YizkorEvent:
		return e.LinkedEvent
	}
	return nil
}

// IsHebrewLocale reports whether locale is one of the Hebrew-script locales.
func IsHebrewLocale(locale string) bool {
	switch strings.ToLower(locale) {
	case "he", "he-x-nonikud", "h":
		return true
	}
	return false
}

// StripEmoji removes the holiday emoji hebcal-go appends to some titles.
func StripEmoji(s string) string {
	out := strings.Map(func(r rune) rune {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) || r == 0xFE0F {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(out)
}
