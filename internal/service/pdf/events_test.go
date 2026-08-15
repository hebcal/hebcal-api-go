package pdf

import (
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
)

func ev(desc string, flags event.HolidayFlags, timeStr string) Event {
	return Event{Subject: desc, Flags: flags, TimeStr: timeStr}
}

// @hebcal/core emits a day's events by walking the holidays that fall on it and
// pushing each one's related events around it. This is the 14 Nisan case, which
// is both a fast day and Erev Pesach: hebcal-go walks the two holidays in the
// opposite order, so without sorting the chametz deadlines came out above
// "Fast begins" instead of below it.
func TestSortDayErevPesachOnAFastDay(t *testing.T) {
	// The order hebcal-go produces.
	evs := []Event{
		ev("Finish eating chametz", 0, "11:00a"),
		ev("Biur Chametz", 0, "12:05p"),
		ev("Erev Pesach", event.EREV|event.LIGHT_CANDLES|event.CHUL_ONLY, ""),
		ev("Fast begins", event.MINOR_FAST, "5:19a"),
		ev("Ta'anit Bechorot", event.MINOR_FAST, ""),
		ev("Candle lighting", event.LIGHT_CANDLES, "7:22p"),
	}
	sortDay(evs)
	want := []string{
		"Fast begins",
		"Ta'anit Bechorot",
		"Finish eating chametz",
		"Biur Chametz",
		"Erev Pesach",
		"Candle lighting",
	}
	for i, w := range want {
		if evs[i].Subject != w {
			t.Errorf("position %d = %q, want %q\nfull order: %s", i, evs[i].Subject, w, subjects(evs))
			return
		}
	}
}

// "Fast ends" carries the same fast flag as "Fast begins" but sorts after the
// fast day itself, matching @hebcal/core's begins/holiday/ends sequence -- e.g.
// on Asara B'Tevet. It still sorts before the parsha and candle lighting, since
// @hebcal/core pushes the fast end inside the holiday block. Regression for the
// order that put "Fast ends" above the fast day.
func TestSortDayFastEndsAfterFastDay(t *testing.T) {
	fastEnds := ev("Fast ends", event.MINOR_FAST, "5:14p")
	fastEnds.FastEnds = true
	evs := []Event{
		fastEnds,
		ev("Parashat Vayechi", event.PARSHA_HASHAVUA, ""),
		ev("Fast begins", event.MINOR_FAST, "6:01a"),
		ev("Candle lighting", event.LIGHT_CANDLES, "4:10p"),
		ev("Asara B'Tevet", event.MINOR_FAST, ""),
	}
	sortDay(evs)
	want := []string{
		"Fast begins",
		"Asara B'Tevet",
		"Fast ends",
		"Parashat Vayechi",
		"Candle lighting",
	}
	for i, w := range want {
		if evs[i].Subject != w {
			t.Errorf("position %d = %q, want %q\nfull order: %s", i, evs[i].Subject, w, subjects(evs))
			return
		}
	}
}

// A holiday can carry LIGHT_CANDLES itself -- Erev Pesach does -- so timed
// events have to be told apart by having a clock time, not by that flag.
// Classifying Erev Pesach as timed pushed it to the bottom of the cell.
func TestErevPesachIsNotTreatedAsATimedEvent(t *testing.T) {
	erev := ev("Erev Pesach", event.EREV|event.LIGHT_CANDLES|event.CHUL_ONLY, "")
	candles := ev("Candle lighting", event.LIGHT_CANDLES, "7:22p")
	if eventOrder(&erev) >= eventOrder(&candles) {
		t.Errorf("Erev Pesach (order %d) should sort before candle lighting (order %d)",
			eventOrder(&erev), eventOrder(&candles))
	}
}

// A Shabbat carrying a minor holiday, a special Shabbat and a parsha lists the
// holidays first and the parsha after them.
func TestSortDayHolidaysBeforeParsha(t *testing.T) {
	evs := []Event{
		ev("Parashat Beshalach", event.PARSHA_HASHAVUA, ""),
		ev("Shabbat Shirah", event.SPECIAL_SHABBAT, ""),
		ev("Tu BiShvat", event.MINOR_HOLIDAY, ""),
		ev("Havdalah", event.YOM_TOV_ENDS, "5:57p"),
	}
	sortDay(evs)
	if evs[len(evs)-1].Subject != "Havdalah" {
		t.Errorf("Havdalah should be last, got %s", subjects(evs))
	}
	parsha, hol := -1, -1
	for i, e := range evs {
		if e.Flags&event.PARSHA_HASHAVUA != 0 {
			parsha = i
		}
		if e.Flags&(event.MINOR_HOLIDAY|event.SPECIAL_SHABBAT) != 0 && hol < i {
			hol = i
		}
	}
	if parsha < hol {
		t.Errorf("parsha at %d should follow the holidays (last at %d): %s", parsha, hol, subjects(evs))
	}
}

