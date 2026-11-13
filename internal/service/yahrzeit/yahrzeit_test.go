package yahrzeit

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// goldenStamp is the DTSTAMP tools/yahrzeit-goldens.sh normalizes to.
var goldenStamp = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// goldenToday is the day the golden files were generated, which the calendars
// depend on: it sets the first year and what a subscription has dropped.
func goldenToday(t *testing.T) model.GregDate {
	t.Helper()
	raw, err := os.ReadFile("testdata/TODAY")
	if err != nil {
		t.Fatal(err)
	}
	day, err := time.Parse("2006-01-02", strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return model.GregDate{Year: day.Year(), Month: day.Month(), Day: day.Day()}
}

func render(t *testing.T, url, userAgent string, today model.GregDate) *Result {
	t.Helper()
	req := parseTestURL(t, url)
	cal, err := New(req, today)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cal.ICS(userAgent, icalendar.NewZoneInfo("testdata/zoneinfo"), goldenStamp)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// parseTestURL resolves a test URL into a request. A /v3/ URL's calendar is
// read from testdata/saved/<id>.json, standing in for the database row.
func parseTestURL(t *testing.T, url string) *Request {
	t.Helper()
	if id, ok := SavedID(url); ok {
		contents, err := os.ReadFile(filepath.Join("testdata", "saved", id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		path, rawQuery, _ := strings.Cut(url, "?")
		req, err := NewSavedRequest(path, rawQuery, id, contents, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	req, ok := ParseRequest(url)
	if !ok || !req.IsYahrzeit() {
		t.Fatalf("not a yahrzeit request: %s", url)
	}
	return req
}

// TestGolden renders every URL in testdata/cases.tsv and requires the bytes
// the reference implementation produced for it, UIDs included: a subscriber
// whose feed moves to this service must see the events it already has.
func TestGolden(t *testing.T) {
	today := goldenToday(t)
	raw, err := os.ReadFile("testdata/cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := strings.Split(ln, "\t")
		if len(f) != 3 {
			t.Fatalf("bad line in cases.tsv: %q", ln)
		}
		name, ua, url := f[0], f[1], f[2]
		n++
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "golden", name+".ics"))
			if err != nil {
				t.Fatal(err)
			}
			got := render(t, url, ua, today).Body
			if name == "empty" {
				// The placeholder's UID hashes the URL with a different
				// function; it changes daily anyway.
				re := regexp.MustCompile(`(UID:yahrzeit-\d{8}-)[0-9a-f]{32}(-dummy)`)
				got, want = re.ReplaceAll(got, []byte("$1$2")), re.ReplaceAll(want, []byte("$1$2"))
			}
			// The PRODID names the build, which varies.
			prodid := regexp.MustCompile(`(PRODID:-//hebcal\.com/NONSGML Hebcal Calendar v)[^/\r\n]*(//)`)
			got, want = prodid.ReplaceAll(got, []byte("$1$2")), prodid.ReplaceAll(want, []byte("$1$2"))
			if !bytes.Equal(got, want) {
				t.Errorf("output differs from golden\n%s", firstDiff(got, want))
			}
		})
	}
	if n < 30 {
		t.Errorf("only %d cases", n)
	}
}

func firstDiff(got, want []byte) string {
	g, w := strings.Split(string(got), "\r\n"), strings.Split(string(want), "\r\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return "line " + itoa(i+1) + "\n got: " + g[i] + "\nwant: " + w[i]
		}
	}
	return "got " + itoa(len(g)) + " lines, want " + itoa(len(w))
}

func itoa(n int) string { return strconv.Itoa(n) }

var today2026 = model.GregDate{Year: 2026, Month: time.November, Day: 10}

func body(t *testing.T, url, ua string) string {
	return string(render(t, url, ua, today2026).Body)
}

func unfold(s string) string { return strings.ReplaceAll(s, "\r\n ", "") }

func TestEveryLineIsFoldedAndCRLF(t *testing.T) {
	out := body(t, "/export/x.ics?v=yahrzeit&hebdate=on&n1="+strings.Repeat("%D7%A9%D6%B8%D7%81", 60)+"&y1=2001&m1=5&d1=5", "")
	if !strings.HasSuffix(out, "END:VCALENDAR\r\n") {
		t.Error("missing final CRLF")
	}
	for _, ln := range strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n") {
		if len(ln) > 75 {
			t.Errorf("line of %d octets: %q", len(ln), ln)
		}
		if strings.ContainsAny(ln, "\r\n") {
			t.Errorf("bare line break in %q", ln)
		}
	}
}

