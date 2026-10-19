package download

import (
	"fmt"
	"strings"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
)

// GenerateCalEvents produces the hebcal-go events for a calendar, in date
// order, after the category filters (yom-tov only, no minor holidays). It is
// the part of Generate every download format shares: the PDF flattens these
// into cells, the CSV export renders each one as a row.
func GenerateCalEvents(p *Params) ([]event.CalEvent, error) {
	opts := p.Opts
	events, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		return nil, fmt.Errorf("hebcal: %w", err)
	}
	// A whole-Hebrew-year calendar in Hebrew-month mode intentionally carries
	// Tishrei of the year after the requested span too, a head start for next
	// year's calendar. Mirrors hebcal-web's makeHebrewCalendar. Anchored at
	// Year+NumYears, not Year+1: NumYears>1 already covers later years in the
	// main call, and duplicating one of those months here -- out of date
	// order, since it's appended -- would make SplitByHebrewMonth's
	// last-event-as-range-end logic stop short and drop real later pages.
	if p.MonthMode != GregorianArabic && opts.IsHebrewYear && opts.Year != 0 &&
		opts.Start.Abs() == 0 && opts.End.Abs() == 0 {
		nextYear := opts.Year + 1
		if opts.NumYears > 1 {
			nextYear = opts.Year + opts.NumYears
		}
		extraOpts := opts
		extraOpts.IsHebrewYear = false
		extraOpts.NumYears = 0
		extraOpts.Start = hdate.New(nextYear, hdate.Tishrei, 1)
		extraOpts.End = hdate.New(nextYear, hdate.Tishrei, 30) // Tishrei always has 30 days
		extra, err := hebcal.HebrewCalendar(&extraOpts)
		if err != nil {
			return nil, fmt.Errorf("hebcal: next Tishrei: %w", err)
		}
		events = append(events, extra...)
	}
	out := events[:0]
	for _, ev := range events {
		if keepEvent(ev, ev.GetFlags(), p) {
			out = append(out, ev)
		}
	}
	return out, nil
}

// keepEvent applies the two post-generation filters (yom-tov-only and
// no-minor-holidays). Both work on categories rather than flags, which is what
// makes the Purim and Chanukah special case above take effect.
func keepEvent(ev event.CalEvent, flags event.HolidayFlags, p *Params) bool {
	switch {
	case p.YomTovOnly:
		cats := EventCategories(ev)
		return len(cats) > 0 && cats[0] == "holiday" && flags.Has(event.CHAG)
	case p.NoMinorHolidays:
		cats := EventCategories(ev)
		return len(cats) < 2 || cats[1] != "minor"
	}
	return true
}

// EventCategories returns an event's [category, subcategory].
//
// Purim and Chanukah are filed as major even though their flags say
// MINOR_HOLIDAY, which is what keeps them in a calendar that asked only for
// major holidays while Tu BiShvat and Rosh Hashana LaBehemot drop out.
func EventCategories(ev event.CalEvent) []string {
	d := untranslatedDesc(ev)
	if d == "Purim" || d == "Erev Purim" || strings.HasPrefix(d, "Chanukah: ") {
		return []string{"holiday", "major"}
	}
	return ev.GetCategories()
}

// untranslatedDesc returns the event's English description, which is what the
// category rules key on.
func untranslatedDesc(ev event.CalEvent) string {
	switch e := ev.(type) {
	case event.HolidayEvent:
		return e.Desc
	}
	if te, ok := AsTimedEvent(ev); ok {
		return te.Desc
	}
	return ev.Render("en")
}

// AsTimedEvent returns the TimedEvent behind ev. KiddushLevanaEvent embeds a
// TimedEvent but is a type of its own, so a plain type assertion misses it and
// its time would stay baked into the subject instead of being drawn like every
// other clock time.
func AsTimedEvent(ev event.CalEvent) (hebcal.TimedEvent, bool) {
	switch e := ev.(type) {
	case hebcal.TimedEvent:
		return e, true
	case hebcal.KiddushLevanaEvent:
		return e.TimedEvent, true
	}
	return hebcal.TimedEvent{}, false
}
