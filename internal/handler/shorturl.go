package handler

// The short URLs www.hebcal.com prints into PDF calendars, iCalendar feeds and
// emails: /h/<holiday> for /holidays/, /o/<page> for /omer/, and
// /s/<year>/<num> for a Torah portion read in that Hebrew year. Each answers
// with a 302 to the long URL, with the short tracking parameters (us, um, uc)
// expanded into their utm_* spellings.

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/sedra"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
)

const sedrotBaseURL = "https://www.hebcal.com/sedrot/"

// numParshiot counts the 54 portions of the Torah, V'Zot HaBerachah included.
// A short URL may name any of them, but V'Zot HaBerachah is read on Simchat
// Torah rather than on a Shabbat, so it is never found in a year's schedule.
const numParshiot = 54

var shortToLong = map[byte]string{
	'h': "holidays",
	's': "sedrot",
	'o': "omer",
}

// shortURL implements GET/HEAD /h/, /s/ and /o/.
func (s *Server) shortURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.MethodNotAllowed(w, r.Method, "GET, HEAD")
		return
	}
	rpath := r.URL.EscapedPath()
	dest, ok := shortToLong[rpath[1]]
	if !ok || len(rpath) < 3 {
		httpx.WriteNotFoundText(w)
		return
	}
	base := rpath[3:]
	qs := shortURLQuery(r)
	destURL := ""
	if dest == "sedrot" {
		u, err := shortParshaURL(base, qs)
		if err != nil {
			httpx.WritePlainError(w, err)
			return
		}
		destURL = u
	}
	if destURL == "" {
		destURL = "https://www.hebcal.com/" + dest + "/" + base + "?" + qs.encode()
	}
	w.Header().Set("Cache-Control", httpx.CacheControl1Day)
	w.Header().Set("Location", destURL)
	w.Header().Set("Content-Type", httpx.ContentTypeText)
	w.WriteHeader(http.StatusFound)
	fmt.Fprintf(w, "Redirecting to %s\n", destURL)
}

