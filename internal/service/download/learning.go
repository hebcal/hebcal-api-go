package download

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/hebcal/hebcal-go/event"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
)

// learningOrder is the order of a day's daily-learning rows, by event
// category: the download form's, except that 929 comes first. It interleaves
// the series hebcal-go generates with those readings-svc supplies (Sefer
// HaMitzvot falls between Rambam and Yerushalmi, Daf Weekly and Pirkei Avot
// between Dirshu Amud HaYomi and Arukh HaShulchan), so the fetched rows cannot
// simply follow the generated ones. It decides which rows survive when a
// subscription is cut at its event limit, and which row overflows a crowded
// PDF cell.
var learningOrder = []string{
	"929", "dafyomi", "mishnayomi", "perekYomi", "nachyomi", "tanakhYomi",
	"dailyPsalms", "rambam1", "rambam3", "seferHaMitzvot", "yerushalmi",
	"chofetzChaim", "shemiratHaLashon", "dirshuAmudYomi", "dafWeekly",
	"pirkeiAvotSummer", "arukhHaShulchanYomi", "kitzurShulchanAruch",
	"dirshuDafHalacha", "tanyaYomi",
}

// LearningOrder places a daily-learning series, named by its event category
// (a readings-svc item's Category), among a day's learning rows. Sort a day by
// time of day first and by this second; an unknown series goes last.
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
	if !ev.GetFlags().HasAny(LearningFlags) {
		return 0
	}
	return LearningOrder(EventCategories(ev)[0])
}

// fallbackSeries maps a daily-learning series with no schedule in
// github.com/hebcal/learning to the readings-svc /learning query code that
// selects it.
//
// Eight series have no Go implementation. Rather than refuse those calendars,
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
	"tanyaYomi":           "dtn",
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

	fetch := func(lg string) ([]readings.Item, error) {
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
	out, err := fetch(lg)
	if err != nil || (lg != "ah" && lg != "sh") {
		return out, err
	}
	// The Hebrew name lg=ah and lg=sh append is the brief Hebrew title, with
	// nikud ("חֲזָרָה מִשְׁנָה בְּרוּרָה"); an item's own Hebrew field is
	// rendered without. The same query in Hebrew lists the same rows in the
	// same order.
	he, err := fetch("he")
	if err != nil {
		return nil, err
	}
	if len(he) == len(out) {
		for i := range out {
			if he[i].Date == out[i].Date && he[i].Category == out[i].Category && he[i].Title != "" {
				out[i].Hebrew = he[i].Title
			}
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
// daily-learning series hebcal-go has no schedule for ("dcc" for
// chofetzChaim), and false for any other series.
func LearningQueryCode(series string) (string, bool) {
	code, ok := fallbackSeries[series]
	return code, ok
}
