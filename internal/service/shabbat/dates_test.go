package shabbat

import (
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// TestWeekRangeAgreesWithExpires pins the window and its Expires to one
// clock reading on either side of Saturday midnight in Jerusalem. A request
// at the instant a cached copy expires must not describe last week while
// promising freshness until next Sunday.
func TestWeekRangeAgreesWithExpires(t *testing.T) {
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
	}
	for _, c := range cases {
		start, _, err := WeekRange(model.GregDate{}, true, tzid, c.now)
		if err != nil {
			t.Fatal(err)
		}
		if start != c.wantStart {
			t.Errorf("%v: start = %v, want %v", c.now, start, c.wantStart)
		}
		_, expires := ExpiresSaturdayNight(tzid, c.now)
		if got := expires.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"); got != c.wantExpires {
			t.Errorf("%v: Expires = %s, want %s", c.now, got, c.wantExpires)
		}
	}
}
