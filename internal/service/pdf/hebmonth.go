package pdf

import (
	"github.com/hebcal/hdate"
)

// HebMonthPage is one page in Hebrew-month mode (mm=1 or mm=2): a Hebrew month
// and its events keyed by Hebrew day of month.
type HebMonthPage struct {
	Year  int
	Month hdate.HMonth
	Days  map[int][]Event
	// PrevDays holds events from the Elul that was folded into Tishrei, keyed
	// by their Elul day. They are drawn in grey in the leading empty cells.
	PrevDays map[int][]Event
}

// SplitByHebrewMonth groups events into one page per Hebrew month, in
// chronological order. Port of eventsToCellsHeb() in hebcal-web's src/pdf.js.
//
// The one subtlety is the leading Elul: a calendar that starts a day or two
// before Rosh Hashana would otherwise spend a whole page on Erev Rosh Hashana
// alone, so those events are folded into the Tishrei page and drawn in grey in
// the empty cells before the 1st.
func SplitByHebrewMonth(events []Event) []HebMonthPage {
	if len(events) == 0 {
		return nil
	}
	startHd := events[0].HD
	endHd := events[len(events)-1].HD

	year := startHd.Year()
	month := startHd.Month()

	skipInitialElul := month == hdate.Elul &&
		startHd.Year() < endHd.Year() &&
		hdate.New(year+1, hdate.Tishrei, 1).Abs() <= endHd.Abs()
	if skipInitialElul {
		month = hdate.Tishrei
		year++
	}

	type key struct {
		y int
		m hdate.HMonth
	}
	var order []key
	index := make(map[key]int)
	for {
		k := key{year, month}
		if _, seen := index[k]; !seen {
			index[k] = len(order)
			order = append(order, k)
		}

		first := hdate.New(year, month, 1)
		last := hdate.New(year, month, first.DaysInMonth())
		if first.Abs() > endHd.Abs() {
			break
		}
		// Stop once Tishrei of a later year covers the end of the range: the
		// calendar has run past what the caller asked for.
		if year > startHd.Year() && month == hdate.Tishrei && last.Abs() >= endHd.Abs() {
			break
		}

		month++
		if month == hdate.Tishrei {
			year++
		} else {
			monthsInYear := hdate.HMonth(12)
			if hdate.IsLeapYear(year) {
				monthsInYear = 13
			}
			if month > monthsInYear {
				month = hdate.Nisan
			}
		}
	}

	pages := make([]HebMonthPage, len(order))
	for i, k := range order {
		pages[i] = HebMonthPage{
			Year:     k.y,
			Month:    k.m,
			Days:     make(map[int][]Event),
			PrevDays: make(map[int][]Event),
		}
	}
	for _, e := range events {
		hd := e.HD
		k := key{hd.Year(), hd.Month()}
		if i, ok := index[k]; ok {
			pages[i].Days[hd.Day()] = append(pages[i].Days[hd.Day()], e)
			continue
		}
		if skipInitialElul && hd.Month() == hdate.Elul && hd.Year() == startHd.Year() {
			if i, ok := index[key{hd.Year() + 1, hdate.Tishrei}]; ok {
				pages[i].PrevDays[hd.Day()] = append(pages[i].PrevDays[hd.Day()], e)
			}
		}
	}
	for i := range pages {
		for _, evs := range pages[i].Days {
			sortDay(evs)
		}
		for _, evs := range pages[i].PrevDays {
			sortDay(evs)
		}
	}
	return pages
}
