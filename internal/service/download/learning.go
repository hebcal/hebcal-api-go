package download

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/hebcal/hebcal-go/event"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/webconfig"
)

// LearningOrder places a daily-learning series, named by its EventCategory
// (a readings-svc item's Category), among a day's learning rows: the order of
// dailyLearningConfig.json, except that 929 comes first (see inDayOrder). It
// interleaves the series hebcal-go generates with those readings-svc supplies
// (Sefer HaMitzvot falls between Rambam and Yerushalmi), so the fetched rows
// cannot simply follow the generated ones. It decides which rows survive when
// a subscription is cut at its event limit, and which row overflows a crowded
// PDF cell. Sort a day by time of day first and by this second; an unknown
// series goes last.
func LearningOrder(category string) int {
	for i, c := range learningOrder {
		if c == category {
			return i
		}
	}
	return len(learningOrder)
}

// EventLearningOrder is LearningOrder for one of hebcal-go's events, and 0
// for an event that is not daily learning.
func EventLearningOrder(ev event.CalEvent) int {
	if !ev.Flags().HasAny(LearningFlags) {
		return 0
	}
	return LearningOrder(webconfig.SeriesKey(EventCategories(ev)))
}

// LearningFetcher retrieves daily-learning rows this service cannot generate,
// from the readings-svc sidecar over its unix domain socket.
type LearningFetcher struct {
	// Client talks to readings-svc; nil disables the fallback.
	Client *readings.Client
}

// NewLearningFetcher returns a fetcher backed by the given readings client.
func NewLearningFetcher(client *readings.Client) *LearningFetcher {
	return &LearningFetcher{Client: client}
}

// FetchItems returns the readings-svc items for the named series over
// [start, end], keeping only the series asked for.
func (f *LearningFetcher) FetchItems(ctx context.Context, series []string, lg string, start, end time.Time) ([]readings.Item, error) {
	if len(series) == 0 {
		return nil, nil
	}
	codes := make([]string, 0, len(series))
	wanted := make(map[string]bool, len(series))
	for _, s := range series {
		code, ok := LearningQueryCode(s)
		if !ok {
			return nil, fmt.Errorf("no query parameter known for series %q", s)
		}
		codes = append(codes, code)
		wanted[s] = true
	}

	items, err := f.Client.Learning(ctx, codes, lg, start, end)
	if err != nil {
		return nil, fmt.Errorf("fetching daily learning: %w", err)
	}

	out := items[:0]
	for _, it := range items {
		// The response can carry other rows if the sidecar decides to add
		// something by default; take only what was asked for.
		if wanted[it.Category] {
			out = append(out, it)
		}
	}
	return out, nil
}

// CanonicalLearningURL strips the tracking parameters readings-svc already
// added to a link, so the document can apply its own campaign. A Sefaria link is left
// otherwise intact.
func CanonicalLearningURL(link string) string {
	if link == "" {
		return ""
	}
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Del("utm_source")
	q.Del("utm_medium")
	q.Del("utm_campaign")
	u.RawQuery = q.Encode()
	return u.String()
}

// LearningQueryCode is the readings-svc /learning query code that selects a
// daily-learning series hebcal-go has no schedule for, named by its
// EventCategory ("dcc" for chofetzChaim), and false for any other series.
func LearningQueryCode(category string) (string, bool) {
	for i := range fallbackSeries {
		if fallbackSeries[i].EventCategory == category {
			return fallbackSeries[i].QueryParam, true
		}
	}
	return "", false
}
