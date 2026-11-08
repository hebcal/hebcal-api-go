package shabbat

import (
	"net/http"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// TestExpiresFollowsWeek checks that a dateless listing expires at 00:00 on
// the Sunday after the Shabbat it shows, on either side of Saturday midnight
// in Jerusalem. A request at the instant a cached copy expires must not
// describe last week while promising freshness until next Sunday.
func TestExpiresFollowsWeek(t *testing.T) {
	const tzid = "Asia/Jerusalem"
	jlm, err := time.LoadLocation(tzid)
	if err != nil {
		t.Skip(err)
	}
	cases := []struct {
		now         time.Time
		wantStart   model.GregDate
		wantExpires string
	}{
		{ // Saturday 23:59:59.999: still Chayei Sara's week
			time.Date(2026, 11, 7, 23, 59, 59, 999e6, jlm),
			model.GregDate{Year: 2026, Month: 11, Day: 6},
			"Sat, 07 Nov 2026 22:00:00 GMT",
		},
		{ // Sunday 00:00:00: Toldot's week
			time.Date(2026, 11, 8, 0, 0, 0, 0, jlm),
			model.GregDate{Year: 2026, Month: 11, Day: 8},
			"Sat, 14 Nov 2026 22:00:00 GMT",
		},
		{ // Friday afternoon: same week, same Expires
			time.Date(2026, 11, 13, 15, 0, 0, 0, jlm),
			model.GregDate{Year: 2026, Month: 11, Day: 13},
			"Sat, 14 Nov 2026 22:00:00 GMT",
		},
	}
	for _, c := range cases {
		start, _, err := WeekRange(model.GregDate{}, true, tzid, c.now)
		if err != nil {
			t.Fatal(err)
		}
		if start != c.wantStart {
			t.Errorf("%v: start = %v, want %v", c.now, start, c.wantStart)
		}
		expires := ExpiresAfterWeek(tzid, start)
		if got := expires.UTC().Format(http.TimeFormat); got != c.wantExpires {
			t.Errorf("%v: Expires = %s, want %s", c.now, got, c.wantExpires)
		}
	}
}

// TestExpiresAfterWeekEveryDay checks, for every hour of two weeks spanning
// a DST change, that Expires is local midnight on a Sunday, lies after now,
// and comes at most one day after the listing's last Shabbat.
func TestExpiresAfterWeekEveryDay(t *testing.T) {
	const tzid = "America/New_York"
	nyc, err := time.LoadLocation(tzid)
	if err != nil {
		t.Skip(err)
	}
	// US DST ends Sunday 1 November 2026.
	for now := time.Date(2026, 10, 25, 0, 30, 0, 0, nyc); now.Before(time.Date(2026, 11, 8, 0, 0, 0, 0, nyc)); now = now.Add(time.Hour) {
		start, _, err := WeekRange(model.GregDate{}, true, tzid, now)
		if err != nil {
			t.Fatal(err)
		}
		exp := ExpiresAfterWeek(tzid, start).In(nyc)
		if exp.Weekday() != time.Sunday || exp.Hour() != 0 || exp.Minute() != 0 {
			t.Errorf("%v: Expires %v is not Sunday 00:00", now, exp)
		}
		if !exp.After(now) {
			t.Errorf("%v: Expires %v is not in the future", now, exp)
		}
		if exp.Sub(now) > 7*24*time.Hour+time.Hour {
			t.Errorf("%v: Expires %v is more than a week away", now, exp)
		}
	}
}
