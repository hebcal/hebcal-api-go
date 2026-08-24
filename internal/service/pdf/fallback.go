package pdf

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
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
// leaves both lists. The codes are the ones readings-svc documents, which are
// also hebcal-web's own /hebcal query parameters.
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

// Fetch returns the events for the named series over [start, end].
//
// The series are requested in one call rather than one call each: /learning
// accepts several at once and returns them interleaved, and each item names its
// own series in its category.
func (f *LearningFetcher) Fetch(ctx context.Context, series []string, lg string, start, end time.Time) ([]Event, error) {
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

	out := make([]Event, 0, len(items))
	for _, it := range items {
		// The response can carry other rows if the sidecar decides to add
		// something by default; take only what was asked for.
		if !wanted[it.Category] {
			continue
		}
		day, err := time.Parse("2006-01-02", it.Date)
		if err != nil {
			continue // a timed event, which no learning series produces
		}
		title := it.Title
		if title == "" {
			title = it.Hebrew
		}
		out = append(out, Event{
			HD:       hdate.FromTime(day),
			Greg:     day,
			Subject:  jsutil.SmartApostrophe(title),
			Learning: true,
			URL:      canonicalLearningURL(it.Link),
		})
	}
	return out, nil
}

// canonicalLearningURL strips the tracking @hebcal/rest-api already added,
// since the renderer applies its own campaign. A Sefaria link is left
// otherwise intact.
func canonicalLearningURL(link string) string {
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

// mergeLearning inserts fetched rows into a generated calendar and restores the
// per-day ordering. Events outside the generated range are dropped: the fetch
// is bounded by the same dates, but hebcal-web resolves them independently.
func mergeLearning(events, learning []Event) []Event {
	if len(learning) == 0 {
		return events
	}
	if len(events) == 0 {
		return learning
	}
	first, last, _ := wholeMonths(events[0].Greg, events[len(events)-1].Greg)
	out := events
	for _, e := range learning {
		if e.Greg.Before(first) || e.Greg.After(last) {
			continue
		}
		out = append(out, e)
	}
	// Generate returns events in date order and the renderer buckets by day, so
	// only the order within a day matters; SplitBy*Month sorts each bucket.
	return out
}

// learningRange is the span to request: whole months, because the calendar
// draws whole months. Asking only for the span between the first and last
// event loses a row on any day at the edge that carries nothing else.
//
// It falls back to the requested calendar when nothing was generated locally,
// which is what a calendar asking only for one of the six unsupported series
// looks like -- there, the fetched rows are the whole calendar.
func learningRange(p *Params, events []Event) (time.Time, time.Time, bool) {
	if len(events) > 0 {
		return wholeMonths(events[0].Greg, events[len(events)-1].Greg)
	}
	if p.Opts.Start.Abs() != 0 && p.Opts.End.Abs() != 0 {
		return wholeMonths(p.Opts.Start.Gregorian(), p.Opts.End.Gregorian())
	}
	if p.Opts.Year == 0 {
		// Without a year or any events there is nothing to anchor a span to.
		return time.Time{}, time.Time{}, false
	}
	if p.Opts.IsHebrewYear {
		// A Hebrew year runs from 1 Tishrei of the year to the day before
		// 1 Tishrei of the next; NumYears extends the far end. wholeMonths
		// then widens it to the Gregorian months the calendar draws.
		n := p.Opts.NumYears
		if n < 1 {
			n = 1
		}
		first := hdate.New(p.Opts.Year, hdate.Tishrei, 1).Gregorian()
		last := hdate.New(p.Opts.Year+n, hdate.Tishrei, 1).Gregorian().AddDate(0, 0, -1)
		return wholeMonths(first, last)
	}
	first := time.Date(p.Opts.Year, time.January, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(p.Opts.Year, time.December, 31, 0, 0, 0, 0, time.UTC)
	if p.Opts.Month >= time.January && p.Opts.Month <= time.December {
		first = time.Date(p.Opts.Year, p.Opts.Month, 1, 0, 0, 0, 0, time.UTC)
		last = first.AddDate(0, 1, -1)
	}
	if n := p.Opts.NumYears; n > 1 {
		last = time.Date(p.Opts.Year+n-1, time.December, 31, 0, 0, 0, 0, time.UTC)
	}
	return wholeMonths(first, last)
}

// wholeMonths widens a span to cover the whole of the months it touches.
func wholeMonths(first, last time.Time) (time.Time, time.Time, bool) {
	start := time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, 1, -1)
	return start, end, true
}
