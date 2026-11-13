package ical

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// today is the day the goldens were generated on, and what the service's clock
// reads: it stamps every event and counts a subscription's look-back.
var today = time.Date(2026, time.November, 11, 0, 0, 0, 0, time.UTC)

// fakeReadings answers /torahMemo from the captured sidecar responses in
// testdata, narrowed to the range asked for, as readings-svc would.
type fakeReadings struct {
	rows  map[bool][]readings.TorahMemo
	calls atomic.Int32
	err   error
}

func newFakeReadings(t *testing.T) *fakeReadings {
	t.Helper()
	f := &fakeReadings{rows: map[bool][]readings.TorahMemo{}}
	for il, name := range map[bool]string{false: "diaspora", true: "israel"} {
		raw, err := os.ReadFile("testdata/torahmemo-" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var rows []readings.TorahMemo
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		f.rows[il] = rows
	}
	return f
}

func (f *fakeReadings) TorahMemos(_ context.Context, start, end time.Time, il bool) ([]readings.TorahMemo, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	lo, hi := start.Format(time.DateOnly), end.Format(time.DateOnly)
	var out []readings.TorahMemo
	for _, r := range f.rows[il] {
		if r.Date >= lo && r.Date <= hi {
			out = append(out, r)
		}
	}
	return out, nil
}

func testService(t *testing.T) (*Service, *fakeReadings) {
	t.Helper()
	db, err := geodb.New("../../handler/testdata/zips.sqlite3", "../../handler/testdata/geonames.sqlite3")
	if err != nil {
		t.Skipf("opening geo databases: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	rs := newFakeReadings(t)
	return &Service{Geo: db, Readings: rs, Clock: func() time.Time { return today }}, rs
}

// render runs a URL through the service the way the handler does.
func render(t *testing.T, s *Service, rawURL string) *Result {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.Parse(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := s.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	s.Limit(feed)
	res, err := s.Marshal(feed, nil, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// minutes masks the minute of every time that comes from the sun. noaa-go
// and @hebcal/core can land a minute apart on some days, and an event that
// moves a minute keeps its UID, so it is the same event to a subscriber.
var minutes = regexp.MustCompile(`(?m)^((?:DTSTART|DTEND);TZID=[^:]*:\d{8}T\d{2})\d{2}00\r$|^(TRIGGER;VALUE=DATE-TIME:\d{8}T\d{2})\d{2}\d{2}Z\r$`)

// prodidVersion matches the build version in the PRODID, which varies.
var prodidVersion = regexp.MustCompile(`(PRODID:-//hebcal\.com/NONSGML Hebcal Calendar v1)[^/\r\n]*(//)`)

func maskMinutes(b []byte) []byte {
	b = prodidVersion.ReplaceAll(b, []byte("$1$2"))
	return minutes.ReplaceAll(b, []byte("${1}${2}XX\r"))
}

// TestGolden renders every URL in testdata/cases.tsv and requires the bytes
// hebcal-web produced for it, UIDs and descriptions included: a subscriber
// whose feed moves to this service must see the events it already has. The one
// difference allowed is a minute in a time computed from the sun, and Rosh
// Hashana's year, which is gematriya in Hebrew here and a number there.
func TestGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rosh := regexp.MustCompile(`(?m)^(SUMMARY:.*(?:רֹאשׁ הַשָּׁנָה|ראש השנה)) .*$`)
	n := 0
	for _, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		name, rawURL, ok := strings.Cut(ln, "\t")
		if !ok {
			t.Fatalf("bad line in cases.tsv: %q", ln)
		}
		n++
		t.Run(name, func(t *testing.T) {
			s, _ := testService(t)
			want, err := os.ReadFile(filepath.Join("testdata", "golden", name+".ics"))
			if err != nil {
				t.Fatal(err)
			}
			got := render(t, s, rawURL).Body
			got = rosh.ReplaceAll(maskMinutes(got), []byte("$1 YEAR"))
			want = rosh.ReplaceAll(maskMinutes(want), []byte("$1 YEAR"))
			if string(got) != string(want) {
				t.Errorf("output differs from golden\n%s", firstDiff(got, want))
			}
		})
	}
	if n < 10 {
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

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// The Torah readings come from readings-svc, once for each Hebrew year and
// schedule however many feeds ask.
func TestTorahMemosAreCached(t *testing.T) {
	s, rs := testService(t)
	const u = "/hebcal/index.cgi/h.ics?v=1&year=2026&month=3&maj=on&s=on&subscribe=1"
	render(t, s, u)
	first := rs.calls.Load()
	if first != 1 {
		t.Fatalf("first feed made %d requests, want 1 (one Hebrew year)", first)
	}
	render(t, s, "/hebcal/index.cgi/h.ics?v=1&year=2026&month=4&maj=on&s=on&subscribe=1")
	if got := rs.calls.Load(); got != first {
		t.Errorf("a second feed in the same Hebrew year asked readings-svc again (%d requests)", got)
	}
	render(t, s, "/hebcal/index.cgi/h.ics?v=1&year=2026&month=4&maj=on&s=on&i=on&subscribe=1")
	if got := rs.calls.Load(); got != first+1 {
		t.Errorf("the Israel schedule shares the Diaspora's readings (%d requests)", got)
	}
}

// A feed with nothing that can have a reading asks readings-svc for nothing.
func TestNoReadingsNoRequest(t *testing.T) {
	s, rs := testService(t)
	render(t, s, "/hebcal/index.cgi/h.ics?v=1&year=2026&month=3&o=on&d=on&subscribe=1")
	if got := rs.calls.Load(); got != 0 {
		t.Errorf("%d requests, want 0", got)
	}
}

func TestReadingsErrors(t *testing.T) {
	const u = "/hebcal/index.cgi/h.ics?v=1&year=2026&month=3&maj=on&subscribe=1"
	parsed, _ := url.Parse(u)

	s, rs := testService(t)
	rs.err = context.DeadlineExceeded
	req, err := s.Parse(context.Background(), parsed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Generate(context.Background(), req)
	if re, ok := err.(*ReadingsError); !ok || !re.Retryable {
		t.Errorf("a failing readings-svc: err = %v, want a retryable ReadingsError", err)
	}

	s, _ = testService(t)
	s.Readings = nil
	_, err = s.Generate(context.Background(), req)
	if re, ok := err.(*ReadingsError); !ok || re.Retryable {
		t.Errorf("no readings-svc: err = %v, want a ReadingsError that is not retryable", err)
	}
}

func TestEmptyCalendarIsAPlaceholder(t *testing.T) {
	s, _ := testService(t)
	res := render(t, s, "/hebcal/index.cgi/h.ics?v=1&year=2026&subscribe=1")
	if !res.Empty {
		t.Fatal("Empty = false")
	}
	body := strings.ReplaceAll(string(res.Body), "\r\n ", "")
	for _, want := range []string{
		"SUMMARY:Calendar contains no events\r\n",
		"DTSTART;VALUE=DATE:20261111\r\n",
		"DESCRIPTION:To create a new Hebcal holiday calendar\\, visit https://www.hebcal.com/hebcal\r\n",
		"X-WR-CALNAME:Hebcal Diaspora\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
	if strings.Contains(body, "REFRESH-INTERVAL") || strings.Contains(body, "CLASS:PUBLIC") {
		t.Errorf("a placeholder asks for no refresh and is private:\n%s", body)
	}
	if !regexp.MustCompile(`UID:hebcal-20261111-[0-9a-f]{32}-dummy`).MatchString(body) {
		t.Errorf("placeholder UID:\n%s", body)
	}
}

// A subscription too long for calendar applications keeps what is recent or to
// come, and past that is cut and says so; a file to save is never cut.
func TestSubscriptionLimit(t *testing.T) {
	s, _ := testService(t)
	const big = "/hebcal/index.cgi/h.ics?v=1&year=2026&ny=10&maj=on&min=on&nx=on&mf=on&ss=on&mod=on&s=on&o=on&d=on&c=on&geonameid=5128581&b=18&M=on"
	u, _ := url.Parse(big + "&subscribe=1")
	req, err := s.Parse(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := s.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	total := feed.Events()
	if total <= MaxSubscriptionEvents {
		t.Fatalf("only %d events; the test needs more than %d", total, MaxSubscriptionEvents)
	}
	if !s.Limit(feed) {
		t.Fatal("Limit cut nothing")
	}
	if got := feed.Events(); got != MaxSubscriptionEvents+1 {
		t.Errorf("%d events after the cut, want %d and a notice", got, MaxSubscriptionEvents)
	}
	res, err := s.Marshal(feed, nil, big)
	if err != nil {
		t.Fatal(err)
	}
	body := string(res.Body)
	if !strings.Contains(body, "SUMMARY:Hebcal calendar feed truncated") {
		t.Error("no truncation notice")
	}
	if strings.Contains(body, "DTSTART;VALUE=DATE:20260101") {
		t.Error("events older than twelve weeks survived")
	}

	u, _ = url.Parse(big)
	req, _ = s.Parse(context.Background(), u)
	feed, _ = s.Generate(context.Background(), req)
	if s.Limit(feed) || feed.Events() != total {
		t.Error("a file to save was cut")
	}
	// The notice is in one language: lg=ah adds no " / " half.
	body = strings.ReplaceAll(string(render(t, s, big+"&subscribe=1&lg=ah").Body), "\r\n ", "")
	if !strings.Contains(body, "SUMMARY:Hebcal calendar feed truncated\r\n") {
		t.Error("lg=ah: the truncation notice should have its title alone")
	}
}

func TestWeek(t *testing.T) {
	s, _ := testService(t)
	// 2026-11-11 is a Wednesday
	if got, want := s.Week(), hdate.FromTime(time.Date(2026, time.November, 8, 0, 0, 0, 0, time.UTC)).Abs(); got != want {
		t.Errorf("Week() = %d, want the Sunday's %d", got, want)
	}
}

func TestProdidLang(t *testing.T) {
	for locale, want := range map[string]string{
		"en": "en", "he": "he", "fr": "fr", "he-x-NoNikud": "he", "ashkenazi": "en",
		"ashkenazi_romanian": "ro", "s": "en",
	} {
		if got := ProdidLang(locale); got != want {
			t.Errorf("ProdidLang(%q) = %q, want %q", locale, got, want)
		}
	}
}

// Counting in Hebrew months lays out a printed page; a feed has no months, so
// a request that asks for it is the same feed as one that does not -- with
// Hebrew dates, never Gregorian ones, and no extra Tishrei.
func TestHebrewMonthModeIsIgnored(t *testing.T) {
	s, _ := testService(t)
	const base = "/hebcal/index.cgi/h.ics?v=1&start=2026-11-10&end=2026-12-10&maj=on&nx=on&D=on"
	want := render(t, s, base).Body
	for _, mm := range []string{"1", "2"} {
		got := render(t, s, base+"&mm="+mm).Body
		if string(got) != string(want) {
			t.Errorf("mm=%s renders differently from mm=0", mm)
		}
	}
	if regexp.MustCompile(`SUMMARY:(Nov|Dec) \d`).Match(want) {
		t.Error("a Gregorian date became an event")
	}
	if !strings.Contains(string(want), "Kislev") {
		t.Error("the Hebrew dates are missing")
	}
}

// The Omer's location alarm is sunset less half an hour on a Friday, 8.5° on
// a Saturday and civil dusk otherwise. In Stockholm in early June the sun
// never sinks 8.5°, so the 45th day (counted from Saturday night 5 June 2027)
// falls back to tzeit at 7.083° -- still an absolute time, but without the
// location's name -- while the 46th keeps its dusk alarm and LOCATION.
func TestOmerAlarmFallsBackToTzeit(t *testing.T) {
	s, _ := testService(t)
	res := render(t, s, "/hebcal/index.cgi/h.ics?v=1&o=on&c=on&start=2027-06-06&end=2027-06-07"+
		"&geo=pos&latitude=59.32938&longitude=18.06871&tzid=Europe/Stockholm")
	body := strings.ReplaceAll(string(res.Body), "\r\n ", "")
	events := map[string]string{}
	for _, ev := range strings.Split(body, "BEGIN:VEVENT")[1:] {
		if m := regexp.MustCompile(`SUMMARY:(\d+)th day of the Omer`).FindStringSubmatch(ev); m != nil {
			events[m[1]] = ev
		}
	}
	day45, day46 := events["45"], events["46"]
	if !regexp.MustCompile(`TRIGGER;VALUE=DATE-TIME:20270605T2[01]\d{4}Z`).MatchString(day45) ||
		strings.Contains(day45, "LOCATION:") {
		t.Errorf("45th day, want a 7.083° alarm and no LOCATION:\n%s", day45)
	}
	if !regexp.MustCompile(`TRIGGER;VALUE=DATE-TIME:20270606T\d{6}Z`).MatchString(day46) ||
		!strings.Contains(day46, "LOCATION:") {
		t.Errorf("46th day, want a dusk alarm and a LOCATION:\n%s", day46)
	}
}

// The campaign is the title's slug, and the title's dates span every event --
// the readings-svc rows too, which are the whole of a Chofetz Chaim calendar.
func TestCampaignCountsFetchedLearning(t *testing.T) {
	s, _ := testService(t)
	u, _ := url.Parse("/hebcal/index.cgi/h.ics?v=1&start=2026-08-01&end=2027-12-31&dcc=on")
	req, err := s.Parse(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	items := []readings.Item{
		{Date: "2026-08-01", Title: "Hilchos Rechilus 2.3-2.4", Category: "chofetzChaim"},
		{Date: "2027-12-31", Title: "Hilchos Rechilus 2.5", Category: "chofetzChaim"},
	}
	if got, want := campaignName(req.Params, req.Query, nil, items), "diaspora-2026-2027"; got != want {
		t.Errorf("campaignName = %q, want %q", got, want)
	}
}
