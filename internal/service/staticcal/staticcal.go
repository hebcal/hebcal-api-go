// Package staticcal builds the multi-year calendars published as static files
// under download.hebcal.com/ical/: the holiday, Hebrew-date, Omer, Torah
// reading, Yizkor and Shabbat Mevarchim feeds, one feed per daily-learning
// series, and Kindness a Day. Each is an .ics and a .csv, regenerated weekly
// by cmd/hebcal-static-ical.
//
// Everything here is a function of the feed, "today" and what readings-svc
// answers -- no flags, files or logging -- so a feed can be regenerated and
// compared in a test.
package staticcal

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/hebcal"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	csvsvc "github.com/hebcal/hebcal-api-go/internal/service/csv"
	"github.com/hebcal/hebcal-api-go/internal/service/download"
	"github.com/hebcal/hebcal-api-go/internal/service/ical"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// Readings is what the feeds need from readings-svc: the Torah readings for
// holiday and Torah-portion feeds, and the daily learning series hebcal-go has
// no schedule for. *readings.Client implements it.
type Readings interface {
	TorahMemos(ctx context.Context, start, end time.Time, il bool) ([]readings.TorahMemo, error)
	Learning(ctx context.Context, codes []string, lg string, start, end time.Time) ([]readings.Item, error)
}

// publishedTTL asks subscribers to refresh weekly, as often as the files are
// regenerated.
const publishedTTL = "P7D"

// Files is one feed rendered.
type Files struct {
	ICS []byte
	CSV []byte
	// Events counts the events in the feed.
	Events int
}

// Window is the span a feed covers: from 90 days before today, so a calendar
// refreshed weekly never loses an event someone is still looking at, to
// years ahead less 60 days.
func Window(today time.Time, years int) (start, end hdate.HDate) {
	now := hdate.FromTime(today).Abs()
	end64 := now + int64(math.Round(365.25*float64(years))) - 60
	return hdate.FromRD(now - 90), hdate.FromRD(end64)
}

// Render builds one feed's two files. today fixes the window and, truncated
// to the minute, every event's DTSTAMP.
func Render(ctx context.Context, f *Feed, today time.Time, rs Readings) (*Files, error) {
	start, end := Window(today, f.Years)
	opt := &ical.Options{
		Locale:   f.Locale,
		IL:       f.IL,
		Emoji:    f.Emoji,
		Campaign: "ical-" + f.File,
	}
	cal := &icalendar.Calendar{
		Title:        f.Title,
		Description:  f.Desc,
		Lang:         ical.ProdidLang(f.Locale),
		PublishedTTL: publishedTTL,
		RelCalID:     f.RelCalID,
		Color:        strings.ToUpper(f.Color),
		Stamp:        today.Truncate(time.Minute),
	}
	switch {
	case f.ChofetzChaim:
		return renderChofetzChaim(ctx, f, cal, opt, start, end, rs)
	case f.Series != "":
		return renderSeries(ctx, f, cal, opt, start, end, rs)
	}

	p := &download.Params{
		Opts:            f.Opts,
		Locale:          f.Locale,
		LG:              f.Locale,
		NoMinorHolidays: f.NoMinorHolidays,
	}
	p.Opts.Start, p.Opts.End, p.Opts.IL = start, end, f.IL
	events, err := download.GenerateCalEvents(p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.File, err)
	}
	if hasReadings(&p.Opts) {
		rows, err := rs.TorahMemos(ctx, start.Gregorian(), end.Gregorian(), f.IL)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.File, err)
		}
		opt.TorahMemos = ical.NewTorahMemos(rows)
	}
	vevents := make([]icalendar.Event, len(events))
	for i, ev := range events {
		vevents[i] = ical.Event(ev, opt)
	}
	ics, err := cal.Marshal(vevents)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.File, err)
	}
	csv := csvsvc.Render(&csvsvc.Calendar{Params: p, Events: events})
	return &Files{ICS: ics, CSV: csv, Events: len(events)}, nil
}

// hasReadings reports whether a feed can carry events with a Torah reading:
// weekly portions, or any of the holidays.
func hasReadings(o *hebcal.Options) bool {
	return o.Sedrot || !o.NoHolidays
}

// renderSeries builds the feed of a daily-learning series readings-svc
// supplies.
func renderSeries(ctx context.Context, f *Feed, cal *icalendar.Calendar, opt *ical.Options,
	start, end hdate.HDate, rs Readings) (*Files, error) {
	items, err := fetchLearning(ctx, rs, []string{f.Series}, f.Locale, start, end)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.File, err)
	}
	vevents := make([]icalendar.Event, 0, len(items))
	for i := range items {
		if e, ok := ical.LearningItem(&items[i], opt); ok {
			vevents = append(vevents, e)
		}
	}
	ics, err := cal.Marshal(vevents)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.File, err)
	}
	p := &download.Params{Locale: f.Locale, LG: f.Locale}
	csv := csvsvc.Render(&csvsvc.Calendar{Params: p, Learning: items})
	return &Files{ICS: ics, CSV: csv, Events: len(vevents)}, nil
}

// fetchLearning returns readings-svc's rows for the named series over the
// window, keeping only those series.
func fetchLearning(ctx context.Context, rs Readings, series []string, lg string,
	start, end hdate.HDate) ([]readings.Item, error) {
	codes := make([]string, len(series))
	wanted := make(map[string]bool, len(series))
	for i, s := range series {
		code, ok := download.LearningQueryCode(s)
		if !ok {
			return nil, fmt.Errorf("no readings-svc query code for series %q", s)
		}
		codes[i] = code
		wanted[s] = true
	}
	items, err := rs.Learning(ctx, codes, lg, start.Gregorian(), end.Gregorian())
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, it := range items {
		if wanted[it.Category] {
			out = append(out, it)
		}
	}
	if len(out) == 0 {
		// An older readings-svc ignores a series it does not know, and the
		// feed would be published empty.
		return nil, fmt.Errorf("readings-svc returned no rows for %v", series)
	}
	return out, nil
}
