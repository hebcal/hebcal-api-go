package readings

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// cacheSize holds roughly a year of dates for both Israel and the Diaspora, so
// the working set of "this week" and "next week" always fits.
const cacheSize = 400

// cacheKey identifies a cached day of readings.
type cacheKey struct {
	date string // YYYY-MM-DD
	il   bool
}

// leyningCache memoizes readings per (date, Israel), because leyning depends
// on nothing else: every city in Israel reads the same portion on a given day,
// as does every city in the Diaspora. A week's worth of dates is therefore
// shared by all the requests for that week, whatever the location.
type leyningCache struct {
	lru   *lru.Cache[cacheKey, []Item]
	group singleflight.Group
}

func newLeyningCache() *leyningCache {
	// lru.New only errors on a non-positive size, a programmer error here.
	c, err := lru.New[cacheKey, []Item](cacheSize)
	if err != nil {
		panic(err)
	}
	return &leyningCache{lru: c}
}

// Leyning returns the readings for every Gregorian date in [start, end], keyed
// by YYYY-MM-DD. Cached days are served from memory; the rest are fetched in a
// single request spanning the missing days.
func (c *Client) Leyning(ctx context.Context, start, end model.GregDate, il bool) (map[string][]Item, error) {
	dates := isoDateRange(start, end)
	out := make(map[string][]Item, len(dates))
	var missing []string
	for _, d := range dates {
		if v, ok := c.cache.lru.Get(cacheKey{d, il}); ok {
			out[d] = v
		} else {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	// One request covering the missing span: the /shabbat window is at most a
	// week, so fetching the gap whole beats a request per day.
	from, to := missing[0], missing[len(missing)-1]
	sfKey := from + ".." + to
	if il {
		sfKey += "i"
	}
	v, err, _ := c.cache.group.Do(sfKey, func() (any, error) {
		return c.fetchLeyning(ctx, from, to, il)
	})
	if err != nil {
		return nil, err
	}
	for d, items := range v.(map[string][]Item) {
		out[d] = items
	}
	return out, nil
}

// fetchLeyning performs one GET against /leyning and returns the readings for
// every date in [from, to], adding each day (including days with no reading)
// to the cache so a repeat request for the same week is a pure cache hit.
func (c *Client) fetchLeyning(ctx context.Context, from, to string, il bool) (map[string][]Item, error) {
	q := url.Values{}
	q.Set("start", from)
	q.Set("end", to)
	if il {
		q.Set("i", "on")
	}
	// No lg: /leyning is English-only by design and ignores one. The readings
	// are locale-invariant, and items are matched to hebcal-go's events by the
	// untranslated event description, so a localized /shabbat request still
	// gets English readings -- as it does from hebcal-web.
	ctx, cancel := context.WithTimeout(ctx, leyningTimeout)
	defer cancel()
	items, err := c.get(ctx, "/leyning", q)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string][]Item)
	for _, item := range items {
		byDate[item.Date] = append(byDate[item.Date], item)
	}
	fromD, err1 := model.IsoDateStringToDate(from)
	toD, err2 := model.IsoDateStringToDate(to)
	if err1 != nil || err2 != nil {
		return byDate, nil
	}
	for _, d := range isoDateRange(fromD, toD) {
		c.cache.lru.Add(cacheKey{d, il}, byDate[d])
	}
	return byDate, nil
}

// isoDateRange lists every YYYY-MM-DD date from start through end inclusive.
func isoDateRange(start, end model.GregDate) []string {
	first := time.Date(start.Year, start.Month, start.Day, 0, 0, 0, 0, time.UTC)
	last := time.Date(end.Year, end.Month, end.Day, 0, 0, 0, 0, time.UTC)
	var out []string
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out
}

// parshaCategory is the classic-API category of a Parsha HaShavua item.
const parshaCategory = "parashat"

// FindParsha returns the parsha ha-shavua reading for a date, i.e. what
// getLeyningForParshaHaShavua() produced for the ParshaEvent. The triennial
// aliyot are already part of it, for Hebrew year 5745 on.
func FindParsha(items []Item) json.RawMessage {
	for i := range items {
		if items[i].Category == parshaCategory {
			return items[i].Leyning
		}
	}
	return nil
}

// FindHoliday returns the reading of the event with the given untranslated
// description, i.e. what getLeyningForHoliday() produced for it.
func FindHoliday(items []Item, desc string) json.RawMessage {
	for i := range items {
		if items[i].Category != parshaCategory && items[i].Desc() == desc {
			return items[i].Leyning
		}
	}
	return nil
}
