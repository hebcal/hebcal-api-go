package shabbat

import (
	"time"

	"github.com/hebcal/hebcal-go/zmanim"
)

// ExpiresSaturdayNight returns "now" and the next Sunday 00:00 in the
// location's timezone. The handler stamps them onto Last-Modified and Expires,
// matching expiresSaturdayNight in hebcal-web: a rolling "this week" answer
// stops being true once the Shabbat it describes has passed.
func ExpiresSaturdayNight(tzid string) (now, expires time.Time) {
	loc, err := zmanim.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	now = time.Now().In(loc)
	offset := 7 - int(now.Weekday()) // Sunday -> +7 (next week), matches dayjs day(7)
	expires = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, offset)
	return now, expires
}