func TestRemindersAndAlarms(t *testing.T) {
	const url = "/export/x.ics?v=yahrzeit&years=2&n1=Golda+Meir&y1=1978&m1=12&d1=8&n2=Jane+Doe&t2=Birthday&y2=1990&m2=1&d2=1"
	out := body(t, url, "")
	if !strings.Contains(out, "SUMMARY:🕯️ Golda Meir Yahrzeit reminder\r\n") {
		t.Error("no reminder event")
	}
	// The reminder's alarm fires as it starts; the Yahrzeit itself has none;
	// a birthday is announced at noon the day before.
	for _, vevent := range strings.Split(out, "BEGIN:VEVENT")[1:] {
		switch {
		case strings.Contains(vevent, "Yahrzeit reminder"):
			if !strings.Contains(vevent, "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Event reminder\r\nTRIGGER:P0DT0H0M0S\r\nEND:VALARM\r\n") {
				t.Errorf("reminder alarm missing:\n%s", vevent)
			}
			if !regexp.MustCompile(`DTSTART:\d{8}T(163000|143000|200000)\r\n`).MatchString(vevent) {
				t.Errorf("reminder is not a floating afternoon time:\n%s", vevent)
			}
		case strings.Contains(vevent, "Yahrzeit"):
			if strings.Contains(vevent, "VALARM") {
				t.Errorf("Yahrzeit has an alarm:\n%s", vevent)
			}
		case strings.Contains(vevent, "Birthday"):
			if !strings.Contains(vevent, "TRIGGER:-P0DT12H0M0S") {
				t.Errorf("birthday alarm missing:\n%s", vevent)
			}
		}
	}

	off := body(t, url+"&yrem=0", "")
	if strings.Contains(off, "Yahrzeit reminder") {
		t.Error("yrem=0 kept the reminders")
	}
	if off2 := body(t, url+"&yrem=off", ""); off2 != off {
		t.Error("yrem=off differs from yrem=0")
	}
}

func TestReminderTimeFollowsTheEve(t *testing.T) {
	out := body(t, "/export/x.ics?v=yahrzeit&dl=1&years=20&n1=Golda+Meir&y1=1978&m1=12&d1=8", "")
	re := regexp.MustCompile(`DTSTART:(\d{8})T(\d{4})00`)
	seen := map[time.Weekday]string{}
	for _, m := range re.FindAllStringSubmatch(out, -1) {
		day, _ := time.Parse("20060102", m[1])
		want := "1630"
		switch day.Weekday() {
		case time.Friday:
			want = "1430" // before Shabbat
		case time.Saturday:
			want = "2000" // after it
		}
		if m[2] != want {
			t.Errorf("%s (%s): reminder at %s, want %s", m[1], day.Weekday(), m[2], want)
		}
		seen[day.Weekday()] = m[2]
	}
	if len(seen) < 4 {
		t.Errorf("only %d weekdays sampled", len(seen))
	}
}

func TestAnniversaryNumbers(t *testing.T) {
	const url = "/export/x.ics?v=yahrzeit&years=2&n1=Golda+Meir&y1=1978&m1=12&d1=8"
	if out := body(t, url, ""); !regexp.MustCompile(`SUMMARY:🕯️ Golda Meir’s \d+(st|nd|rd|th) Yahrzeit`).MatchString(out) {
		t.Error("anniversary number missing by default")
	}
	out := body(t, url+"&an=off", "")
	if !strings.Contains(out, "SUMMARY:🕯️ Golda Meir’s Yahrzeit\r\n") {
		t.Error("an=off kept the number in the title")
	}
	if !strings.Contains(unfold(out), "whose Yahrzeit occurs on") {
		t.Error("an=off kept the number in the description")
	}
}

func TestSubscriptionDropsThePast(t *testing.T) {
	const url = "/export/x.ics?v=yahrzeit&years=10&start=5780&n1=Chaim+Weizmann&y1=1952&m1=11&d1=9"
	sub := body(t, url, "")
	dl := body(t, url+"&dl=1", "")
	if n := strings.Count(dl, "UID:yahrzeit-"); n != 10 {
		t.Errorf("download has %d observances, want 10", n)
	}
	if strings.Contains(sub, "UID:yahrzeit-5781-") || !strings.Contains(dl, "UID:yahrzeit-5781-") {
		t.Error("a subscription should drop 5781 and a download keep it")
	}
	// 21 Cheshvan 5787 was 1 November 2026, within twelve weeks of today.
	if !strings.Contains(sub, "UID:yahrzeit-5787-") {
		t.Error("a subscription should keep the recent past")
	}
}