// Every daily-learning schedule sorts into one slot, in the order hebcal-go
// emitted them -- which is the order the request listed them in. Only four of
// the thirteen have a flag of their own; the rest carry the generic
// DAILY_LEARNING, and testing only the four dropped them into the holiday slot,
// where they came out above Daf Yomi and Mishna Yomi rather than beside them.
//
// A Boston 2027 calendar with F+myomi+dps+dr3 drew every cell as Psalms,
// Rambam, Daf Yomi, Mishna Yomi where hebcal-web draws Daf Yomi, Mishna Yomi,
// Psalms, Rambam. That also cost 30 Mishna Yomi links, because on a crowded day
// the row that overflows the cell is whichever one is last.
func TestSortDayKeepsAllDailyLearningTogether(t *testing.T) {
	evs := []Event{
		ev("Temurah 12", event.DAF_YOMI, ""),
		ev("Negaim 12:7-13:1", event.MISHNA_YOMI, ""),
		ev("Psalms 106-107", event.DAILY_LEARNING, ""),
		ev("Mourning 6-8", event.DAILY_LEARNING, ""),
		ev("Rosh Chodesh Tevet", event.ROSH_CHODESH, ""),
		ev("Candle lighting", event.LIGHT_CANDLES, "3:59p"),
	}
	sortDay(evs)
	want := "Rosh Chodesh Tevet | Temurah 12 | Negaim 12:7-13:1 | " +
		"Psalms 106-107 | Mourning 6-8 | Candle lighting"
	if got := subjects(evs); got != want {
		t.Errorf("sortDay() = %s\n          want %s", got, want)
	}
}

// The generic flag must not sort a learning row into the holiday slot, which is
// the specific regression above.
func TestDailyLearningIsNotAHoliday(t *testing.T) {
	learning := eventOrder(&Event{Flags: event.DAILY_LEARNING})
	holiday := eventOrder(&Event{Flags: event.ROSH_CHODESH})
	dafYomi := eventOrder(&Event{Flags: event.DAF_YOMI})
	if learning == holiday {
		t.Errorf("DAILY_LEARNING sorts to the holiday slot %d", learning)
	}
	if learning != dafYomi {
		t.Errorf("DAILY_LEARNING sorts to %d, Daf Yomi to %d; the two must share a slot",
			learning, dafYomi)
	}
}

// Events sharing a position keep the order hebcal-go produced them in, so a
// stable sort is required.
func TestSortDayIsStable(t *testing.T) {
	evs := []Event{
		ev("Tu BiShvat", event.MINOR_HOLIDAY, ""),
		ev("Shabbat Shirah", event.SPECIAL_SHABBAT, ""),
	}
	sortDay(evs)
	if evs[0].Subject != "Tu BiShvat" || evs[1].Subject != "Shabbat Shirah" {
		t.Errorf("stable order not preserved: %s", subjects(evs))
	}
}

func subjects(evs []Event) string {
	out := ""
	for i, e := range evs {
		if i > 0 {
			out += " | "
		}
		out += e.Subject
	}
	return out
}

