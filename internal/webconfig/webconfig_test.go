package webconfig

import (
	"strings"
	"testing"
)

func TestFilesDecode(t *testing.T) {
	if len(Learning()) == 0 || len(StaticCalendars()) == 0 {
		t.Fatalf("Learning() = %d entries, StaticCalendars() = %d", len(Learning()), len(StaticCalendars()))
	}
	for i, c := range StaticCalendars() {
		if c.DownloadSlug == "" || c.Title == "" || c.Locale == "" || c.Years == 0 {
			t.Errorf("staticCalendars.json entry %d (%q) is missing a slug, title, locale or years",
				i, c.DownloadSlug)
		}
	}
}

// A staticCalendars.json key is a calendar() option unless the website alone
// reads it, so one this service does not know must stop the build rather than
// leave a feed without the events it asks for.
func TestStaticCalendarsRejectUnknownOptions(t *testing.T) {
	_, err := decode[StaticCalendar]([]byte(`[{"downloadSlug":"x","molad":true}]`), staticWebsiteOnly)
	if err == nil || !strings.Contains(err.Error(), "molad") {
		t.Errorf("decode with an unknown option: err = %v", err)
	}
	got, err := decode[StaticCalendar]([]byte(`[{"downloadSlug":"x","ordinal":3,"descLong":"<p>"}]`), staticWebsiteOnly)
	if err != nil || len(got) != 1 || got[0].DownloadSlug != "x" {
		t.Errorf("decode with website-only keys = %v, %v", got, err)
	}
}

// Keys dailyLearningConfig.json adds for the website do not concern this
// service, and must not break it.
func TestLearningIgnoresUnknownKeys(t *testing.T) {
	got, err := decode[LearningSeries]([]byte(`[{"eventCategory":"x","somethingNew":1,"downloadSlug":null}]`), nil)
	if err != nil || len(got) != 1 || got[0].EventCategory != "x" || got[0].DownloadSlug != "" {
		t.Errorf("decode = %v, %v", got, err)
	}
}

func TestSeriesKey(t *testing.T) {
	for _, tt := range []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"dafyomi"}, "dafyomi"},
		{[]string{"yerushalmi", "vilna"}, "yerushalmi-vilna"},
		{[]string{"yerushalmi", "schottenstein"}, "yerushalmi-schottenstein"},
		{[]string{"holiday", "major"}, "holiday"},
	} {
		if got := SeriesKey(tt.in); got != tt.want {
			t.Errorf("SeriesKey(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFeedDesc(t *testing.T) {
	s := LearningSeries{DescMedium: "short"}
	if s.FeedDesc() != "short" {
		t.Error(s.FeedDesc())
	}
	s.CalDesc = "long"
	if s.FeedDesc() != "long" {
		t.Error(s.FeedDesc())
	}
}
