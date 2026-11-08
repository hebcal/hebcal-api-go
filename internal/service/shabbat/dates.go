package shabbat

import (
	"time"

	"github.com/hebcal/hebcal-go/zmanim"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// ExpiresAfterWeek returns 00:00 on the Sunday after the Shabbat of the week
// that begins on start, in the location's timezone: the moment a rolling
// "this week" listing beginning on start stops being true. The handler stamps
// it onto Expires.
//
// It is derived from the window the response actually covers, not from the
// clock, so the header cannot disagree with the body. When Expires was
// computed from a second clock reading, a request arriving at Saturday
// midnight built last week's listing and stamped it with next week's Expires,
// and caches served the past Shabbat for a whole week -- a popular URL's cache
// entry expires, and is refetched, at exactly that instant.
func ExpiresAfterWeek(tzid string, start model.GregDate) time.Time {
	loc, err := zmanim.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	base := time.Date(start.Year, start.Month, start.Day, 0, 0, 0, 0, loc)
	toSaturday := (int(time.Saturday) - int(base.Weekday()) + 7) % 7
	return time.Date(start.Year, start.Month, start.Day+toSaturday+1, 0, 0, 0, 0, loc)
}
