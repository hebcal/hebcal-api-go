package pdf

import (
	"testing"

	"github.com/hebcal/hebcal-go/dailylearning"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	_ "github.com/hebcal/learning"

	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// Every name in learningSchedules has to exist in the registry, or the option
// is silently ignored and the calendar comes out missing rows the user asked
// for. The registry is populated by importing github.com/hebcal/learning for
// its side effects.
func TestLearningScheduleNamesAreRegistered(t *testing.T) {
	for _, s := range learningSchedules {
		if !dailylearning.Has(s.name) {
			t.Errorf("%q is not registered; registered names are %v",
				s.name, dailylearning.GetCalendars())
		}
	}
}

// Each schedule must actually produce events, which catches a name that is
// registered but wired to the wrong protobuf field.
//
// Over a whole year rather than one month: Pirkei Avot is read only between
// Pesach and Rosh Hashana, so a winter month would find nothing for it.
func TestEachLearningScheduleGeneratesEvents(t *testing.T) {
	for _, s := range learningSchedules {
		t.Run(s.name, func(t *testing.T) {
			opts := hebcal.CalOptions{
				Year: 2026, NoHolidays: true,
				DailyLearning: []string{s.name},
			}
			evs, err := hebcal.HebrewCalendar(&opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(evs) == 0 {
				t.Errorf("%q produced no events in 2026", s.name)
			}
			// Since github.com/hebcal/learning v0.5.0 the schedule events
			// implement event.URLer, so the in-process rows carry the same
			// Sefaria (or dafyomi.org) links production draws over them. Before
			// v0.5.0 none of these thirteen series had URLs.
			//
			// Two carry none, matching the TypeScript implementation:
			// yerushalmi-schottenstein has no Sefaria mapping at all, and
			// rambam3's multi-reading days keep their links in the memo rather
			// than a single event URL (single-reading days still get one). So
			// the invariant is "at least one URL", except for schottenstein.
			var withURL int
			for _, ev := range evs {
				if event.URL(ev) != "" {
					withURL++
				}
			}
			if withURL == 0 && s.name != "yerushalmi-schottenstein" {
				t.Errorf("%q produced %d events but none carry a URL", s.name, len(evs))
			}
		})
	}
}

// The protobuf field each schedule is wired to must be the right one.
func TestLearningFieldsMapToTheRightSchedules(t *testing.T) {
	tests := []struct {
		name string
		msg  *pb.Download
		want string
	}{
		{"daf yomi", &pb.Download{Year: 2026, Dafyomi: true}, "dafYomi"},
		{"mishna yomi", &pb.Download{Year: 2026, MishnaYomi: true}, "mishnaYomi"},
		{"nach yomi", &pb.Download{Year: 2026, NachYomi: true}, "nachYomi"},
		{"yerushalmi vilna", &pb.Download{Year: 2026, YerushalmiYomi: true}, "yerushalmi-vilna"},
		{"yerushalmi schottenstein", &pb.Download{Year: 2026, YySchottenstein: true}, "yerushalmi-schottenstein"},
		{"perek yomi", &pb.Download{Year: 2026, PerekYomi: true}, "perekYomi"},
		{"daf weekly", &pb.Download{Year: 2026, DafWeekly: true}, "dafWeeklySunday"},
		{"929", &pb.Download{Year: 2026, Nine29: true}, "929"},
		{"psalms", &pb.Download{Year: 2026, Psalms: true}, "psalms"},
		{"rambam 1", &pb.Download{Year: 2026, Rambam1: true}, "rambam1"},
		{"rambam 3", &pb.Download{Year: 2026, Rambam3: true}, "rambam3"},
		{"tanakh yomi", &pb.Download{Year: 2026, TanakhYomi: true}, "tanakhYomi"},
		{"pirkei avot", &pb.Download{Year: 2026, PirkeiAvotSummer: true}, "pirkeiAvotSummer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := DecodeParams(encode(t, tt.msg), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Opts.DailyLearning) != 1 || p.Opts.DailyLearning[0] != tt.want {
				t.Errorf("DailyLearning = %v, want [%s]", p.Opts.DailyLearning, tt.want)
			}
			if got := unsupportedSeries(tt.msg); len(got) != 0 {
				t.Errorf("should be supported, but unsupportedSeries() = %v", got)
			}
		})
	}
}

// The six with no schedule in the learning package must still be reported, so
// the handler hands those requests back to Node rather than rendering a
// calendar missing rows.
func TestRemainingUnsupportedSeries(t *testing.T) {
	msg := &pb.Download{
		Year: 2026, ChofetzChaim: true, ShemiratHaLashon: true,
		ArukhHaShulchanYomi: true, SeferHaMitzvot: true,
		KitzurShulchanAruch: true, DirshuAmudYomi: true,
	}
	if got := unsupportedSeries(msg); len(got) != 6 {
		t.Errorf("unsupportedSeries() = %v, want all six", got)
	}
}
