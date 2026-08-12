package model

import (
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/omer"
	"github.com/hebcal/hebcal-go/sedra"
)

// GetEvents returns the list of holidays and other calendar events occurring
// on the given Hebrew date. Ported from converter.js getEvents(), but leaning
// on hebcal.HebrewCalendar for holiday, Shabbat Mevarchim, and Molad events.
func GetEvents(hd hdate.HDate, il bool) []CalEv {
	// Matan Torah traditionally on 6 Sivan 2448
	if hd.Abs() < -479441 {
		return nil
	}
	// Look up this day's holiday/Shabbat Mevarchim/Molad events from the
	// memoized whole-year computation instead of recomputing the year per day.
	evs := holidayEventsForYear(hd.Year(), il)[hd.Abs()]
	events := make([]CalEv, 0, 4)
	for _, ev := range evs {
		if hev, ok := ev.(event.HolidayEvent); ok {
			if hev.Desc == "Chanukah: 1 Candle" {
				continue
			}
			events = append(events, HolidayEv{hev})
		} else {
			events = append(events, GenericEv{ev})
		}
	}
	events = append(events, parshaEvents(hd, il)...)
	events = append(events, omerEvents(hd)...)
	return events
}

// holidaysOnDate returns the holiday events for a single Hebrew date, using the
// memoized per-year holiday index.
func holidaysOnDate(hd hdate.HDate, il bool) []event.HolidayEvent {
	return holidaysForYearByDate(hd.Year(), il)[hd.Abs()]
}

// hasHolidayReading reports whether the date has a special (non-parsha) full
// Torah reading. This approximates @hebcal/leyning getLeyningOnDate() with
// `fullkriyah && !parshaNum`: major holidays, chol hamoed, Rosh Chodesh, fast
// days, Chanukah, and Purim all have full kriyah readings.
func hasHolidayReading(hd hdate.HDate, il bool) bool {
	const readingFlags = event.CHAG | event.CHOL_HAMOED | event.ROSH_CHODESH |
		event.MINOR_FAST | event.MAJOR_FAST | event.CHANUKAH_CANDLES
	for _, hev := range holidaysOnDate(hd, il) {
		if hev.Desc == "Chanukah: 1 Candle" {
			continue // candle-lighting the previous evening; no Torah reading
		}
		if hev.Flags&event.YOM_KIPPUR_KATAN != 0 ||
			hev.Desc == "Ta'anit BeHaB" || hev.Desc == "Ta'anit Bechorot" {
			continue // fast-day flag but no special Torah reading in leyning
		}
		if hev.Flags&readingFlags != 0 {
			return true
		}
		if hev.Desc == "Purim" || hev.Desc == "Shushan Purim" {
			return true
		}
	}
	return false
}

// parshaEvents returns the upcoming Torah reading for the date.
// Ported from converter.js getParshaEvents().
func parshaEvents(hd hdate.HDate, il bool) []CalEv {
	saturday := hd.OnOrAfter(time.Saturday)
	hy := saturday.Year()
	s := sedra.New(hy, il)
	parsha := s.Lookup(saturday)
	if !parsha.Chag {
		return []CalEv{ParshaEv{Sat: saturday, Parsha: parsha, IL: il}}
	}
	if hasHolidayReading(hd, il) {
		return nil
	}
	mm := hd.Month()
	dd := hd.Day()
	if mm == hdate.Tishrei && dd > 2 && dd < 15 {
		st := SimchatTorahDate(hy, il)
		p := sedra.Parsha{Name: []string{"Vezot Haberakhah"}}
		return []CalEv{ParshaEv{Sat: st, Parsha: p, IL: il}}
	}
	var events []CalEv
	for _, hev := range holidaysOnDate(saturday, il) {
		events = append(events, PseudoParshaEv{HolidayEv{hev}})
	}
	return events
}

// omerEvents returns the Sefirat HaOmer count for the date, if within the
// Omer. Ported from converter.js makeOmer(); appended after the parsha so
// the events array matches the documented API output order.
func omerEvents(hd hdate.HDate) []CalEv {
	mm := hd.Month()
	if mm == hdate.Nisan || mm == hdate.Iyyar || mm == hdate.Sivan {
		beginOmer := hdate.ToRD(hd.Year(), hdate.Nisan, 16)
		abs := hd.Abs()
		if abs >= beginOmer && abs < beginOmer+49 {
			return []CalEv{OmerEv{omer.NewOmerEvent(hd, int(abs-beginOmer)+1)}}
		}
	}
	return nil
}