func TestVariantTracksWhatTheBodyDependsOn(t *testing.T) {
	variant := func(url, ua string, today model.GregDate) string {
		req, _ := ParseRequest(url)
		cal, err := New(req, today)
		if err != nil {
			t.Fatal(err)
		}
		return cal.Variant(ua)
	}
	const sub = "/export/x.ics?v=yahrzeit&n1=A&y1=2001&m1=5&d1=5"
	nextDay := model.GregDate{Year: 2026, Month: time.November, Day: 11}
	nextWeek := model.GregDate{Year: 2026, Month: time.November, Day: 15}
	if variant(sub, "", today2026) != variant(sub, "", nextDay) {
		t.Error("changes within a week")
	}
	if variant(sub, "", today2026) == variant(sub, "", nextWeek) {
		t.Error("a subscription should change with the week")
	}
	if variant(sub+"&dl=1", "", today2026) != variant(sub+"&dl=1", "", nextWeek) {
		t.Error("a download should not change with the week")
	}
	if variant(sub, "", today2026) == variant(sub, GoogleImporter, today2026) {
		t.Error("Google's copy should differ")
	}
	a := "/v2/y/" + "bjE9QSZ5MT0yMDAxJm0xPTUmZDE9NSZ2PXlhaHJ6ZWl0" + "/x.ics"
	b := "/v2/y/" + "bjE9QiZ5MT0yMDAxJm0xPTUmZDE9NSZ2PXlhaHJ6ZWl0" + "/x.ics"
	if variant(a, "", today2026) == variant(b, "", today2026) {
		t.Error("should differ by calendar")
	}
}

func TestInvalidDates(t *testing.T) {
	for url, want := range map[string]string{
		"/export/x.ics?v=yahrzeit&d1=31&m1=2&y1=2003":             "Gregorian day 31 out of valid range for 2/2003",
		"/export/x.ics?v=yahrzeit&d1=1&m1=13&y1=2003":             "Gregorian month out of valid range 1-12: 13",
		"/export/x.ics?v=yahrzeit&d1=x&m1=1&y1=2003":              "Gregorian day must be numeric: x",
		"/export/x.ics?v=yahrzeit&hd1=31&hm1=Nisan&hy1=5700&n1=A": "Hebrew day 31 out of valid range 1-30 for Nisan 5700",
	} {
		req, _ := ParseRequest(url)
		cal, err := New(req, today2026)
		if err != nil {
			t.Fatal(err)
		}
		_, err = cal.ICS("", nil, goldenStamp)
		if err == nil || err.Error() != want || model.StatusOf(err) != 400 {
			t.Errorf("%s: got %v, want 400 %q", url, err, want)
		}
	}
	req, _ := ParseRequest("/export/x.ics?v=yahrzeit&d1=1&m1=1&y1=2003&start=99999999")
	if _, err := New(req, today2026); err == nil || model.StatusOf(err) != 400 {
		t.Errorf("out-of-range start: got %v", err)
	}
}

func TestEmptyCalendar(t *testing.T) {
	res := render(t, "/export/x.ics?v=yahrzeit", "", today2026)
	out := string(res.Body)
	if !res.Empty {
		t.Error("Empty not set")
	}
	for _, want := range []string{
		"SUMMARY:Calendar contains no events\r\n",
		"DTSTART;VALUE=DATE:20261110\r\n",
		"https://www.hebcal.com/yahrzeit\r\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "X-PUBLISHED-TTL") || strings.Contains(out, "VALARM") {
		t.Error("placeholder calendar should carry no refresh hint and no alarm")
	}
	// The placeholder's UID is stable within a day and distinct per URL.
	uid := regexp.MustCompile(`UID:\S+`)
	again := string(render(t, "/export/x.ics?v=yahrzeit", "", today2026).Body)
	other := string(render(t, "/export/y.ics?v=yahrzeit", "", today2026).Body)
	if uid.FindString(out) != uid.FindString(again) || uid.FindString(out) == uid.FindString(other) {
		t.Errorf("placeholder UIDs: %s / %s / %s", uid.FindString(out), uid.FindString(again), uid.FindString(other))
	}
}

func TestManyEntriesAreCheap(t *testing.T) {
	// An entry number is a key, not a count: a URL naming entry 999999999
	// describes one entry.
	out := body(t, "/export/x.ics?v=yahrzeit&years=2&n999999999=Jane+Doe&y999999999=2001&m999999999=5&d999999999=5", "")
	if !strings.Contains(out, "-999999999\r\n") {
		t.Error("entry not rendered")
	}
}

func TestTextCannotInjectLines(t *testing.T) {
	out := body(t, "/export/x.ics?v=yahrzeit&years=2&dl=1&yrem=0&y1=2001&m1=5&d1=5"+
		"&n1=A%0D%0AEND:VCALENDAR%0D%0AX&title=T%0ABEGIN:VEVENT", "")
	if strings.Count(out, "\r\nEND:VCALENDAR\r\n") != 1 || strings.Count(out, "\r\nBEGIN:VEVENT\r\n") != 2 {
		t.Errorf("a name or title broke out of its line:\n%s", out[:600])
	}
	if !strings.Contains(out, `X-WR-CALNAME:T\nBEGIN:VEVENT`) || !strings.Contains(out, `SUMMARY:🕯️ A\nEND:VCALENDAR\nX’s`) {
		t.Errorf("line breaks should be escaped:\n%s", out[:600])
	}
}

