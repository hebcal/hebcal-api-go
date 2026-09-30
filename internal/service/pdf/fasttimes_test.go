package pdf

import (
	"strings"
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/model"
	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// A token encoded by hebcal-web's own download_pb.cjs, carrying
// fastStartDeg=19.8, fastEndMins=45 and tishaBavEndDeg=8.5 beside a New York
// 2026 calendar. 19.8 does not survive a 32-bit float exactly, and must come
// back as 19.8 rather than 19.799999237060547.
const hebcalWebFastToken = "CAEoAUABUAFYhYO5AmDqD7UEZmaeQcgELdUEAAAIQQ"

func TestFastTimesFromHebcalWebToken(t *testing.T) {
	msg, err := DecodeMessage(hebcalWebFastToken)
	if err != nil {
		t.Fatal(err)
	}
	want := model.FastTimes{StartDeg: 19.8, EndMins: 45, TishaBavEndDeg: 8.5}
	if got := fastTimesFromMessage(msg); got != want {
		t.Errorf("fastTimesFromMessage = %+v, want %+v", got, want)
	}
	if qs := MessageToQuery(msg); !strings.Contains(qs, "&fsd=19.8&fem=45&tbed=8.5&") {
		t.Errorf("MessageToQuery = %q, want the fast parameters", qs)
	}
}

// A crafted token gets the query string's validation: nothing out of range,
// and degrees win when a pair carries both.
func TestFastTimesFromCraftedToken(t *testing.T) {
	msg := &pb.Download{
		FastStartDeg: -16.1, FastStartMins: 90, // degrees win, sign dropped
		FastEndMins:    300, // over 240
		TishaBavEndDeg: 95,  // over 90
	}
	want := model.FastTimes{StartDeg: 16.1}
	if got := fastTimesFromMessage(msg); got != want {
		t.Errorf("fastTimesFromMessage = %+v, want %+v", got, want)
	}
}

func TestDecodeV2FastTimes(t *testing.T) {
	msg := decodeV2(t, "v=1&geonameid=5128581&fsd=19.8&fsm=72&fem=45&tbed=abc&tbem=50")
	if msg.GetFastStartDeg() != 19.8 || msg.GetFastStartMins() != 0 {
		t.Errorf("fast start = %v deg, %d mins; want 19.8 deg only", msg.GetFastStartDeg(), msg.GetFastStartMins())
	}
	if msg.GetFastEndDeg() != 0 || msg.GetFastEndMins() != 45 {
		t.Errorf("fast end = %v deg, %d mins; want 45 mins", msg.GetFastEndDeg(), msg.GetFastEndMins())
	}
	if msg.GetTishaBavEndDeg() != 0 || msg.GetTishaBavEndMins() != 50 {
		t.Errorf("Tish'a B'Av end = %v deg, %d mins; want 50 mins", msg.GetTishaBavEndDeg(), msg.GetTishaBavEndMins())
	}

	// geo=none drops them along with the other time parameters.
	msg = decodeV2(t, "v=1&geo=none&fsd=19.8&fem=45&tbem=50")
	if msg.GetFastStartDeg() != 0 || msg.GetFastEndMins() != 0 || msg.GetTishaBavEndMins() != 0 {
		t.Errorf("geo=none kept a fast time: %v", msg)
	}
}

// Generate applies the fast times to the calendar it draws.
func TestGenerateRetimesFasts(t *testing.T) {
	fastEnds := func(msg *pb.Download) string {
		t.Helper()
		p, err := DecodeParams(encode(t, msg), nil)
		if err != nil {
			t.Fatal(err)
		}
		events, err := Generate(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if e.FastEnds && e.Greg.Month() == time.September && e.Greg.Day() == 14 {
				return e.TimeStr
			}
		}
		t.Fatal("no Fast ends on Tzom Gedaliah")
		return ""
	}
	newYork := func() *pb.Download {
		return &pb.Download{
			Year: 2026, Month: 9, MinorFast: true, GeoPos: true,
			LatOneof:  &pb.Download_Latitude{Latitude: 40.71427},
			LongOneof: &pb.Download_Longitude{Longitude: -74.00597},
			Tzid:      "America/New_York",
		}
	}
	if got := fastEnds(newYork()); got != "7:40p" {
		t.Errorf("default Fast ends = %q, want 7:40p", got)
	}
	msg := newYork()
	msg.FastEndMins = 45
	if got := fastEnds(msg); got != "7:52p" {
		t.Errorf("fem=45 Fast ends = %q, want 7:52p", got)
	}
}
