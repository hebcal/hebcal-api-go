package staticcal

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	csvsvc "github.com/hebcal/hebcal-api-go/internal/service/csv"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// KindnessFile is the Kindness a Day feed's file name, without extension.
const KindnessFile = "kindness"

// kindnessJSON maps each Gregorian "MM-DD" to that day's act of kindness and
// its quote, from www.areyvut.org.
//
//go:embed kindness.json
var kindnessJSON []byte

var whitespace = regexp.MustCompile(`\s+`)

// kindnessDay is one entry of the Kindness a Day list.
type kindnessDay struct {
	month time.Month
	day   int
	act   string
	quote string
}

func kindnessDays() ([]kindnessDay, error) {
	var m map[string][2]string
	if err := json.Unmarshal(kindnessJSON, &m); err != nil {
		return nil, fmt.Errorf("kindness.json: %w", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // "MM-DD" sorts by date
	days := make([]kindnessDay, 0, len(keys))
	for _, k := range keys {
		ms, ds, ok := strings.Cut(k, "-")
		month, err1 := strconv.Atoi(ms)
		day, err2 := strconv.Atoi(ds)
		if !ok || err1 != nil || err2 != nil {
			return nil, fmt.Errorf("kindness.json: bad key %q", k)
		}
		v := m[k]
		days = append(days, kindnessDay{time.Month(month), day, cleanKindness(v[0]), cleanKindness(v[1])})
	}
	return days, nil
}

// cleanKindness trims an entry, drops a final full stop and collapses runs of
// whitespace. A quote's paragraph breaks are the two characters `\n`, which
// the iCalendar text format reads as a line break.
func cleanKindness(s string) string {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	return strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
}

// Kindness builds the Kindness a Day feed: one event a day for today's
// Gregorian year, each repeating yearly, with a 9am reminder. The .csv, which
// cannot repeat an event, lists this year and next.
func Kindness(today time.Time) (*Files, error) {
	days, err := kindnessDays()
	if err != nil {
		return nil, err
	}
	const title = "Kindness a Day"
	year := today.Year()
	vevents := make([]icalendar.Event, len(days))
	for i, d := range days {
		date := kindnessDate(year, d)
		vevents[i] = icalendar.Event{
			UID:         icalendar.DefaultUID(date, d.act, ""),
			Summary:     d.act,
			Date:        date,
			Category:    title,
			Description: d.quote,
			RRule:       "FREQ=YEARLY;INTERVAL=1;BYMONTH=" + strconv.Itoa(int(d.month)),
			Alarm:       icalendar.RelativeAlarm("P0DT9H0M0S"),
		}
	}
	cal := &icalendar.Calendar{
		Title:        title,
		Description:  "Daily Act of Kindness and Quote from www.areyvut.org",
		PublishedTTL: publishedTTL,
		Stamp:        today.Truncate(time.Minute),
	}
	ics, err := cal.Marshal(vevents)
	if err != nil {
		return nil, err
	}
	rows := make([][]string, 0, 2*len(days))
	for _, y := range []int{year, year + 1} {
		for _, d := range days {
			date := kindnessDate(y, d)
			rows = append(rows, []string{d.act, csvsvc.FormatDate(date.Year, date.Month, date.Day, false),
				"", "", "", "true", d.quote, "3", "Personal"})
		}
	}
	return &Files{ICS: ics, CSV: csvsvc.Document(rows), Events: len(vevents)}, nil
}

// kindnessDate is the day's date in year; a 29 February outside a leap year
// is 1 March.
func kindnessDate(year int, d kindnessDay) icalendar.Date {
	return icalendar.DateOf(time.Date(year, d.month, d.day, 12, 0, 0, 0, time.UTC))
}