func csvBody(t *testing.T, url string) string {
	t.Helper()
	req, ok := ParseRequest(url)
	if !ok || !req.IsYahrzeit() {
		t.Fatalf("not a yahrzeit request: %s", url)
	}
	cal, err := New(req, today2026)
	if err != nil {
		t.Fatal(err)
	}
	b, err := cal.CSV()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCSV(t *testing.T) {
	const url = "/export/x.csv?v=yahrzeit&years=3&start=5786&yizkor=on" +
		"&n1=Chaim+Weizmann&y1=1952&m1=11&d1=9&t1=Yahrzeit" +
		"&n2=Golda%2C+%22Meir%22&y2=1898&m2=5&d2=3&t2=Birthday"
	got := csvBody(t, url)
	const header = "\xEF\xBB\xBFSubject,Start Date,Start Time,End Date,End Time,All day event,Description,Show time as,Location\r\n"
	if !strings.HasPrefix(got, header) || !strings.HasSuffix(got, "\r\n") {
		t.Fatalf("framing: %.200q", got)
	}
	// Every observance in the range, past ones included, and no reminders.
	if n := strings.Count(got, "\nChaim Weizmann’s "); n != 3 {
		t.Errorf("%d Yahrzeit rows, want 3", n)
	}
	if strings.Contains(got, "reminder") {
		t.Error("a CSV has no reminder events")
	}
	if n := strings.Count(got, "\nYizkor ("); n != 12 {
		t.Errorf("%d Yizkor rows, want 12", n)
	}
	// 21 Cheshvan 5786 was 12 November 2025. No emoji, and the memo's
	// paragraphs are flattened onto one line.
	want := "Chaim Weizmann’s 73rd Yahrzeit,11/12/2025,,,,true," +
		"\"Hebcal joins you in remembering Chaim Weizmann, whose 73rd Yahrzeit occurs on Wednesday, November 12, " +
		"corresponding to the 21st of Cheshvan, 5786. /  / Chaim Weizmann’s Yahrzeit begins at sundown on " +
		"Tuesday, November 11 and continues until sundown on the day of observance. It is customary to light a " +
		"memorial candle at dusk as the Yahrzeit begins. /  / May your loved one’s soul be bound up in the bond of " +
		"eternal life and may their memory serve as a continued source of inspiration and comfort to you.\",3,Personal\r\n"
	if !strings.Contains(got, want) {
		t.Errorf("Yahrzeit row missing:\n%s", got)
	}
	// Commas and quotes survive, quoted.
	if !strings.Contains(got, "\"Golda, \"\"Meir\"\"’s 128th Hebrew Birthday\",") {
		t.Errorf("birthday row missing:\n%s", got)
	}
	if !strings.Contains(got, "Yizkor (Yom Kippur),10/2/2025,,,,true,,3,Personal\r\n") {
		t.Errorf("Yizkor row missing:\n%s", got)
	}

	// Any euro= value means day/month/year.
	if euro := csvBody(t, url+"&euro=0"); !strings.Contains(euro, "Yahrzeit,12/11/2025,") {
		t.Errorf("euro dates:\n%.400s", euro)
	}
	if empty := csvBody(t, "/v2/y/"+"dj15YWhyemVpdA"+"/x.csv"); empty != header {
		t.Errorf("empty calendar: %q", empty)
	}
}

// A CSV is imported once, so it never says how to edit a saved calendar, with
// or without dl=1 and for Yizkor rows as much as for entries.
func TestCSVHasNoEditMemo(t *testing.T) {
	for _, dl := range []string{"", "&dl=1"} {
		got := csvBody(t, "/export/x.csv?v=yahrzeit&years=2&start=5786&yizkor=on&ulid=01m73gh503ng2gfkfbyt3t0a38"+
			"&n1=A&y1=2001&m1=5&d1=5&t1=Yahrzeit&n2=B&y2=2001&m2=5&d2=5&t2=Birthday"+dl)
		if strings.Contains(got, "To edit") || strings.Contains(got, "yahrzeit/edit") {
			t.Errorf("edit memo in CSV (%q):\n%.600s", dl, got)
		}
	}
}

func TestCSVVariantIgnoresWeekAndClient(t *testing.T) {
	req, _ := ParseRequest("/export/x.csv?v=yahrzeit&n1=A&y1=2001&m1=5&d1=5")
	a, _ := New(req, today2026)
	b, _ := New(req, model.GregDate{Year: 2026, Month: time.November, Day: 15})
	if a.Variant("") != b.Variant(GoogleImporter) {
		t.Error("a CSV does not depend on the week or the client")
	}
}
