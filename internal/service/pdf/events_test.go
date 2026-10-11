package pdf

import (
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"
)

func ev(desc string, flags event.HolidayFlags, timeStr string) Event {
	return Event{Subject: desc, Flags: flags, TimeStr: timeStr}
}

// generateDay renders one day in Toronto with candle-lighting, Kiddush
// Levana and Mishna Yomi, as subjects in the order sortDay leaves them.
func generateDay(t *testing.T, year int, month time.Month, day int, learning []Event) string {
	t.Helper()
	d := hdate.FromGregorian(year, month, day)
	toronto := zmanim.NewLocation("Toronto", "CA", 43.70011, -79.4163, 0, "America/Toronto")
	p := &Params{Locale: "en", MonthMode: GregorianArabic}
	p.Opts = hebcal.CalOptions{
		Start: d, End: d, Location: &toronto, CandleLighting: true, HavdalahMins: 42,
		Sedrot: true, KiddushLevanaMaharil: true, MishnaYomi: true,
	}
	events, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for i := range learning {
		learning[i].HD, learning[i].Greg = d, d.Gregorian()
	}
	events = mergeLearning(events, learning)
	pages := SplitByGregorianMonth(events)
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	return subjects(pages[0].Days[day])
}

// A day is drawn in hebcal-go's order, which ranks events by time of day:
// timed events before dawn, the morning's fast, the chametz deadlines, daily
// learning, the evening's holiday and then the evening's timed events.
func TestSortDayKeepsHebcalTimeOfDayOrder(t *testing.T) {
	tests := []struct {
		year  int
		month time.Month
		day   int
		want  string
	}{
		{2027, time.October, 4,
			"Fast begins | Tzom Gedaliah | Berakhot 5:1-2 | Fast ends"},
		{2027, time.October, 15,
			"Latest Kiddush Levana | Berakhot 8:5-6 | Erev Sukkot | Candle lighting"},
		{2028, time.April, 10,
			"Fast begins | Ta’anit Bechorot | Finish eating chametz | Biur Chametz | " +
				"Terumot 7:7-8:1 | Erev Pesach | Candle lighting"},
	}
	for _, tt := range tests {
		if got := generateDay(t, tt.year, tt.month, tt.day, nil); got != tt.want {
			t.Errorf("%d-%02d-%02d = %s\n           want %s", tt.year, tt.month, tt.day, got, tt.want)
		}
	}
}

