package model

import (
	"strings"
	"testing"

	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"
)

func TestParseFastTimes(t *testing.T) {
	cases := []struct {
		name  string
		query map[string]string
		want  FastTimes
	}{
		{"none", nil, FastTimes{}},
		{"all degrees", map[string]string{"fsd": "19.8", "fed": "8.5", "tbed": "6.45"},
			FastTimes{StartDeg: 19.8, EndDeg: 8.5, TishaBavEndDeg: 6.45}},
		{"all minutes", map[string]string{"fsm": "72", "fem": "45", "tbem": "50"},
			FastTimes{StartMins: 72, EndMins: 45, TishaBavEndMins: 50}},
		{"negative is positive", map[string]string{"fsd": "-16.1", "fem": "-20"},
			FastTimes{StartDeg: 16.1, EndMins: 20}},
		{"degrees win over minutes", map[string]string{"fsd": "19.8", "fsm": "90"},
			FastTimes{StartDeg: 19.8}},
		{"invalid degrees let minutes through", map[string]string{"fsd": "90", "fsm": "90"},
			FastTimes{StartMins: 90}},
		{"out of range", map[string]string{"fsd": "0", "fed": "95", "fem": "241", "tbem": "0"},
			FastTimes{}},
		{"unparsable", map[string]string{"fsd": "abc", "fsm": "x", "tbed": ""}, FastTimes{}},
		{"parseInt prefix", map[string]string{"fem": "45min", "tbem": "7.9"},
			FastTimes{EndMins: 45, TishaBavEndMins: 7}},
		{"upper bounds", map[string]string{"fed": "89.9", "tbem": "240"},
			FastTimes{EndDeg: 89.9, TishaBavEndMins: 240}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseFastTimes(func(k string) string { return c.query[k] })
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// fastTimes returns the "Fast begins" and "Fast ends" times of a 2026
// calendar, as "MM-DD begins|ends HH:MM".
func fastTimes(t *testing.T, city string, il bool, f FastTimes) []string {
	t.Helper()
	opts := hebcal.CalOptions{Year: 2026, Location: zmanim.LookupCity(city), CandleLighting: true, IL: il}
	events, err := hebcal.HebrewCalendar(&opts)
	if err != nil {
		t.Fatal(err)
	}
	RetimeFasts(events, &opts, f)
	var out []string
	for _, ev := range events {
		te, ok := ev.(hebcal.TimedEvent)
		if !ok || !strings.HasPrefix(te.Desc, "Fast ") {
			continue
		}
		out = append(out, te.EventTime.Format("01-02 ")+strings.TrimPrefix(te.Desc, "Fast ")+te.EventTime.Format(" 15:04"))
	}
	return out
}

// The expected times are @hebcal/core 6.11's for the same calendars. The
// fasts are Ta'anit Esther (03-02), Ta'anit Bechorot (04-01, no end), Tzom
// Tammuz (07-02), Tish'a B'Av (07-22/23), Tzom Gedaliah (09-14) and Asara
// B'Tevet (12-20).
func TestRetimeFasts(t *testing.T) {
	cases := []struct {
		name string
		city string
		il   bool
		f    FastTimes
		want string
	}{
		{"diaspora defaults", "New York", false, FastTimes{},
			"03-02 begins 05:08,03-02 ends 18:22,04-01 begins 05:16,07-02 begins 03:41,07-02 ends 21:11," +
				"07-22 begins 20:21,07-23 ends 20:54,09-14 begins 05:13,09-14 ends 19:40,12-20 begins 05:48,12-20 ends 17:09"},
		// minor fasts end 15 minutes after sunset in Israel; Tish'a B'Av does not
		{"israel defaults", "Jerusalem", true, FastTimes{},
			"03-02 begins 04:53,03-02 ends 17:53,04-01 begins 05:15,07-02 begins 04:10,07-02 ends 20:04," +
				"07-22 begins 19:43,07-23 ends 20:11,09-14 begins 05:09,09-14 ends 19:02,12-20 begins 05:17,12-20 ends 16:54"},
		{"fsd", "New York", false, FastTimes{StartDeg: 19.8},
			"03-02 begins 04:48,03-02 ends 18:22,04-01 begins 04:55,07-02 begins 03:07,07-02 ends 21:11," +
				"07-22 begins 20:21,07-23 ends 20:54,09-14 begins 04:52,09-14 ends 19:40,12-20 begins 05:27,12-20 ends 17:09"},
		{"fsm", "New York", false, FastTimes{StartMins: 72},
			"03-02 begins 05:16,03-02 ends 18:22,04-01 begins 05:27,07-02 begins 04:17,07-02 ends 21:11," +
				"07-22 begins 20:21,07-23 ends 20:54,09-14 begins 05:23,09-14 ends 19:40,12-20 begins 06:04,12-20 ends 17:09"},
		{"fed in israel", "Jerusalem", true, FastTimes{EndDeg: 8.5},
			"03-02 begins 04:53,03-02 ends 18:14,04-01 begins 05:15,07-02 begins 04:10,07-02 ends 20:31," +
				"07-22 begins 19:43,07-23 ends 20:11,09-14 begins 05:09,09-14 ends 19:23,12-20 begins 05:17,12-20 ends 17:19"},
		{"fem", "New York", false, FastTimes{EndMins: 45},
			"03-02 begins 05:08,03-02 ends 18:34,04-01 begins 05:16,07-02 begins 03:41,07-02 ends 21:16," +
				"07-22 begins 20:21,07-23 ends 20:54,09-14 begins 05:13,09-14 ends 19:52,12-20 begins 05:48,12-20 ends 17:16"},
		{"tbed", "New York", false, FastTimes{TishaBavEndDeg: 8.5},
			"03-02 begins 05:08,03-02 ends 18:22,04-01 begins 05:16,07-02 begins 03:41,07-02 ends 21:11," +
				"07-22 begins 20:21,07-23 ends 21:08,09-14 begins 05:13,09-14 ends 19:40,12-20 begins 05:48,12-20 ends 17:09"},
		{"tbem in israel", "Jerusalem", true, FastTimes{TishaBavEndMins: 50},
			"03-02 begins 04:53,03-02 ends 17:53,04-01 begins 05:15,07-02 begins 04:10,07-02 ends 20:04," +
				"07-22 begins 19:43,07-23 ends 20:32,09-14 begins 05:09,09-14 ends 19:02,12-20 begins 05:17,12-20 ends 16:54"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Join(fastTimes(t, c.city, c.il, c.f), ",")
			if got != c.want {
				t.Errorf("got\n  %s\nwant\n  %s", got, c.want)
			}
		})
	}
}
