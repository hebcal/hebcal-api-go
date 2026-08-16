package pdf

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-go/sedra"
)

// Link tracking defaults, matching hebcal-web's renderPdfEvent().
const (
	// utmSource is "hebcal.com" rather than "pdf" for external links:
	// appendIsraelAndTracking defaults it that way so a Sefaria page can see
	// where the visit came from.
	utmSource = "hebcal.com"
	utmMedium = "document"
)

// parshaID maps a parsha slug to its ordinal, so a /sedrot/ URL can be
// shortened to the /s/<hyear>/<id> form the website prefers. The doubled
// portions share the id of their first half and are marked with a trailing
// "d" in the short URL.
var parshaID = func() map[string]int {
	m := make(map[string]int, 62)
	for i, name := range sedra.Parshiot() {
		m[jsutil.MakeAnchor(name)] = i + 1
	}
	for _, name := range doubledParshiyot {
		slug := jsutil.MakeAnchor(name)
		first, _, _ := strings.Cut(slug, "-")
		if id, ok := m[first]; ok {
			m[slug] = id
		}
	}
	return m
}()

// doubledParshiyot are the seven pairs that can be read together.
var doubledParshiyot = []string{
	"Vayakhel-Pekudei",
	"Tazria-Metzora",
	"Achrei Mot-Kedoshim",
	"Behar-Bechukotai",
	"Chukat-Balak",
	"Matot-Masei",
	"Nitzavim-Vayeilech",
}

// doubledSlugs is the set of doubled-parsha slugs, which take the "d" suffix.
var doubledSlugs = func() map[string]bool {
	m := make(map[string]bool, len(doubledParshiyot))
	for _, name := range doubledParshiyot {
		m[jsutil.MakeAnchor(name)] = true
	}
	return m
}()

// eventLink turns the canonical URL from hebcal-go into the short, tracked
// form that appears in production PDFs, e.g.
//
//	https://www.hebcal.com/sedrot/eikev-20260808  ->  https://hebcal.com/s/5786/46?uc=pdf-palo-alto-2026-2027
//	https://www.hebcal.com/holidays/sukkot-2026   ->  https://hebcal.com/h/sukkot-2026?uc=pdf-palo-alto-2026-2027
//
// Returns "" when the event has no page, so callers can skip the annotation.
func eventLink(rawURL string, hyear int, campaign string, il bool) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	q := u.Query()

	// Only hebcal.com's own holiday, parsha and Omer pages are shortened and
	// tracked with uc=. Everything else -- the Sefaria links the daily-learning
	// series carry -- keeps its host and takes full utm_* parameters. Rewriting
	// those to hebcal.com produced links like hebcal.com/Yoma.49a, which 404.
	shortenable := u.Host == "www.hebcal.com" &&
		(strings.HasPrefix(u.Path, "/holidays/") ||
			strings.HasPrefix(u.Path, "/sedrot/") ||
			strings.HasPrefix(u.Path, "/omer/"))
	if !shortenable {
		// appendIsraelAndTracking sets i=on for any www.hebcal.com URL, not
		// only a shortenable one, but no event carries such a URL: the only
		// links that reach here are the external Sefaria and dafyomi.org pages
		// the daily-learning series produce, which take full utm_* parameters
		// and no i.
		u.RawQuery = appendParams(u.RawQuery,
			[2]string{"utm_source", utmSource},
			[2]string{"utm_medium", utmMedium},
			[2]string{"utm_campaign", campaign})
		return u.String()
	}

	u.Host = "hebcal.com"

	// Israel calendars ask every page for the Israel schedule, not just the
	// events that are Israel-only. appendIsraelAndTracking does this with
	// searchParams.set() and does it *before* shortening, which matters twice:
	// a page whose canonical URL already carries i=on -- the Israel-only
	// modern holidays, ben-gurion-day and friends -- must come out with one
	// parameter rather than two, and a parsha link spends the i on its path
	// (/s/5786i/12) instead of repeating it in the query.
	if il {
		q.Set("i", "on")
	}
	switch {
	case strings.HasPrefix(u.Path, "/sedrot/"):
		// shortenSedrotUrl consumes the parameter only when it recognised the
		// path; the fallback that merely trims /sedrot/ to /s/ leaves it.
		if shortenSedrot(u, hyear, q.Get("i") == "on") {
			q.Del("i")
		}
	case strings.HasPrefix(u.Path, "/holidays/"):
		u.Path = "/h/" + strings.TrimPrefix(u.Path, "/holidays/")
	case strings.HasPrefix(u.Path, "/omer/"):
		u.Path = "/o/" + strings.TrimPrefix(u.Path, "/omer/")
	}

	u.RawQuery = appendParams(q.Encode(), [2]string{"uc", campaign})
	return u.String()
}

// appendParams adds parameters to a query string in the order given, skipping
// empty values.
//
// net/url's Values.Encode sorts keys, so it cannot reproduce the order
// URLSearchParams produces on the Node side. The URLs resolve identically
// either way, but matching the order keeps a link-by-link comparison against
// production a clean equality rather than one that needs normalising.
func appendParams(raw string, params ...[2]string) string {
	var b strings.Builder
	b.WriteString(raw)
	for _, p := range params {
		if p[1] == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(p[0]))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p[1]))
	}
	return b.String()
}

// shortenSedrot rewrites /sedrot/<parsha>-<YYYYMMDD> to /s/<hyear>[i]/<id>[d].
// If the path does not have that shape the prefix is simply trimmed to /s/,
// which is what @hebcal/rest-api does.
//
// It reports whether the short form was produced, because that is the only
// case in which shortenSedrotUrl consumes the i=on parameter into the path.
func shortenSedrot(u *url.URL, hyear int, israel bool) bool {
	path := strings.TrimPrefix(u.Path, "/sedrot/")
	dash := strings.LastIndex(path, "-")
	if dash < 0 || len(path)-dash-1 != 8 {
		u.Path = "/s/" + path
		return false
	}
	slug := path[:dash]
	id, ok := parshaID[slug]
	if !ok {
		u.Path = "/s/" + path
		return false
	}
	p := "/s/" + strconv.Itoa(hyear)
	if israel {
		p += "i"
	}
	p += "/" + strconv.Itoa(id)
	if doubledSlugs[slug] {
		p += "d"
	}
	u.Path = p
	return true
}
