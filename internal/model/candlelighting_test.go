package model

import (
	"testing"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"
)

func TestMoveCandleLightingToSunset(t *testing.T) {
	loc := zmanim.NewLocation("New York", "US", 40.7128, -74.0060, 0, "America/New_York")
	opts := &hebcal.CalOptions{Location: &loc}
	hd := hdate.FromGregorian(2024, time.January, 5)

	// 18 minutes before sunset (21:42 UTC that day), as a normal calendar
	// would have produced it before b=0 asked for sunset itself.
	before := time.Date(2024, time.January, 5, 21, 24, 0, 0, time.UTC)
	candleLighting := hebcal.TimedEvent{
		HolidayEvent: event.HolidayEvent{Date: hd, Desc: "Candle lighting", Flags: event.LIGHT_CANDLES},
		EventTime:    before,
	}
	// Untimed and non-candle-lighting events must be left alone.
	untimed := event.HolidayEvent{Date: hd, Desc: "Rosh Chodesh Sh'vat", Flags: event.ROSH_CHODESH}
	havdalahTime := time.Date(2024, time.January, 6, 22, 30, 0, 0, time.UTC)
	havdalah := hebcal.TimedEvent{
		HolidayEvent: event.HolidayEvent{Date: hd, Desc: "Havdalah", Flags: event.LIGHT_CANDLES_TZEIS},
		EventTime:    havdalahTime,
	}

	events := []event.CalEvent{candleLighting, untimed, havdalah}
	MoveCandleLightingToSunset(events, opts)

	gy, gm, gd := hd.Greg()
	z := zmanim.New(&loc, time.Date(gy, gm, gd, 12, 0, 0, 0, time.UTC))
	wantSunset := z.SunsetOffset(0, true)
	if wantSunset.IsZero() {
		t.Fatal("test setup: sunset did not occur, pick a different date/location")
	}

	got, ok := events[0].(hebcal.TimedEvent)
	if !ok {
		t.Fatalf("events[0] is %T, want hebcal.TimedEvent", events[0])
	}
	if !got.EventTime.Equal(wantSunset) {
		t.Errorf("candle lighting EventTime = %v, want %v (sunset)", got.EventTime, wantSunset)
	}
	if got.EventTime.Equal(before) {
		t.Error("candle lighting EventTime was not moved off its original value")
	}

	if events[1] != untimed {
		t.Errorf("untimed event was modified: got %+v, want %+v", events[1], untimed)
	}

	gotHavdalah, ok := events[2].(hebcal.TimedEvent)
	if !ok {
		t.Fatalf("events[2] is %T, want hebcal.TimedEvent", events[2])
	}
	if !gotHavdalah.EventTime.Equal(havdalahTime) {
		t.Errorf("Havdalah EventTime = %v, want unchanged %v", gotHavdalah.EventTime, havdalahTime)
	}
}
