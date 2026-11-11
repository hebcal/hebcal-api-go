package download

import (
	"time"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// NumYears is how many years a CSV or iCalendar calendar spans. A download that names no
// span gets several years, fewer the more events each year carries, so that
// the file stays a reasonable size: 7 by default, down to 4 with
// candle-lighting, 2 with alternate dates every day or with any daily
// learning, and 1 with two or more daily series. An explicit span is honoured
// up to 10 years. The PDF calendars do not do this; they draw one year unless
// asked for more.
func NumYears(msg *downloadpb.Download, p *Params, numUnsupported int) int {
	if n := msg.GetNumYears(); n > 0 {
		return min(int(n), 10)
	}
	o := &p.Opts
	n := 7
	if o.Omer && o.Sedrot {
		n = 4 // omer + sedrot adds 101 events
	}
	if o.CandleLighting {
		if p.AddAltDates {
			n = 2
		} else if p.AddAltDatesForEvents {
			n = 3
		}
	}
	numLearning := len(o.DailyLearning) + numUnsupported
	for _, cap := range []struct {
		on bool
		ny int
	}{
		{o.CandleLighting, 4},
		{o.Sedrot, 5},
		{o.Omer, 5},
		{p.AddAltDatesForEvents, 4},
		{p.AddAltDates, 2},
		{numLearning > 0, 2},
	} {
		if cap.on && cap.ny < n {
			n = cap.ny
		}
	}
	daily := numLearning
	if p.AddAltDates {
		daily++
	}
	if daily > 1 {
		n = 1
	}
	return n
}

// LearningRange is the span to fetch daily learning for: the calendar's own
// date range, the same days hebcal-go generates the other series over. A
// whole Hebrew year starts on Erev Rosh Hashana, a Gregorian one may be a
// single month, and a Hebrew-month calendar also carries the following
// Tishrei.
func LearningRange(p *Params) (time.Time, time.Time) {
	o := &p.Opts
	if o.Start.Abs() != 0 && o.End.Abs() != 0 {
		return o.Start.Gregorian(), o.End.Gregorian()
	}
	year := o.Year
	if year == 0 {
		now := time.Now()
		if o.IsHebrewYear {
			year = hdate.FromTime(now).Year()
		} else {
			year = now.Year()
		}
	}
	n := max(o.NumYears, 1)
	if o.IsHebrewYear {
		first := hdate.New(year, hdate.Tishrei, 1).Gregorian()
		if year > 1 {
			first = first.AddDate(0, 0, -1)
		}
		last := hdate.New(year+n, hdate.Tishrei, 1).Gregorian().AddDate(0, 0, -1)
		if p.MonthMode != GregorianArabic {
			last = hdate.New(year+n, hdate.Tishrei, 30).Gregorian()
		}
		return first, last
	}
	if o.Month >= time.January && o.Month <= time.December {
		first := time.Date(year, o.Month, 1, 0, 0, 0, 0, time.UTC)
		return first, first.AddDate(0, 1, -1)
	}
	first := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(year+n-1, time.December, 31, 0, 0, 0, 0, time.UTC)
	return first, last
}
