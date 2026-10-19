package pdf

import (
	"context"
	"net/url"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
)

// fetchLearning returns the calendar rows for the named series over
// [start, end], fetched from readings-svc.
func fetchLearning(ctx context.Context, f *download.LearningFetcher, series []string, lg string, start, end time.Time) ([]Event, error) {
	items, err := f.FetchItems(ctx, series, lg, start, end)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(items))
	for _, it := range items {
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
			rank:     hebcal.TimeOfDayLearning,
		})
	}
	return out, nil
}

// canonicalLearningURL strips the tracking parameters the sidecar already
// added, since the renderer applies its own campaign. A Sefaria link is left
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
// is bounded by the same dates, but the sidecar resolves them independently.
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
func learningRange(p *download.Params, events []Event) (time.Time, time.Time, bool) {
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
