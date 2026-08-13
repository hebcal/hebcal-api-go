package pdf

import (
	"net/url"
	"strings"
	"testing"
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
		{"Ta’anit Bechorot", "taanit-bechorot"},
		{"Palo Alto", "palo-alto"},
	}
	for _, tt := range tests {
		if got := urlSlug(tt.in); got != tt.want {
			t.Errorf("urlSlug(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The campaign ties a link back to the calendar that produced it, and is
// derived from the document title so the two cannot drift apart.
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
