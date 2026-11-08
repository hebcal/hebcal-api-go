package shabbat

import (
	"time"

	"github.com/hebcal/hebcal-go/zmanim"
)

// ExpiresSaturdayNight returns now and the next Sunday 00:00 in the location's
// timezone. The handler stamps them onto Last-Modified and Expires: a rolling
// "this week" answer stops being true once the Shabbat it describes has passed.
//
// now must be the same instant WeekRange chose the week from. Reading the
// clock separately here let a request arriving at Saturday midnight build last
// week's listing (WeekRange still on Saturday) and stamp it with next week's
// Expires (this clock already on Sunday), so caches kept the stale Shabbat for
// a whole week -- and a popular URL's cache entry expires, and is refetched, at
// exactly that instant.
func ExpiresSaturdayNight(tzid string, now time.Time) (time.Time, time.Time) {
	loc, err := zmanim.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	now = now.In(loc)
	offset := 7 - int(now.Weekday()) // Sunday -> +7 (next week), matches dayjs day(7)
	expires := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, offset)
	return now, expires
}
