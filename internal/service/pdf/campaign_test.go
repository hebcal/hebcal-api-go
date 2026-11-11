package pdf

import (
	"testing"
	"time"

	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/service/download"
)

func TestCampaignFromTitle(t *testing.T) {
	tests := []struct{ title, want string }{
		{"Hebcal Palo Alto 2026-2027", "pdf-palo-alto-2026-2027"},
		{"Hebcal Diaspora August 2026", "pdf-diaspora-august-2026"},
		{"Hebcal Israel 5787", "pdf-israel-5787"},
		{"Hebcal", "pdf"},
	}
	for _, tt := range tests {
		if got := campaignFromTitle(tt.title); got != tt.want {
			t.Errorf("campaignFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestCampaignPrefersTheAsciiName(t *testing.T) {
	// A range spanning more than one month of one year, so the title is the
	// location plus a bare year rather than "<Month> <year>".
	events := []Event{
		{Greg: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{Greg: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	}
	tests := []struct {
		name         string
		city, ascii  string
		wantTitle    string
		wantCampaign string
	}{
		{"an accented city", "Zürich", "Zuerich", "Hebcal Zürich 2026", "pdf-zuerich-2026"},
		{"a longer geonames name", "New York", "New York City",
			"Hebcal New York 2026", "pdf-new-york-city-2026"},
		{"the two agree", "Boston", "Boston", "Hebcal Boston 2026", "pdf-boston-2026"},
		// A ZIP or lat/long location has no asciiname, so the display name
		// stands in for both -- which is how "washington-dc" comes out of a
		// ZIP whose city column says only "Washington".
		{"no asciiname at all", "Washington, DC", "", "Hebcal Washington, DC 2026",
			"pdf-washington-dc-2026"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &download.Params{CityName: tt.city, CityNameAscii: tt.ascii, Opts: hebcal.CalOptions{Year: 2026}}
			if got := CalendarTitle(p, events); got != tt.wantTitle {
				t.Errorf("CalendarTitle() = %q, want %q", got, tt.wantTitle)
			}
			if got := CampaignName(p, events); got != tt.wantCampaign {
				t.Errorf("CampaignName() = %q, want %q", got, tt.wantCampaign)
			}
		})
	}
}
