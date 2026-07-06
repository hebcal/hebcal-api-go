package main

import (
	"sync"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
)

// Year-level memoization.
//
// A single /converter range request converts up to ~180 days, and for each day
// getEvents() needs that day's holidays. Those come from a whole-Hebrew-year
// computation: hebcal.HebrewCalendar / GetHolidaysForYear build and sort ~120
// events for the year. Recomputing that per day made a range request redo the
// same year ~180 times, which is what made the ported service slower than the
// Node original for range requests.
//
// @hebcal/core (the Node implementation this service was ported from) avoids
// that by memoizing getHolidaysForYear_ behind a QuickLRU. These caches port
// that behavior to Go so that a range spanning one or two Hebrew years computes
// each year's holidays only once.
//
// (@hebcal/core also memoizes getSedra, but measurements here showed sedra.New
// costs ~230ns and caching it saved nothing on a range render, so it is left
// uncached.)

// yearKey identifies a cached per-year computation. il matters because the
// holiday and parsha schedules differ between Israel and the Diaspora.
type yearKey struct {
	year int
	il   bool
}

// lruCache is a tiny bounded cache with the same two-generation eviction as
// the QuickLRU used by @hebcal/core: once maxSize live entries accumulate, the
// current generation is retired to old and a fresh generation begins, so at
// most 2*maxSize entries are retained. It is safe for concurrent use; the
// value computation runs outside the lock so a slow miss doesn't block hits.
type lruCache[K comparable, V any] struct {
	mu      sync.Mutex
	maxSize int
	size    int
	cache   map[K]V
	old     map[K]V
}

func newLRUCache[K comparable, V any](maxSize int) *lruCache[K, V] {
	return &lruCache[K, V]{
		maxSize: maxSize,
		cache:   make(map[K]V),
		old:     make(map[K]V),
	}
}

// getOrCompute returns the cached value for key, computing and storing it on a
// miss. compute runs without the lock held; if two goroutines miss the same
// key concurrently, both may compute but only the first stored value is kept.
func (c *lruCache[K, V]) getOrCompute(key K, compute func() V) V {
	c.mu.Lock()
	if v, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return v
	}
	if v, ok := c.old[key]; ok {
		c.insert(key, v) // promote into the current generation
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()

	v := compute()

	c.mu.Lock()
	if existing, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return existing
	}
	c.insert(key, v)
	c.mu.Unlock()
	return v
}

// insert stores key/value, rotating generations when the current one is full.
// Callers must hold c.mu.
func (c *lruCache[K, V]) insert(key K, v V) {
	c.cache[key] = v
	c.size++
	if c.size >= c.maxSize {
		c.old = c.cache
		c.cache = make(map[K]V)
		c.size = 0
	}
}

// maxSize mirrors the QuickLRU({maxSize: 120}) used by @hebcal/core.
const yearCacheSize = 120

var (
	holidayYearCache  = newLRUCache[yearKey, map[int64][]event.CalEvent](yearCacheSize)
	holidaysOnlyCache = newLRUCache[yearKey, map[int64][]event.HolidayEvent](yearCacheSize)
)

// holidayEventsForYear returns the holiday, Shabbat Mevarchim, and Molad events
// for an entire Hebrew year, indexed by RD (absolute) date. It runs
// HebrewCalendar once over the whole year with the same options getEvents used
// per day, so a lookup for any single day yields exactly what the per-day call
// produced. Cached per (year, il).
func holidayEventsForYear(year int, il bool) map[int64][]event.CalEvent {
	return holidayYearCache.getOrCompute(yearKey{year, il}, func() map[int64][]event.CalEvent {
		opts := hebcal.CalOptions{
			Start:            hdate.New(year, hdate.Tishrei, 1),
			End:              hdate.New(year, hdate.Elul, 29),
			IL:               il,
			ShabbatMevarchim: true,
			Molad:            true,
		}
		evs, err := hebcal.HebrewCalendar(&opts)
		if err != nil {
			return map[int64][]event.CalEvent{}
		}
		m := make(map[int64][]event.CalEvent, len(evs))
		for _, ev := range evs {
			d := ev.GetDate()
			abs := d.Abs()
			m[abs] = append(m[abs], ev)
		}
		return m
	})
}

// holidaysForYearByDate returns GetHolidaysForYear indexed by RD date. Unlike
// holidayEventsForYear this is the raw holiday list (including events such as
// Yom Kippur Katan that HebrewCalendar filters out), matching what the original
// holidaysOnDate() consumed. Cached per (year, il).
func holidaysForYearByDate(year int, il bool) map[int64][]event.HolidayEvent {
	return holidaysOnlyCache.getOrCompute(yearKey{year, il}, func() map[int64][]event.HolidayEvent {
		all := hebcal.GetHolidaysForYear(year, il)
		m := make(map[int64][]event.HolidayEvent, len(all))
		for _, hev := range all {
			d := hev.Date
			abs := d.Abs()
			m[abs] = append(m[abs], hev)
		}
		return m
	})
}
