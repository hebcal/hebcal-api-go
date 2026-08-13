package pdf

import (
	"testing"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
)

func hebEvent(hy int, hm hdate.HMonth, hd int, subject string) Event {
	d := hdate.New(hy, hm, hd)
	return Event{HD: d, Greg: d.Gregorian(), Subject: subject}
}

// One page per Hebrew month, in chronological order across a year boundary.
// Tishrei starts a new Hebrew year, so the month sequence is not monotonic in
// the month number.
func TestSplitByHebrewMonthOrder(t *testing.T) {
	evs := []Event{
		hebEvent(5787, hdate.Tishrei, 1, "Rosh Hashana"),
		hebEvent(5787, hdate.Cheshvan, 1, "Rosh Chodesh"),
		hebEvent(5787, hdate.Kislev, 25, "Chanukah"),
	}
	pages := SplitByHebrewMonth(evs)
	if len(pages) < 3 {
		t.Fatalf("got %d pages, want at least 3", len(pages))
	}
	want := []hdate.HMonth{hdate.Tishrei, hdate.Cheshvan, hdate.Kislev}
	for i, w := range want {
		if pages[i].Month != w {
			t.Errorf("page %d is %v, want %v", i, pages[i].Month, w)
		}
	}
	if len(pages[0].Days[1]) != 1 {
		t.Errorf("Rosh Hashana should land on 1 Tishrei, got %v", pages[0].Days)
	}
}

// A calendar starting a day or two before Rosh Hashana would otherwise spend a
// whole page on Erev Rosh Hashana alone, so those Elul events fold onto the
// Tishrei page and are drawn in the leading empty cells.
func TestLeadingElulFoldsIntoTishrei(t *testing.T) {
	evs := []Event{
		hebEvent(5786, hdate.Elul, 29, "Erev Rosh Hashana"),
		hebEvent(5787, hdate.Tishrei, 1, "Rosh Hashana"),
		hebEvent(5787, hdate.Tishrei, 10, "Yom Kippur"),
		hebEvent(5787, hdate.Cheshvan, 1, "Rosh Chodesh Cheshvan"),
	}
	pages := SplitByHebrewMonth(evs)
	if pages[0].Month != hdate.Tishrei {
		t.Fatalf("first page is %v, want Tishrei (the Elul events should fold in)", pages[0].Month)
	}
	if got := pages[0].PrevDays[29]; len(got) != 1 || got[0].Subject != "Erev Rosh Hashana" {
		t.Errorf("29 Elul should be carried in PrevDays, got %v", pages[0].PrevDays)
	}
	// The folded events must not also appear as ordinary Tishrei days.
	for day, evs := range pages[0].Days {
		for _, e := range evs {
			if e.Subject == "Erev Rosh Hashana" {
				t.Errorf("Erev Rosh Hashana also appears as Tishrei day %d", day)
			}
		}
	}
}

// A calendar that starts in Elul and stays within the same Hebrew year keeps
// its Elul page: there is no following Tishrei to fold into.
func TestElulKeepsItsPageWithinOneYear(t *testing.T) {
	evs := []Event{
		hebEvent(5786, hdate.Elul, 1, "Rosh Chodesh Elul"),
		hebEvent(5786, hdate.Elul, 29, "Erev Rosh Hashana"),
	}
	pages := SplitByHebrewMonth(evs)
	if len(pages) == 0 || pages[0].Month != hdate.Elul {
		t.Fatalf("expected an Elul page, got %+v", pages)
	}
	if len(pages[0].Days) == 0 {
		t.Error("the Elul page should carry its own events")
	}
}

// A leap year has thirteen months, and both Adars must get a page.
func TestLeapYearHasBothAdars(t *testing.T) {
	if !hdate.IsLeapYear(5787) {
		t.Skip("5787 is not a leap year in this implementation")
	}
	evs := []Event{
		hebEvent(5787, hdate.Shvat, 1, "Rosh Chodesh Sh'vat"),
		hebEvent(5787, hdate.Adar1, 1, "Rosh Chodesh Adar I"),
		hebEvent(5787, hdate.Adar2, 1, "Rosh Chodesh Adar II"),
		hebEvent(5787, hdate.Nisan, 1, "Rosh Chodesh Nisan"),
	}
	pages := SplitByHebrewMonth(evs)
	var seen1, seen2 bool
	for _, p := range pages {
		if p.Month == hdate.Adar1 {
			seen1 = true
		}
		if p.Month == hdate.Adar2 {
			seen2 = true
		}
	}
	if !seen1 || !seen2 {
		t.Errorf("leap year pages missing an Adar: Adar1=%v Adar2=%v", seen1, seen2)
	}
}

// Each page's events are ordered the same way the Gregorian pages order theirs.
//
// The flags matter: eventOrder reserves the flagless-timed slot for the Erev
// Pesach chametz deadlines, which are the only timed events @hebcal/core emits
// without any, so a candle-lighting event built without LIGHT_CANDLES would be
// sorted as one of those.
func TestHebrewPagesSortTheirDays(t *testing.T) {
	d := hdate.New(5787, hdate.Tishrei, 15)
	timed := Event{HD: d, Greg: d.Gregorian(), Subject: "Candle lighting",
		TimeStr: "6:00p", Flags: event.LIGHT_CANDLES}
	holiday := Event{HD: d, Greg: d.Gregorian(), Subject: "Sukkot I", Flags: event.CHAG}
	pages := SplitByHebrewMonth([]Event{timed, holiday})
	got := pages[0].Days[15]
	if len(got) != 2 {
		t.Fatalf("got %d events on 15 Tishrei, want 2", len(got))
	}
	if got[0].Subject != "Sukkot I" {
		t.Errorf("holiday should precede the timed event, got %q first", got[0].Subject)
	}
}

func TestSplitByHebrewMonthEmptyInput(t *testing.T) {
	if pages := SplitByHebrewMonth(nil); pages != nil {
		t.Errorf("got %v, want nil for no events", pages)
	}
}
