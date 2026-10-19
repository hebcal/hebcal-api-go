package download

import (
	"strings"
	"testing"

	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
)

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
