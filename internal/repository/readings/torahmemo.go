package readings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// TorahMemo is one row of readings-svc's /torahMemo: the finished Torah and
// Haftarah text for one event with a reading.
type TorahMemo struct {
	// Date is the Gregorian date the event falls on, YYYY-MM-DD.
	Date string `json:"date"`
	// Desc is the event's untranslated English description ("Parashat
	// Vayera", "Rosh Chodesh Kislev", "Chanukah: 3 Candles").
	Desc string `json:"desc"`
	// Memo is the reading, one line per part:
	//
	//	Torah: Genesis 18:1-22:24
	//	Haftarah: II Kings 4:1-37
	//	Haftarah for Sephardim: II Kings 4:1-23
	Memo string `json:"memo"`
}

// TorahMemos returns the Torah memo of every weekly portion and holiday with a
// reading between start and end inclusive, on the Israel schedule when il is
// set. Events without a reading (the Omer, Shabbat Mevarchim, daily learning,
// Hebrew dates and timed events among them) have no row. The text depends on
// nothing but the date, the schedule and the event, so one fetch serves every
// calendar over the same range.
func (c *Client) TorahMemos(ctx context.Context, start, end time.Time, il bool) ([]TorahMemo, error) {
	q := url.Values{}
	q.Set("start", start.Format(time.DateOnly))
	q.Set("end", end.Format(time.DateOnly))
	if il {
		q.Set("i", "on")
	}
	data, err := c.fetch(ctx, "/torahMemo", q)
	if err != nil {
		return nil, err
	}
	var rows []TorahMemo
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("readings: decoding /torahMemo: %w", err)
	}
	// A row outside the range asked for means the sidecar wrote some other
	// date (a Hebrew one looks like "5787-09-27"), and every row would then
	// silently fail to match its event.
	lo, hi := start.Format(time.DateOnly), end.Format(time.DateOnly)
	for _, r := range rows {
		if _, err := time.Parse(time.DateOnly, r.Date); err != nil || r.Date < lo || r.Date > hi {
			return nil, fmt.Errorf("readings: /torahMemo row dated %q is outside %s..%s", r.Date, lo, hi)
		}
	}
	return rows, nil
}
