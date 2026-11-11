package download

import (
	"context"
	"net/url"
	"testing"
)

func decodeICS(t *testing.T, rawURL string) *ICSQuery {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	_, q, err := DecodeICS(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestICSQueryLegacy(t *testing.T) {
	q := decodeICS(t, "/hebcal/index.cgi/h.ics?v=1&year=2026&maj=on&s=on&subscribe=1&title=My%20Cal"+
		"&color=%23abcdef&emoji=0&utm_campaign=x&caldesc=a%3Bb&latitude=40.7&longitude=-74")
	if q.Title != "My Cal" || q.Color != "#ABCDEF" || q.UTMCampaign != "x" || q.Caldesc != "a;b" {
		t.Errorf("got %+v", q)
	}
	if q.Emoji == nil || *q.Emoji {
		t.Error("emoji=0 should say no")
	}
	if !q.IsSubscription() || q.Attachment() {
		t.Error("subscribe=1 is a subscription")
	}
	if lat, long, ok := q.Coordinates(); !ok || lat != 40.7 || long != -74 {
		t.Errorf("Coordinates() = %v, %v, %v", lat, long, ok)
	}
	if got, want := q.OptionNames(), "Major Holidays, Torah Readings"; got != want {
		t.Errorf("OptionNames() = %q, want %q", got, want)
	}
}

func TestICSQueryAttachment(t *testing.T) {
	q := decodeICS(t, "/hebcal/index.cgi/h.ics?v=1&year=2026&maj=on")
	if !q.Attachment() || q.IsSubscription() || q.Emoji != nil {
		t.Errorf("got %+v", q)
	}
	// Any value makes it inline; only 1 names it from its contents.
	q = decodeICS(t, "/hebcal/index.cgi/h.ics?v=1&year=2026&maj=on&subscribe=yes")
	if q.Attachment() || q.IsSubscription() {
		t.Errorf("subscribe=yes: %+v", q)
	}
}

// A /v4/ URL's own query string is laid over what its payload says.
func TestICSQueryV4(t *testing.T) {
	const payload = "CAEQARgBKAEwAUABUAFYhYO5AmDqD3gSgAEBiAEBoAEB8AEMqAIB4AMB" // maj, subscribe, emoji, ...
	q := decodeICS(t, "/v4/"+payload+"/hebcal.ics?title=Mine&utm_source=news&subscribe=")
	if q.Title != "Mine" || q.UTMSource != "news" {
		t.Errorf("got %+v", q)
	}
	if q.IsSubscription() {
		t.Error("an empty subscribe in the URL overrides the payload's")
	}
	if q.Emoji == nil || !*q.Emoji {
		t.Error("the payload's emoji should stand")
	}
}

func TestOptionDescriptions(t *testing.T) {
	q := decodeICS(t, "/hebcal/index.cgi/h.ics?v=1&year=2026&mod=on&s=on&dshl=on&yzkr=0")
	if got, want := q.OptionNames(), "Modern Holidays, Torah Readings, Shemirat HaLashon"; got != want {
		t.Errorf("OptionNames() = %q, want %q", got, want)
	}
	// Shemirat HaLashon has no long description, and still takes its place.
	if got, want := q.OptionDescriptions(), "Modern Holidays, Parashat ha-Shavua - Weekly Torah Portion, "; got != want {
		t.Errorf("OptionDescriptions() = %q, want %q", got, want)
	}
}

func TestEventLinkFrom(t *testing.T) {
	const u = "https://www.hebcal.com/holidays/purim-2026"
	// The generated campaigns say where the link was found.
	if got, want := EventLinkFrom(u, 5786, "ical-x", "ical", "icalendar", false),
		"https://hebcal.com/h/purim-2026?uc=ical-x"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// Any other campaign needs the source and medium recorded.
	if got, want := EventLinkFrom(u, 5786, "newsletter", "ical", "icalendar", false),
		"https://hebcal.com/h/purim-2026?us=ical&um=icalendar&uc=newsletter"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// An external page takes the subscription's own source.
	if got, want := EventLinkFrom("https://www.sefaria.org/Yoma.49a", 0, "ical-x", "news", "icalendar", false),
		"https://www.sefaria.org/Yoma.49a?utm_source=news&utm_medium=icalendar&utm_campaign=ical-x"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
