package staticcal

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	csvsvc "github.com/hebcal/hebcal-api-go/internal/service/csv"
	"github.com/hebcal/hebcal-api-go/internal/service/ical"
	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// chofetzChaimSeries are the two readings each day of the Chofetz Chaim feed
// pairs, in the order they are shown.
var chofetzChaimSeries = [2]struct{ series, book string }{
	{"chofetzChaim", "Sefer Chofetz Chaim"},
	{"shemiratHaLashon", "Shemirat HaLashon"},
}

const chofetzChaimCategory = "Chofetz Chaim"

// renderChofetzChaim builds the feed whose every day is one event naming both
// that day's Chofetz Chaim and its Shemirat HaLashon reading. A day missing
// either reading is skipped.
func renderChofetzChaim(ctx context.Context, f *Feed, cal *icalendar.Calendar, opt *ical.Options,
	start, end hdate.HDate, rs Readings) (*Files, error) {
	items, err := fetchLearning(ctx, rs,
		[]string{chofetzChaimSeries[0].series, chofetzChaimSeries[1].series}, f.Locale, start, end)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.File, err)
	}
	byDay := make(map[string]*[2]*readings.Item)
	var days []string
	for i := range items {
		it := &items[i]
		idx := 0
		if it.Category == chofetzChaimSeries[1].series {
			idx = 1
		}
		pair := byDay[it.Date]
		if pair == nil {
			pair = new([2]*readings.Item)
			byDay[it.Date] = pair
			days = append(days, it.Date)
		}
		pair[idx] = it
	}

	vevents := make([]icalendar.Event, 0, len(days))
	rows := make([][]string, 0, len(days))
	for _, d := range days {
		pair := byDay[d]
		day, err := time.Parse(time.DateOnly, d)
		if err != nil || pair[0] == nil || pair[1] == nil {
			continue
		}
		hd := hdate.FromTime(day)
		subj := chofetzChaimTitle(pair[0]) + " / " + pair[1].Title
		memo := chofetzChaimMemo(hd, pair, opt.Campaign)
		date := icalendar.DateOf(day)
		vevents = append(vevents, icalendar.Event{
			UID:         "hebcal-" + date.String() + "-chofetz-chaim",
			Summary:     subj,
			Date:        date,
			Category:    chofetzChaimCategory,
			Location:    chofetzChaimCategory,
			Description: memo,
		})
		rows = append(rows, []string{subj, csvsvc.FormatDate(day.Year(), day.Month(), day.Day(), false),
			"", "", "", "true", csvsvc.FlattenNewlines(memo), "3", "Jewish Holidays"})
	}
	ics, err := cal.Marshal(vevents)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.File, err)
	}
	return &Files{ICS: ics, CSV: csvsvc.Document(rows), Events: len(vevents)}, nil
}

// chofetzChaimMemo describes both readings: for each, the book and the Hebrew
// day, the reading's section, and its tracked link, each followed by a blank
// line.
func chofetzChaimMemo(hd hdate.HDate, pair *[2]*readings.Item, campaign string) string {
	hdateStr := strconv.Itoa(hd.Day()) + " " + model.HDMonthNameEn(hd)
	var memo string
	for i, it := range pair {
		memo += chofetzChaimSeries[i].book + ", " + hdateStr + "\n" +
			it.Memo + "\n" + ical.LearningLink(it.Link, campaign) + "\n\n"
	}
	return memo
}

// chofetzChaimTitle is a Chofetz Chaim reading's full title: readings-svc's
// brief title, plus -- for the readings that have them, the preface's -- the
// Hebrew words the reading starts and ends with. Those end the reading's memo
// ("Preface 5-10 וכאשר נחפשה - מגדל העון"), after its English name and pages,
// which are Latin script and digits.
func chofetzChaimTitle(it *readings.Item) string {
	i := strings.IndexFunc(it.Memo, func(r rune) bool { return r >= 0x0590 && r <= 0x05FF })
	if i <= 0 || it.Memo[i-1] != ' ' {
		return it.Title
	}
	return it.Title + it.Memo[i-1:]
}
