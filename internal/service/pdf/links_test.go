package pdf

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-go/hebcal"
)

// eventLink turns the canonical URL from hebcal-go into the short, tracked
// form production embeds. These cases are taken from a real Palo Alto
// calendar, where the Go and Node link sets were compared annotation by
// annotation.
func TestEventLink(t *testing.T) {
	const campaign = "pdf-palo-alto-2026-2027"
	tests := []struct {
		name  string
		raw   string
		hyear int
		il    bool
		want  string
	}{
		{
			name:  "parsha shortens to /s/<hyear>/<id>",
			raw:   "https://www.hebcal.com/sedrot/eikev-20260808",
			hyear: 5786,
			want:  "https://hebcal.com/s/5786/46?uc=" + campaign,
		},
		{
			name:  "holiday shortens to /h/",
			raw:   "https://www.hebcal.com/holidays/rosh-chodesh-elul-2026",
			hyear: 5786,
			want:  "https://hebcal.com/h/rosh-chodesh-elul-2026?uc=" + campaign,
		},
		{
			name:  "omer shortens to /o/",
			raw:   "https://www.hebcal.com/omer/5787/10",
			hyear: 5787,
			want:  "https://hebcal.com/o/5787/10?uc=" + campaign,
		},
		{
			name:  "an event with no page yields no link",
			raw:   "",
			hyear: 5786,
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventLink(tt.raw, tt.hyear, campaign, tt.il); got != tt.want {
				t.Errorf("eventLink() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A doubled portion shares the id of its first half and takes a trailing "d",
// which is how hebcal.com distinguishes "Vayakhel-Pekudei" from "Vayakhel".
func TestDoubledParshaShortensWithSuffix(t *testing.T) {
	single := eventLink("https://www.hebcal.com/sedrot/vayakhel-20260314", 5786, "", false)
	doubled := eventLink("https://www.hebcal.com/sedrot/vayakhel-pekudei-20260314", 5786, "", false)
	if !strings.HasSuffix(single, "/22") {
		t.Errorf("single parsha = %q, want it to end in /22", single)
	}
	if !strings.HasSuffix(doubled, "/22d") {
		t.Errorf("doubled parsha = %q, want it to end in /22d", doubled)
	}
}

// An Israel calendar asks every page for the Israel schedule, so i=on is added
// even for events that are not themselves Israel-only.
func TestIsraelCalendarAddsIOn(t *testing.T) {
	got := eventLink("https://www.hebcal.com/holidays/sukkot-2026", 5787, "", true)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("i") != "on" {
		t.Errorf("eventLink() = %q, want i=on", got)
	}
}

// An Israel-only observance already carries ?i=on from hebcal-go. Shortening
// must not drop it or duplicate it.
func TestIsraelOnlyEventKeepsSingleIOn(t *testing.T) {
	got := eventLink("https://www.hebcal.com/holidays/yom-haatzmaut-2026?i=on", 5786, "", false)
	if strings.Count(got, "i=on") != 1 {
		t.Errorf("eventLink() = %q, want exactly one i=on", got)
	}
}

// A path that does not match <parsha>-<YYYYMMDD> keeps its name rather than
// being mapped onto a wrong portion number.
func TestUnrecognizedSedrotPathIsNotGuessed(t *testing.T) {
	got := eventLink("https://www.hebcal.com/sedrot/not-a-parsha", 5786, "", false)
	if !strings.Contains(got, "/s/not-a-parsha") {
		t.Errorf("eventLink() = %q, want the path preserved under /s/", got)
	}
}

func TestUrlSlug(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Rosh Chodesh Elul", "rosh-chodesh-elul"},
		{"Yom HaAtzma'ut", "yom-haatzmaut"},
		// Only the straight apostrophe is deleted. makeAnchor's character
		// class is JavaScript's `\w` without the `u` flag, so the typographic
		// one is not a word character and becomes a hyphen like any other
		// punctuation (measured against @hebcal/rest-api: "Ta’anit Bechorot"
		// gives "ta-anit-bechorot"). Nothing reaches this function with one --
		// a holiday's slug comes from its event URL, not from here -- but the
		// two apostrophes really do behave differently.
		{"Ta’anit Bechorot", "ta-anit-bechorot"},
		{"Palo Alto", "palo-alto"},
		// A location, which is what campaignFromTitle actually feeds this:
		// punctuation collapses to single hyphens and the ends are trimmed,
		// rather than surviving into the campaign to be percent-encoded.
		{"Washington, D.C", "washington-d-c"},
		{"St. Louis", "st-louis"},
		{"GB-London", "gb-london"},
		// The degrees/minutes name a legacy /v2/ ladeg location produces.
		// Underscore is a word character and survives.
		{"40°42′N 74°0′W America/New_York 2026", "40-42-n-74-0-w-america-new_york-2026"},
	}
	for _, tt := range tests {
		if got := urlSlug(tt.in); got != tt.want {
			t.Errorf("urlSlug(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The campaign ties a link back to the calendar that produced it. It is the
// second half of campaignName(); the first half, which decides whether the
// location is named by its display name or its asciiname, is CampaignName.
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

// External links keep their host and take full utm_* parameters; only
// hebcal.com's own holiday, parsha and Omer pages are shortened and given uc=.
// Rewriting a Sefaria link to hebcal.com produced URLs that 404.
func TestExternalLinksKeepTheirHost(t *testing.T) {
	got := eventLink("https://www.sefaria.org/Yoma.49a?lang=bi", 5786, "pdf-diaspora-august-2026", false)
	if !strings.HasPrefix(got, "https://www.sefaria.org/Yoma.49a?") {
		t.Fatalf("eventLink() = %q, want the Sefaria host preserved", got)
	}
	for _, want := range []string{"lang=bi", "utm_source=hebcal.com", "utm_medium=document", "utm_campaign=pdf-diaspora-august-2026"} {
		if !strings.Contains(got, want) {
			t.Errorf("eventLink() = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "uc=") {
		t.Errorf("eventLink() = %q, external links use utm_campaign, not uc=", got)
	}
}

// Every regular portion must map to an id, or its links would fall back to the
// long form. sedra.Parshiot() has 53 entries: V'Zot HaBerachah is read on
// Simchat Torah rather than on a Shabbat, so it is not in the weekly cycle.
func TestEveryParshaHasAnID(t *testing.T) {
	if len(parshaID) < 53 {
		t.Fatalf("parshaID has %d entries, want at least 53", len(parshaID))
	}
	for _, name := range []string{"bereshit", "noach", "haazinu"} {
		if _, ok := parshaID[name]; !ok {
			t.Errorf("parshaID is missing %q", name)
		}
	}
}

// A doubled portion whose first half is a single word shortens to that half's
// id with a "d" suffix.
func TestDoubledPortionsWithSingleWordFirstHalf(t *testing.T) {
	for _, name := range []string{"Tazria-Metzora", "Chukat-Balak", "Matot-Masei"} {
		slug := urlSlug(name)
		if _, ok := parshaID[slug]; !ok {
			t.Errorf("parshaID is missing %q", slug)
		}
		if !doubledSlugs[slug] {
			t.Errorf("doubledSlugs is missing %q", slug)
		}
	}
}

// "Achrei Mot-Kedoshim" is the one doubled portion whose first half is itself
// two words, so keying it by the text before the first hyphen looks up
// "achrei" and finds nothing. @hebcal/rest-api's shortenSedrotUrl has the same
// quirk -- it does anchor.split('-')[0] -- and falls back to the long form, so
// matching production means falling back here too rather than "fixing" it into
// a divergence.
func TestAchreiMotKedoshimFallsBackToTheLongForm(t *testing.T) {
	got := eventLink("https://www.hebcal.com/sedrot/achrei-mot-kedoshim-20270501", 5787, "", false)
	if !strings.Contains(got, "/s/achrei-mot-kedoshim-20270501") {
		t.Errorf("eventLink() = %q, want the long form under /s/", got)
	}
	// The undoubled portion still shortens, which is what makes the fallback
	// specific to this one name rather than a general failure.
	single := eventLink("https://www.hebcal.com/sedrot/achrei-mot-20270501", 5787, "", false)
	if !strings.HasSuffix(single, "/29") {
		t.Errorf("single Achrei Mot = %q, want it to end in /29", single)
	}
}

// appendIsraelAndTracking sets i=on with searchParams.set(), and does it before
// shortening. Both halves matter, and getting either wrong put a wrong link on
// every event of every Israel calendar -- 56 of 103 on a Jerusalem 2026
// download, measured against hebcal-web.
func TestIsraelParameterIsSetNotAppended(t *testing.T) {
	const campaign = "pdf-jerusalem-2026"

	// The Israel-only modern holidays already carry i=on in the URL hebcal-go
	// hands over, so appending a second one produced ...?i=on&i=on.
	t.Run("a page that already asks for Israel", func(t *testing.T) {
		got := eventLink("https://www.hebcal.com/holidays/ben-gurion-day-2026?i=on",
			5786, campaign, true)
		want := "https://hebcal.com/h/ben-gurion-day-2026?i=on&uc=" + campaign
		if got != want {
			t.Errorf("eventLink() = %q, want %q", got, want)
		}
	})

	t.Run("a page that does not", func(t *testing.T) {
		got := eventLink("https://www.hebcal.com/holidays/sukkot-2026", 5787, campaign, true)
		want := "https://hebcal.com/h/sukkot-2026?i=on&uc=" + campaign
		if got != want {
			t.Errorf("eventLink() = %q, want %q", got, want)
		}
	})

	// A parsha link spends the i on its path -- /s/5786i/12 -- and
	// shortenSedrotUrl deletes the parameter as it does so, rather than
	// carrying both.
	t.Run("a parsha spends it on the path", func(t *testing.T) {
		got := eventLink("https://www.hebcal.com/sedrot/vayechi-20260103?i=on",
			5786, campaign, true)
		want := "https://hebcal.com/s/5786i/12?uc=" + campaign
		if got != want {
			t.Errorf("eventLink() = %q, want %q", got, want)
		}
	})

	// The fallback that only trims /sedrot/ to /s/ has no path to spend it on,
	// so there the parameter survives into the query.
	t.Run("the long-form fallback keeps it in the query", func(t *testing.T) {
		got := eventLink("https://www.hebcal.com/sedrot/achrei-mot-kedoshim-20270501",
			5787, campaign, true)
		if !strings.Contains(got, "/s/achrei-mot-kedoshim-20270501") ||
			!strings.Contains(got, "i=on") {
			t.Errorf("eventLink() = %q, want the long form with i=on", got)
		}
	})

	// A diaspora calendar adds nothing, and does not strip an i=on the page
	// came with either -- appendIsraelAndTracking only ever sets.
	t.Run("a diaspora calendar", func(t *testing.T) {
		got := eventLink("https://www.hebcal.com/holidays/sukkot-2026", 5787,
			"pdf-boston-2026", false)
		if strings.Contains(got, "i=on") {
			t.Errorf("eventLink() = %q, want no i=on on a diaspora calendar", got)
		}
	})
}

// campaignName() builds its title with preferAsciiName, so the campaign names
// the location by its raw geonames asciiname wherever there is one, while the
// document title uses the display short name. Measured against hebcal-web:
// geonameid 2657896 renders "Hebcal Zürich 2026" and tags every link
// pdf-zuerich-2026, and 5128581 renders "Hebcal New York 2026" and tags
// pdf-new-york-city-2026. Deriving the campaign from the document title gave
// "pdf-z-rich-2026", which matched nothing production had ever emitted.
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
			p := &Params{CityName: tt.city, CityNameAscii: tt.ascii, Opts: hebcal.CalOptions{Year: 2026}}
			if got := CalendarTitle(p, events); got != tt.wantTitle {
				t.Errorf("CalendarTitle() = %q, want %q", got, tt.wantTitle)
			}
			if got := CampaignName(p, events); got != tt.wantCampaign {
				t.Errorf("CampaignName() = %q, want %q", got, tt.wantCampaign)
			}
		})
	}
}
