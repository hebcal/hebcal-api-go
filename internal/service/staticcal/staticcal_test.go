package staticcal

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
)

func readGzip(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// stubReadings answers from the responses tools/static-ical-goldens.mjs
// captured, narrowed to the range asked for.
type stubReadings struct {
	memos    map[bool][]readings.TorahMemo
	learning []readings.Item
}

func newStubReadings(t *testing.T) *stubReadings {
	t.Helper()
	s := &stubReadings{memos: map[bool][]readings.TorahMemo{}}
	for il, name := range map[bool]string{false: "torahMemo-diaspora", true: "torahMemo-israel"} {
		var rows []readings.TorahMemo
		if err := json.Unmarshal(readGzip(t, "testdata/readings/"+name+".json.gz"), &rows); err != nil {
			t.Fatal(err)
		}
		s.memos[il] = rows
	}
	if err := json.Unmarshal(readGzip(t, "testdata/readings/learning.json.gz"), &s.learning); err != nil {
		t.Fatal(err)
	}
	return s
}

func inRange(date string, start, end time.Time) bool {
	return date >= start.Format(time.DateOnly) && date <= end.Format(time.DateOnly)
}

func (s *stubReadings) TorahMemos(_ context.Context, start, end time.Time, il bool) ([]readings.TorahMemo, error) {
	var out []readings.TorahMemo
	for _, r := range s.memos[il] {
		if inRange(r.Date, start, end) {
			out = append(out, r)
		}
	}
	return out, nil
}

// learningCodes are the series the learning fixture holds.
var learningCodes = map[string]string{"ddh": "dirshuDafHalacha", "dcc": "chofetzChaim", "dshl": "shemiratHaLashon"}

func (s *stubReadings) Learning(_ context.Context, codes []string, _ string, start, end time.Time) ([]readings.Item, error) {
	want := map[string]bool{}
	for _, c := range codes {
		series, ok := learningCodes[c]
		if !ok {
			return nil, errors.New("learning fixture has no series " + c)
		}
		want[series] = true
	}
	var out []readings.Item
	for _, it := range s.learning {
		if want[it.Category] && inRange(it.Date, start, end) {
			out = append(out, it)
		}
	}
	return out, nil
}

func testToday(t *testing.T) time.Time {
	t.Helper()
	b, err := os.ReadFile("testdata/TODAY")
	if err != nil {
		t.Fatal(err)
	}
	d, err := time.Parse(time.DateOnly, strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return d.Add(12 * time.Hour)
}

func feedByName(t *testing.T, name string) *Feed {
	t.Helper()
	for i := range Feeds {
		if Feeds[i].File == name {
			return &Feeds[i]
		}
	}
	t.Fatalf("no feed %q", name)
	return nil
}

// TestGolden renders the feeds hebcal-web's static calendars were captured
// from and requires the same bytes, UIDs included: a subscriber's calendar
// matches events by UID, and a changed one duplicates every event. The .csv
// is compared field by field, after undoing the two ways production writes it
// differently: commas deleted from the subject, and commas and double quotes
// replaced in the description.
func TestGolden(t *testing.T) {
	rs := newStubReadings(t)
	today := testToday(t)
	entries, err := os.ReadDir("testdata/golden")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".ics.gz")
		if !ok {
			continue
		}
		n++
		t.Run(name, func(t *testing.T) {
			files, err := Render(context.Background(), feedByName(t, name), today, rs)
			if err != nil {
				t.Fatal(err)
			}
			want := readGzip(t, "testdata/golden/"+name+".ics.gz")
			if !bytes.Equal(files.ICS, want) {
				t.Errorf(".ics differs from the golden:\n%s", firstDiff(string(want), string(files.ICS)))
			}
			compareCSV(t, readGzip(t, "testdata/golden/"+name+".csv.gz"), files.CSV)
		})
	}
	if n < 10 {
		t.Fatalf("found %d goldens", n)
	}
}

func compareCSV(t *testing.T, want, got []byte) {
	t.Helper()
	parse := func(b []byte) [][]string {
		r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte("\xEF\xBB\xBF"))))
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	w, g := parse(want), parse(got)
	if len(w) != len(g) {
		t.Fatalf(".csv has %d rows, want %d", len(g), len(w))
	}
	for i := range w {
		row := append([]string(nil), g[i]...)
		if len(row) == 9 {
			row[0] = strings.ReplaceAll(row[0], ",", "")
			row[6] = strings.ReplaceAll(strings.ReplaceAll(row[6], ",", ";"), `"`, "''")
		}
		if strings.Join(row, "\x00") != strings.Join(w[i], "\x00") {
			t.Fatalf(".csv row %d = %q, want %q", i, row, w[i])
		}
	}
}

// firstDiff shows the first line that differs, unfolded.
func firstDiff(want, got string) string {
	unfold := func(s string) []string {
		return strings.Split(strings.ReplaceAll(s, "\r\n ", ""), "\r\n")
	}
	w, g := unfold(want), unfold(got)
	for i := range min(len(w), len(g)) {
		if w[i] != g[i] {
			return "want: " + w[i] + "\n got: " + g[i]
		}
	}
	return "lengths differ"
}

// The window runs from 90 days back to the years ahead less 60 days, whole
// days counted from today's local date.
func TestWindow(t *testing.T) {
	start, end := Window(time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC), 2)
	if got := start.Gregorian().Format(time.DateOnly); got != "2026-08-13" {
		t.Errorf("start %s, want 2026-08-13", got)
	}
	// 2 * 365.25 - 60 = 670.5 days, rounded to 671
	if got := end.Gregorian().Format(time.DateOnly); got != "2028-09-12" {
		t.Errorf("end %s, want 2028-09-12", got)
	}
}

// A feed whose series readings-svc does not answer for -- an older sidecar,
// or a learning cycle that has ended -- is an error, not an empty file.
func TestSeriesWithNoRows(t *testing.T) {
	rs := newStubReadings(t)
	rs.learning = nil
	if _, err := Render(context.Background(), feedByName(t, "dirshu-daf-halacha"), testToday(t), rs); err == nil {
		t.Fatal("rendered a feed with no events")
	}
}

func TestKindness(t *testing.T) {
	files, err := Kindness(time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if files.Events != 365 {
		t.Errorf("%d events, want 365", files.Events)
	}
	ics := strings.ReplaceAll(string(files.ICS), "\r\n ", "")
	for _, want := range []string{
		"X-WR-CALNAME:Kindness a Day\r\n",
		// the first entry, its UID as hebcal-web made it
		"SUMMARY:Create a WhatsApp group to share daily chesed Ideas\r\n",
		"UID:hebcal-20260101-2d0a8e72\r\n",
		`DESCRIPTION:One mitzvah leads to another\n\nEthics of the Fathers 4:2` + "\r\n",
		"RRULE:FREQ=YEARLY;INTERVAL=1;BYMONTH=1\r\n",
		"TRIGGER:P0DT9H0M0S\r\n",
		"CATEGORIES:Kindness a Day\r\n",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf(".ics is missing %q", want)
		}
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(files.CSV, []byte("\xEF\xBB\xBF")))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1+2*365 || rows[366][1] != "1/1/2027" {
		t.Errorf(".csv has %d rows, row 366 %q; want this year and next", len(rows), rows[366])
	}
}
