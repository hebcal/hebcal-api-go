package download

import (
	"testing"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
)

// The Rosh Hashana year is whatever hebcal-go renders: a number, except in
// Hebrew, which writes it in gematriya.
func TestRenderSubjectRoshHashanaYear(t *testing.T) {
	ev := event.NewHolidayEvent(hdate.New(5787, hdate.Tishrei, 1), "Rosh Hashana 5787", event.CHAG, "")
	for locale, want := range map[string]string{
		"en": "Rosh Hashana 5787",
		"he": "רֹאשׁ הַשָּׁנָה תשפ״ז",
	} {
		if got := RenderSubject(ev, locale, false); got != want {
			t.Errorf("%s: got %q, want %q", locale, got, want)
		}
	}
}
