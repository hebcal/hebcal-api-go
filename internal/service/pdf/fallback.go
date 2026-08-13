package pdf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// fallbackSeries maps a daily-learning series with no schedule in
// github.com/hebcal/learning to the hebcal-web query parameter that selects it.
//
// Six series have no Go implementation. Rather than refuse those calendars,
// their rows are fetched from hebcal-web's /hebcal?cfg=json and merged into the
// locally generated events. Keep this in step with unsupportedSeries in
// params.go: a series gaining a Go schedule moves to learningSchedules and
// leaves both lists.
var fallbackSeries = map[string]string{
	"chofetzChaim":        "dcc",
	"shemiratHaLashon":    "dshl",
	"seferHaMitzvot":      "dsm",
	"kitzurShulchanAruch": "dksa",
	"dirshuAmudYomi":      "ayd",
	"arukhHaShulchanYomi": "ahsy",
}

// LearningFetcher retrieves daily-learning rows this service cannot generate.
type LearningFetcher struct {
	// BaseURL is where hebcal-web is reachable, e.g. http://www.hebcal.com.
	BaseURL string
	// Client bounds the request; a calendar should not hang on a slow sibling.
	Client *http.Client
}

// NewLearningFetcher returns a fetcher pointed at a hebcal-web instance.
func NewLearningFetcher(baseURL string) *LearningFetcher {
	return &LearningFetcher{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		Client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// hebcalJSON is the part of the /hebcal?cfg=json response this needs.
type hebcalJSON struct {
	Items []struct {
		Title    string `json:"title"`
		Date     string `json:"date"`
		Category string `json:"category"`
		Hebrew   string `json:"hebrew"`
		Link     string `json:"link"`
	} `json:"items"`
}

// Fetch returns the events for the named series over [start, end].
//
// The series are requested in one call rather than one call each: hebcal-web
// accepts several at once and returns them interleaved, and each item names its
// own category.
func (f *LearningFetcher) Fetch(ctx context.Context, series []string, lg string, start, end time.Time) ([]Event, error) {
	if len(series) == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("v", "1")
	q.Set("cfg", "json")
	q.Set("start", start.Format("2006-01-02"))
	q.Set("end", end.Format("2006-01-02"))
	if lg != "" {
		q.Set("lg", lg)
	}
	for _, s := range series {
		param, ok := fallbackSeries[s]
		if !ok {
			return nil, fmt.Errorf("no query parameter known for series %q", s)
		}
		q.Set(param, "on")
	}

	u := f.BaseURL + "/hebcal?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching daily learning: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching daily learning: %s returned %d", u, resp.StatusCode)
	}

	var body hebcalJSON
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding daily learning: %w", err)
	}

	wanted := make(map[string]bool, len(series))
	for _, s := range series {
		wanted[s] = true
	}
	out := make([]Event, 0, len(body.Items))
	for _, it := range body.Items {
		// The response can carry other rows if hebcal-web decides to add
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

// canonicalLearningURL strips the tracking hebcal-web already added, since the
// renderer applies its own campaign. A Sefaria link is left otherwise intact.
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
	if p.Opts.Year == 0 || p.Opts.IsHebrewYear {
		// A Hebrew year has no fixed Gregorian span to guess at, and without
		// any events there is nothing to anchor it to.
		return time.Time{}, time.Time{}, false
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