func TestStripEmoji(t *testing.T) {
	if got := stripEmoji("Chanukah: 1 Candle 🕎"); got != "Chanukah: 1 Candle" {
		t.Errorf("stripEmoji() = %q", got)
	}
	// Hebrew and Latin text must survive untouched.
	for _, s := range []string{"Parashat Eikev", "פָּרָשַׁת עֵקֶב"} {
		if got := stripEmoji(s); got != s {
			t.Errorf("stripEmoji(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestTimedReportsAClockTime(t *testing.T) {
	timed := ev("Candle lighting", event.LIGHT_CANDLES, "7:22p")
	allDay := ev("Erev Pesach", event.EREV, "")
	if !timed.Timed() || allDay.Timed() {
		t.Error("Timed() should follow TimeStr")
	}
}

// A quiet month still gets a page, so a gap in the events does not silently
// drop a month from the calendar.
func TestSplitByGregorianMonthFillsEmptyMonths(t *testing.T) {
	mk := func(y int, m time.Month, d int) Event {
		g := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		return Event{Greg: g, HD: hdate.FromTime(g), Subject: "x"}
	}
	// August and October, with nothing in September.
	pages := SplitByGregorianMonth([]Event{mk(2026, time.August, 1), mk(2026, time.October, 1)})
	if len(pages) != 3 {
		t.Fatalf("got %d pages, want 3 (August, September, October)", len(pages))
	}
	wantMonths := []time.Month{time.August, time.September, time.October}
	for i, w := range wantMonths {
		if pages[i].Month != w {
			t.Errorf("page %d is %v, want %v", i, pages[i].Month, w)
		}
	}
	if len(pages[1].Days) != 0 {
		t.Errorf("September should have no events, got %d days", len(pages[1].Days))
	}
}

func TestSplitByGregorianMonthOrdersPagesChronologically(t *testing.T) {
	mk := func(y int, m time.Month) Event {
		g := time.Date(y, m, 15, 0, 0, 0, 0, time.UTC)
		return Event{Greg: g, HD: hdate.FromTime(g), Subject: "x"}
	}
	pages := SplitByGregorianMonth([]Event{
		mk(2026, time.November), mk(2027, time.January), mk(2026, time.December),
	})
	for i := 1; i < len(pages); i++ {
		a, b := pages[i-1], pages[i]
		if a.Year > b.Year || (a.Year == b.Year && a.Month > b.Month) {
			t.Errorf("pages out of order at %d: %d-%v then %d-%v", i, a.Year, a.Month, b.Year, b.Month)
		}
	}
}

func TestSplitByGregorianMonthEmptyInput(t *testing.T) {
	if pages := SplitByGregorianMonth(nil); pages != nil {
		t.Errorf("got %v, want nil for no events", pages)
	}
}

// hebcal-web filters by category rather than by flag, and @hebcal/rest-api's
// getEventCategories files Purim and Chanukah as major even though their flags
// say MINOR_HOLIDAY. That is what keeps them in a calendar asking only for
// major holidays, while Tu BiShvat and Rosh Hashana LaBehemot drop out --
// all four carry the same flag.
func TestMinorHolidayFilterUsesCategories(t *testing.T) {
	opts := hebcal.CalOptions{Year: 2026}
	evs, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		t.Fatal(err)
	}
	majorOnly := &Params{NoMinorHolidays: true}
	seen := map[string]bool{}
	for _, ev := range evs {
		if keepEvent(ev, ev.GetFlags(), majorOnly) {
			seen[untranslatedDesc(ev)] = true
		}
	}
	for _, want := range []string{"Purim", "Erev Purim"} {
		if !seen[want] {
			t.Errorf("%q should survive the minor-holiday filter", want)
		}
	}
	var chanukah bool
	for d := range seen {
		if strings.HasPrefix(d, "Chanukah: ") {
			chanukah = true
		}
	}
	if !chanukah {
		t.Error("Chanukah should survive the minor-holiday filter")
	}
	for _, unwanted := range []string{"Tu BiShvat", "Rosh Hashana LaBehemot"} {
		if seen[unwanted] {
			t.Errorf("%q carries the same flag but should be filtered out", unwanted)
		}
	}
}

// Asking for minor holidays keeps all of them.
func TestMinorHolidaysKeptWhenRequested(t *testing.T) {
	opts := hebcal.CalOptions{Year: 2026}
	evs, _ := hebcal.HebrewCalendar(&opts)
	all := &Params{}
	for _, ev := range evs {
		if untranslatedDesc(ev) == "Tu BiShvat" && !keepEvent(ev, ev.GetFlags(), all) {
			t.Error("Tu BiShvat should be kept when minor holidays were requested")
		}
	}
}

func TestYomTovOnlyKeepsOnlyChagim(t *testing.T) {
	opts := hebcal.CalOptions{Year: 2026}
	evs, _ := hebcal.HebrewCalendar(&opts)
	p := &Params{YomTovOnly: true}
	for _, ev := range evs {
		keep := keepEvent(ev, ev.GetFlags(), p)
		if keep && ev.GetFlags()&event.CHAG == 0 {
			t.Errorf("%q is not a chag but survived yomTovOnly", untranslatedDesc(ev))
		}
	}
}