// A learning row fetched from the sidecar is appended to the end of the day by
// mergeLearning, and sortDay moves it up to the learning slot: after the
// series hebcal-go generated, ahead of the evening's holiday and timed events.
func TestSortDayPutsFetchedLearningInTheLearningSlot(t *testing.T) {
	psalms := Event{Subject: "Psalms 60-65", Learning: true, rank: hebcal.TimeOfDayLearning}
	got := generateDay(t, 2027, time.October, 15, []Event{psalms})
	want := "Latest Kiddush Levana | Berakhot 8:5-6 | Psalms 60-65 | Erev Sukkot | Candle lighting"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Events of equal rank keep the order hebcal-go produced them in, so a stable
// sort is required.
func TestSortDayIsStable(t *testing.T) {
	evs := []Event{
		{Subject: "Tu BiShvat", rank: hebcal.TimeOfDayDay},
		{Subject: "Shabbat Shirah", rank: hebcal.TimeOfDayDay},
		{Subject: "Psalms 1-2", rank: hebcal.TimeOfDayLearning},
		{Subject: "Daf Yomi", rank: hebcal.TimeOfDayLearning},
	}
	sortDay(evs)
	if got, want := subjects(evs), "Tu BiShvat | Shabbat Shirah | Psalms 1-2 | Daf Yomi"; got != want {
		t.Errorf("stable order not preserved: %s", got)
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

// AppendHebrew (lg=ah/sh) draws the Hebrew name on its own, next to a bold
// clock time the renderer already draws separately. ev.Render("he") for a
// timed event comes back as "הדלקת נרות: 19:22", and without the same trim
// applied to the English subject above, that time was baked into
// HebrewBrief and drawn a second time.
func TestAppendHebrewBriefOmitsTimeForTimedEvents(t *testing.T) {
	p := gregorianParams()
	p.AppendHebrew = true
	events, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	found := false
	for _, e := range events {
		if !e.Timed() || e.Subject != "Candle lighting" {
			continue
		}
		found = true
		if strings.HasSuffix(e.HebrewBrief, e.TimeStr) || strings.ContainsAny(e.HebrewBrief, "0123456789") {
			t.Errorf("HebrewBrief for timed event %q = %q, want no clock time", e.Subject, e.HebrewBrief)
		}
	}
	if !found {
		t.Fatal("no timed events generated")
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

// The minor-holiday filter goes by category, not by flag: Purim and Chanukah
// are filed as major even though their flags say MINOR_HOLIDAY. That is what
// keeps them in a calendar asking only for major holidays, while Tu BiShvat and
// Rosh Hashana LaBehemot drop out -- all four carry the same flag.
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

// A whole Hebrew-year calendar in Hebrew-month mode carries real events for
// Tishrei of the following year too -- a head start for next year's printed
// calendar -- rather than an empty trailing page. Mirrors hebcal-web's
// makeHebrewCalendar. Gregorian-month mode has no Hebrew-month page to fill,
// so the same request must carry none of this.
func TestGenerateIncludesNextYearTishrei(t *testing.T) {
	p := &Params{
		MonthMode: HebrewArabic,
		Opts:      hebcal.CalOptions{Year: 5787, IsHebrewYear: true},
	}
	evs, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	var sawNextRoshHashana bool
	for _, e := range evs {
		if e.HD.Year() != 5788 {
			continue
		}
		if e.HD.Month() != hdate.Tishrei {
			t.Errorf("event %q on %d %v %d: next year's calendar should carry only Tishrei",
				e.Subject, e.HD.Day(), e.HD.Month(), e.HD.Year())
		}
		if strings.Contains(e.Subject, "Rosh Hashana") {
			sawNextRoshHashana = true
		}
	}
	if !sawNextRoshHashana {
		t.Error("expected next year's Rosh Hashana among the events, got none")
	}

	p.MonthMode = GregorianArabic
	evs, err = Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.HD.Year() == 5788 {
			t.Errorf("Gregorian-month mode should not carry next year's events, got %q on %v %d, %d",
				e.Subject, e.HD.Month(), e.HD.Day(), e.HD.Year())
		}
	}
}

// A multi-year request (NumYears > 1) must not duplicate a year the main
// call already covers: the trailing Tishrei peek belongs after the whole
// span (Year+NumYears), not Year+1, or it both repeats an already-generated
// month and, appended out of date order, breaks SplitByHebrewMonth's
// last-event-as-range-end logic into dropping real later pages.
func TestGenerateNumYearsDoesNotDuplicateTishrei(t *testing.T) {
	p := &Params{
		MonthMode: HebrewArabic,
		Opts:      hebcal.CalOptions{Year: 5787, IsHebrewYear: true, NumYears: 2},
	}
	evs, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	// Match the exact first-day subject ("Rosh Hashana 5788"/"...5789"), not
	// a substring: a loose Contains also matches "Rosh Hashana II" (day 2 of
	// the same, legitimate occurrence) and would over-count by design.
	var roshHashana5788 int
	var sawElul5788, sawTishrei5789 bool
	for _, e := range evs {
		if e.Subject == "Rosh Hashana 5788" {
			roshHashana5788++
		}
		if e.HD.Year() == 5788 && e.HD.Month() == hdate.Elul {
			sawElul5788 = true
		}
		if e.HD.Year() == 5789 {
			if e.HD.Month() != hdate.Tishrei {
				t.Errorf("event %q on %v %d, %d: the trailing peek should carry only Tishrei 5789",
					e.Subject, e.HD.Month(), e.HD.Day(), e.HD.Year())
			}
			if e.Subject == "Rosh Hashana 5789" {
				sawTishrei5789 = true
			}
		}
	}
	if roshHashana5788 != 1 {
		t.Errorf("Rosh Hashana 5788 should appear exactly once (NumYears=2 already covers it), got %d", roshHashana5788)
	}
	if !sawElul5788 {
		t.Error("Elul 5788, the true end of the requested 2-year span, should still be present")
	}
	if !sawTishrei5789 {
		t.Error("expected the trailing peek into Tishrei 5789 (Year+NumYears), got none")
	}
}

func TestYomTovOnlyKeepsOnlyChagim(t *testing.T) {
	opts := hebcal.CalOptions{Year: 2026}
	evs, _ := hebcal.HebrewCalendar(&opts)
	p := &Params{YomTovOnly: true}
	for _, ev := range evs {
		keep := keepEvent(ev, ev.GetFlags(), p)
		if keep && !ev.GetFlags().Has(event.CHAG) {
			t.Errorf("%q is not a chag but survived yomTovOnly", untranslatedDesc(ev))
		}
	}
}

// Latest Kiddush Levana is a timed event: its clock time is drawn separately,
// with the a/p suffix in a 12-hour country and 24-hour elsewhere, and is not
// left in the subject. KiddushLevanaEvent embeds TimedEvent rather than being
// one, which once left "Latest Kiddush Levana: 9:44" in the subject with no
// suffix and no separate time.
func TestKiddushLevanaIsTimed(t *testing.T) {
	tests := []struct {
		city, locale          string
		hour12                int32
		wantSubject, wantTime string
	}{
		{"New York", "en", 0, "Latest Kiddush Levana", "9:44p"},
		// The same instant, early on the following morning.
		{"Jerusalem", "en", 0, "Latest Kiddush Levana", "03:44"},
		{"Jerusalem", "en", 1, "Latest Kiddush Levana", "3:44a"},
		{"New York", "he", 0, "סוֹף זְמַן קִדּוּשׁ לְבָנָה", "9:44p"},
	}
	for _, tt := range tests {
		p := &Params{Locale: tt.locale, MonthMode: GregorianArabic, Hour12: tt.hour12}
		p.Opts = hebcal.CalOptions{
			Year: 2026, Month: 10, CandleLighting: true, KiddushLevanaMaharil: true,
			Location: zmanim.LookupCity(tt.city),
		}
		events, err := Generate(p)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		found := false
		for _, e := range events {
			if !e.Flags.Has(event.KIDDUSH_LEVANA) {
				continue
			}
			found = true
			if e.Subject != tt.wantSubject || e.TimeStr != tt.wantTime {
				t.Errorf("%s/%s: got subject %q time %q, want %q %q",
					tt.city, tt.locale, e.Subject, e.TimeStr, tt.wantSubject, tt.wantTime)
			}
		}
		if !found {
			t.Errorf("%s/%s: no Kiddush Levana event", tt.city, tt.locale)
		}
	}
}
