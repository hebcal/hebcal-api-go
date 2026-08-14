package readings

import (
	"context"
	"net/url"
	"time"
)

// Learning returns the daily-learning items for the given readings-svc query
// codes ("dcc", "dsm", ...) over [start, end].
//
// The series are requested in one call rather than one call each: /learning
// accepts several at once and returns them interleaved, and each item names
// its own series in Category.
func (c *Client) Learning(ctx context.Context, codes []string, lg string, start, end time.Time) ([]Item, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("start", start.Format("2006-01-02"))
	q.Set("end", end.Format("2006-01-02"))
	if lg != "" {
		q.Set("lg", lg)
	}
	for _, code := range codes {
		q.Set(code, "on")
	}
	return c.get(ctx, "/learning", q)
}
