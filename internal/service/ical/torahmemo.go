package ical

import (
	"context"
	"fmt"
	"sort"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
)

// memoCacheSize is how many (Hebrew year, schedule) readings are kept: a
// handful of years on each of the two schedules.
const memoCacheSize = 32

type memoKey struct {
	hyear int
	il    bool
}

// torahMemos returns the Torah reading text for every event of the calendar
// that has one. The text depends only on the date, the schedule and the event
// -- no location, no language -- so each Hebrew year is fetched once per
// schedule and shared by every feed that reaches into it. A feed none of
// whose events can have a reading asks for nothing.
func (s *Service) torahMemos(ctx context.Context, events []event.CalEvent, il bool) (TorahMemos, error) {
	years := map[int]bool{}
	for _, ev := range events {
		if hasReading(ev) {
			years[ev.GetDate().Year()] = true
		}
	}
	if len(years) == 0 {
		return nil, nil
	}
	if s.Readings == nil {
		return nil, &ReadingsError{}
	}
	order := make([]int, 0, len(years))
	for y := range years {
		order = append(order, y)
	}
	sort.Ints(order)
	all := TorahMemos{}
	for _, y := range order {
		m, err := s.yearMemos(ctx, y, il)
		if err != nil {
			return nil, &ReadingsError{Retryable: true, Err: err}
		}
		for k, v := range m {
			all[k] = v
		}
	}
	return all, nil
}

// yearMemos is one Hebrew year's readings, from the cache or readings-svc.
func (s *Service) yearMemos(ctx context.Context, hyear int, il bool) (TorahMemos, error) {
	s.cacheOnce.Do(func() {
		// New only fails for a size below one.
		s.cache, _ = lru.New[memoKey, TorahMemos](memoCacheSize)
	})
	key := memoKey{hyear, il}
	if m, ok := s.cache.Get(key); ok {
		return m, nil
	}
	v, err, _ := s.flight.Do(fmt.Sprint(key), func() (any, error) {
		start := hdate.New(hyear, hdate.Tishrei, 1).Gregorian()
		end := hdate.New(hyear+1, hdate.Tishrei, 1).Gregorian().AddDate(0, 0, -1)
		// A detached context: the answer is shared with every request waiting
		// on it, so one caller giving up must not fail them all.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		rows, err := s.Readings.TorahMemos(fetchCtx, start, end, il)
		if err != nil {
			return nil, err
		}
		m := NewTorahMemos(rows)
		s.cache.Add(key, m)
		return m, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(TorahMemos), nil
}
