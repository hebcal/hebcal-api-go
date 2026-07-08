package main

import (
	"strconv"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
)

// Year-level memoization.
//
// A single /converter range request converts up to ~399 days, and for each day
// getEvents() needs that day's holidays. Those come from a whole-Hebrew-year
// computation: hebcal.HebrewCalendar / GetHolidaysForYear build and sort ~120
// events for the year. Recomputing that per day made a range request redo the
// same year ~399 times, which is what made the ported service slower than the
// Node original for range requests.
//
// @hebcal/core (the Node implementation this service was ported from) avoids
// that by memoizing getHolidaysForYear_ behind a QuickLRU. These caches port
// that behavior to Go so that a range spanning one or two Hebrew years computes
// each year's holidays only once.
//
// (@hebcal/core also memoizes getSedra, but measurements here showed sedra.New
// costs ~230ns and caching it saved nothing on a range render, so the sedra
// schedule is left uncached.)

// yearKey identifies a cached per-year computation. il matters because the
// holiday schedule differs between Israel and the Diaspora.
type yearKey struct {
	year int
	il   bool
}

// yearMemo memoizes a per-(year, il) computation. It pairs a bounded LRU with a
// singleflight group so that concurrent misses for the same key compute once
// and share the result, while misses for different keys still run in parallel.
type yearMemo[V any] struct {
	cache *lru.Cache[yearKey, V]
	group singleflight.Group
}

func newYearMemo[V any](size int) *yearMemo[V] {
	// lru.New only errors on a non-positive size, which is a programmer error
	// for our fixed yearCacheSize.
	cache, err := lru.New[yearKey, V](size)
	if err != nil {
		panic(err)
	}
	return &yearMemo[V]{cache: cache}
}

// get returns the cached value for key, computing it on a miss. compute runs at
// most once per key even under concurrent misses.
func (m *yearMemo[V]) get(key yearKey, compute func() V) V {
	if v, ok := m.cache.Get(key); ok {
		return v
	}
	// singleflight keys are strings; year digits plus an "i" for Israel are
	// unambiguous (digits never collide with the suffix).
	sfKey := strconv.Itoa(key.year)
	if key.il {
		sfKey += "i"
	}
	v, _, _ := m.group.Do(sfKey, func() (any, error) {
		// Re-check under singleflight in case another goroutine populated the
		// cache between our miss and acquiring flight leadership.
		if v, ok := m.cache.Get(key); ok {
			return v, nil
		}
		computed := compute()
		m.cache.Add(key, computed)
		return computed, nil
	})
	return v.(V)
}

// yearCacheSize mirrors the QuickLRU({maxSize: 120}) used by @hebcal/core.
const yearCacheSize = 120

var (
	holidayYearCache  = newYearMemo[map[int64][]event.CalEvent](yearCacheSize)
	holidaysOnlyCache = newYearMemo[map[int64][]event.HolidayEvent](yearCacheSize)
)

// holidayEventsForYear returns the holiday, Shabbat Mevarchim, and Molad events
// for an entire Hebrew year, indexed by RD (absolute) date. It runs
// HebrewCalendar once over the whole year with the same options getEvents used
// per day, so a lookup for any single day yields exactly what the per-day call
// produced. Cached per (year, il).
func holidayEventsForYear(year int, il bool) map[int64][]event.CalEvent {
	return holidayYearCache.get(yearKey{year, il}, func() map[int64][]event.CalEvent {
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
	return holidaysOnlyCache.get(yearKey{year, il}, func() map[int64][]event.HolidayEvent {
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
