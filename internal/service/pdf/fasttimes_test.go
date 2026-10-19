package pdf

import (
	"testing"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/service/download"
	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// Generate applies the fast times to the calendar it draws.
func TestGenerateRetimesFasts(t *testing.T) {
	fastEnds := func(msg *pb.Download) string {
		t.Helper()
		p, err := download.ParamsFromMessage(msg, nil)
		if err != nil {
			t.Fatal(err)
		}
		events, err := Generate(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if e.Subject == "Fast ends" && e.Greg.Month() == time.September && e.Greg.Day() == 14 {
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
