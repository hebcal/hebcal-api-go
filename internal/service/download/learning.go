package download

import (
	"context"
	"fmt"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
)

// fallbackSeries maps a daily-learning series with no schedule in
// github.com/hebcal/learning to the readings-svc /learning query code that
// selects it.
//
// Seven series have no Go implementation. Rather than refuse those calendars,
// their rows are fetched from the readings-svc sidecar and merged into the
// locally generated events. Keep this in step with unsupportedSeries in
// params.go: a series gaining a Go schedule moves to learningSchedules and
// leaves both lists. The codes are the ones readings-svc documents.
var fallbackSeries = map[string]string{
	"chofetzChaim":        "dcc",
	"shemiratHaLashon":    "dshl",
	"seferHaMitzvot":      "dsm",
	"kitzurShulchanAruch": "dksa",
	"dirshuAmudYomi":      "ayd",
	"dirshuDafHalacha":    "ddh",
	"arukhHaShulchanYomi": "ahsy",
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
		code, ok := fallbackSeries[s]
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
