package model

import (
	"time"

	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"
)

// MoveCandleLightingToSunset re-times candle-lighting to sunset itself, for
// b=0. hebcal-go only reaches for a zero offset on the havdalah side and
// rewrites a zero CandleLightingMins to the default before the calendar is
// built, so the times have to be recomputed here. Drop this once hebcal-go can
// express it.
func MoveCandleLightingToSunset(events []event.CalEvent, opts *hebcal.CalOptions) {
	for i, ev := range events {
		timed, ok := ev.(hebcal.TimedEvent)
		if !ok || timed.Desc != "Candle lighting" {
			continue
		}
		gy, gm, gd := timed.Date.Greg()
		z := zmanim.New(opts.Location, time.Date(gy, gm, gd, 12, 0, 0, 0, time.UTC))
		z.UseElevation = opts.UseElevation
		sunset := z.SunsetOffset(0, true)
		if sunset.IsZero() {
			continue
		}
		timed.EventTime = sunset
		events[i] = timed
	}
}