// shortURLQuery rewrites the request's query string for the long URL. A uc
// campaign becomes utm_campaign and, for a pdf- or ical- campaign, implies the
// source and medium of that kind of link. Otherwise the source falls back to
// the referring site, then to "redir", as the medium does.
func shortURLQuery(r *http.Request) *orderedQuery {
	qs := parseOrderedQuery(r.URL.RawQuery)
	utmSource := firstNonEmpty(qs.get("utm_source"), qs.get("us"))
	utmMedium := firstNonEmpty(qs.get("utm_medium"), qs.get("um"))
	if qs.has("uc") {
		campaign := qs.get("uc")
		if strings.HasPrefix(campaign, "pdf-") {
			utmSource = firstNonEmpty(utmSource, "pdf")
			utmMedium = firstNonEmpty(utmMedium, "document")
		} else if strings.HasPrefix(campaign, "ical-") {
			utmSource = firstNonEmpty(utmSource, "js")
			utmMedium = firstNonEmpty(utmMedium, "icalendar")
		}
		qs.set("utm_campaign", campaign)
		qs.del("uc")
	}
	if utmSource == "" {
		utmSource = utmSourceFromRef(r.Header.Get("Referer"))
	}
	qs.set("utm_source", firstNonEmpty(utmSource, "redir"))
	qs.set("utm_medium", firstNonEmpty(utmMedium, "redir"))
	qs.del("us")
	qs.del("um")
	return qs
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// utmSourceFromRef names the site a visitor followed the link from: the
// referrer's hostname without a leading "www.", or "" for hebcal.com itself,
// a local host, or a referrer that is not an absolute URL.
func utmSourceFromRef(ref string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	hostname := strings.ToLower(u.Hostname())
	if hostname == "hebcal.com" || strings.HasSuffix(hostname, ".hebcal.com") ||
		hostname == "localhost" || hostname == "127.0.0.1" {
		return ""
	}
	return strings.TrimPrefix(hostname, "www.")
}

// shortParshaURL resolves /s/<year>/<num> to the portion's page for the date
// it is read that year. <year> ends in "i" for the Israel schedule, and <num>
// (Bereshit is 1) ends in "d" for a doubled portion, numbered by its first
// half. An empty base goes to the portion index. A base that does not start
// with a digit is not a portion number, and yields "" so the caller redirects
// it like any other short URL.
func shortParshaURL(str string, qs *orderedQuery) (string, error) {
	if str == "" {
		return sedrotBaseURL, nil
	}
	if str[0] < '0' || str[0] > '9' {
		return "", nil
	}
	// Trailing slashes name the same file, as they do to a basename/dirname.
	trimmed := strings.TrimRight(str, "/")
	parshaStr := path.Base(trimmed)
	num, ok := jsutil.ParseInt(parshaStr)
	parshaID := num - 1
	if !ok || parshaID < 0 || parshaID > numParshiot-1 {
		return "", model.BadRequest("invalid short redirect parsha: %s", parshaStr)
	}
	doubled := strings.HasSuffix(parshaStr, "d")
	if doubled && !isValidDouble(parshaID) {
		return "", model.BadRequest("invalid short redirect double parsha: %s", parshaStr)
	}
	yearStr := path.Dir(trimmed)
	year, ok := jsutil.ParseInt(yearStr)
	if !ok || year == 0 {
		return "", model.BadRequest("invalid short redirect parsha: %s", str)
	}
	if !model.YearIsSupported(year, true) {
		return "", &model.HTTPError{Status: http.StatusGone,
			Message: "short redirect year out of range: " + yearStr}
	}
	il := strings.HasSuffix(yearStr, "i")
	sched := sedra.New(year, il)
	// FindParshaNum takes a 1-based number; a doubled reading is the negated
	// index of its first half.
	findNum := num
	if doubled {
		findNum = 1 - parshaID
	}
	hd, err := sched.FindParshaNum(findNum)
	if err != nil {
		return "", model.NotFound("short redirect not found: %s", str)
	}
	parshiot := sedra.Parshiot()
	name := parshiot[parshaID]
	if doubled {
		name += "-" + parshiot[parshaID+1]
	}
	if il {
		qs.set("i", "on")
	}
	return sedrotBaseURL + jsutil.MakeAnchor(name) + "-" + yyyymmdd(hd) + "?" + qs.encode(), nil
}

// isValidDouble reports whether the 0-based parsha index begins one of the
// seven portions that can be read together with the next.
func isValidDouble(id int) bool {
	switch id {
	case 21, // Vayakhel-Pekudei
		26, // Tazria-Metzora
		28, // Achrei Mot-Kedoshim
		31, // Behar-Bechukotai
		38, // Chukat-Balak
		41, // Matot-Masei
		50: // Nitzavim-Vayeilech
		return true
	}
	return false
}

// yyyymmdd formats the proleptic Gregorian date of hd, zero-padding the year
// to four digits.
func yyyymmdd(hd hdate.HDate) string {
	y, m, d := hd.ProlepticGreg()
	return fmt.Sprintf("%04d%02d%02d", y, int(m), d)
}

// orderedQuery is a query string that keeps its parameters in the order they
// arrived, so the long URL repeats them as the visitor sent them. A key given
// more than once keeps its first position with its values comma-joined.
type orderedQuery struct {
	keys []string
	vals map[string]string
}

func parseOrderedQuery(raw string) *orderedQuery {
	q := &orderedQuery{vals: map[string]string{}}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		k, v = unescapeQuery(k), unescapeQuery(v)
		if prev, ok := q.vals[k]; ok {
			q.vals[k] = prev + "," + v
			continue
		}
		q.keys = append(q.keys, k)
		q.vals[k] = v
	}
	return q
}

// unescapeQuery decodes a form-encoded component, leaving it as it was when
// its percent-escapes are malformed.
func unescapeQuery(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return strings.ReplaceAll(s, "+", " ")
}

func (q *orderedQuery) has(k string) bool {
	_, ok := q.vals[k]
	return ok
}

func (q *orderedQuery) get(k string) string {
	return q.vals[k]
}

// set replaces k's value in place, or appends k when it is absent.
func (q *orderedQuery) set(k, v string) {
	if !q.has(k) {
		q.keys = append(q.keys, k)
	}
	q.vals[k] = v
}

func (q *orderedQuery) del(k string) {
	if !q.has(k) {
		return
	}
	delete(q.vals, k)
	for i, key := range q.keys {
		if key == k {
			q.keys = append(q.keys[:i], q.keys[i+1:]...)
			break
		}
	}
}

// encode serializes the query as application/x-www-form-urlencoded, which
// leaves only ASCII alphanumerics and "*-._" unescaped and writes a space as
// "+".
func (q *orderedQuery) encode() string {
	var sb strings.Builder
	for i, k := range q.keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		formEncode(&sb, k)
		sb.WriteByte('=')
		formEncode(&sb, q.vals[k])
	}
	return sb.String()
}

func formEncode(sb *strings.Builder, s string) {
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '*', c == '-', c == '.', c == '_':
			sb.WriteByte(c)
		case c == ' ':
			sb.WriteByte('+')
		default:
			sb.WriteByte('%')
			sb.WriteByte(hex[c>>4])
			sb.WriteByte(hex[c&15])
		}
	}
}
