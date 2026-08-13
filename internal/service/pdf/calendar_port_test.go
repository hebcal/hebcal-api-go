package pdf

import (
	"errors"
	"testing"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"

	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// These cover the derivations ported from hebcal-web's src/calendar.js
// (makeHebcalOptions) that the protobuf does not carry: a location implies
// candle-lighting, Israel forces its own offset, early years drop times, the
// 12/24-hour default follows the country, and lg=ah/sh append the Hebrew name.

// A location before 1900 (Gregorian) or 5661 (Hebrew) renders without candle
// times even though the location itself implies them.
func TestEarlyYearDisablesCandleLighting(t *testing.T) {
	base := func(year int32, hebrew bool) *pb.Download {
		return &pb.Download{
			Year: year, IsHebrewYear: hebrew, Major: true, GeoPos: true,
			LatOneof:  &pb.Download_Latitude{Latitude: 5.43},
			LongOneof: &pb.Download_Longitude{Longitude: -2.14},
			Tzid:      "Africa/Accra",
			CityName:  "Prestea",
		}
	}
	tests := []struct {
		name   string
		msg    *pb.Download
		wantCL bool
	}{
		{"1899 Gregorian off", base(1899, false), false},
		{"1900 Gregorian on", base(1900, false), true},
		{"5660 Hebrew off", base(5660, true), false},
		{"5661 Hebrew on", base(5661, true), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := DecodeParams(encode(t, tt.msg), nil)
			if err != nil {
				t.Fatal(err)
			}
			if p.Opts.CandleLighting != tt.wantCL {
				t.Errorf("CandleLighting = %v, want %v", p.Opts.CandleLighting, tt.wantCL)
			}
		})
	}
}

// An Israel location forces the Israel schedule and its default 20-minute
// candle-lighting offset, whether Israel is signalled by the flag or by the
// Asia/Jerusalem timezone.
func TestIsraelLocationForcesILAndOffset(t *testing.T) {
	tests := []struct {
		name string
		msg  *pb.Download
	}{
		{"via tzid", &pb.Download{
			Year: 2026, Major: true, GeoPos: true,
			LatOneof:  &pb.Download_Latitude{Latitude: 31.78},
			LongOneof: &pb.Download_Longitude{Longitude: 35.23},
			Tzid:      "Asia/Jerusalem", CityName: "Jerusalem",
		}},
		{"via israel flag", &pb.Download{
			Year: 2026, Major: true, GeoPos: true, Israel: true,
			LatOneof:  &pb.Download_Latitude{Latitude: 31.78},
			LongOneof: &pb.Download_Longitude{Longitude: 35.23},
			Tzid:      "Etc/GMT-2", CityName: "Somewhere",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := DecodeParams(encode(t, tt.msg), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !p.Opts.IL {
				t.Error("IL should be forced for an Israel location")
			}
			if p.Opts.CandleLightingMins != 20 {
				t.Errorf("CandleLightingMins = %d, want 20", p.Opts.CandleLightingMins)
			}
		})
	}
}

// A non-default candle-lighting offset is respected even for an Israel location.
func TestIsraelLocationKeepsCustomOffset(t *testing.T) {
	msg := &pb.Download{
		Year: 2026, Major: true, GeoPos: true,
		LatOneof:  &pb.Download_Latitude{Latitude: 31.78},
		LongOneof: &pb.Download_Longitude{Longitude: 35.23},
		Tzid:      "Asia/Jerusalem", CityName: "Jerusalem",
		CandleLightingMins: 30,
	}
	p, err := DecodeParams(encode(t, msg), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.CandleLightingMins != 30 {
		t.Errorf("CandleLightingMins = %d, want the requested 30", p.Opts.CandleLightingMins)
	}
}

// deserializeDownload.js sets q.m = havdalahMins when M=off, and makeHebcalOptions
// leaves options.havdalahMins === 0 there, which @hebcal/core reads as "no
// Havdalah". hebcal-go reads a zero HavdalahMins as "use the default tzeit"
// instead, so DecodeParams sets SuppressHavdalah to reproduce the suppression. A
// non-default offset or tzeit keeps Havdalah.
func TestSuppressHavdalah(t *testing.T) {
	tests := []struct {
		name         string
		msg          *pb.Download
		wantSuppress bool
		wantMins     int
	}{
		{"default m=0 suppresses", &pb.Download{Year: 2026, Major: true, Candlelighting: true}, true, 0},
		{"m=50 keeps havdalah", &pb.Download{Year: 2026, Major: true, Candlelighting: true, HavdalahMins: 50}, false, 50},
		{"tzeit (M=on) keeps havdalah", &pb.Download{Year: 2026, Major: true, Candlelighting: true, HavdalahTzeit: true}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := DecodeParams(encode(t, tt.msg), nil)
			if err != nil {
				t.Fatal(err)
			}
			if p.Opts.SuppressHavdalah != tt.wantSuppress {
				t.Errorf("SuppressHavdalah = %v, want %v", p.Opts.SuppressHavdalah, tt.wantSuppress)
			}
			if p.Opts.HavdalahMins != tt.wantMins {
				t.Errorf("HavdalahMins = %d, want %d", p.Opts.HavdalahMins, tt.wantMins)
			}
		})
	}
}

// The 12/24-hour default follows the location's country: only the dozen listed
// countries default to 12-hour, and hour12 overrides either way.
func TestUse12Hour(t *testing.T) {
	mk := func(cc string, il bool, hour12 int32) *Params {
		p := &Params{Hour12: hour12}
		p.Opts.IL = il
		if cc != "" {
			p.Opts.Location = &zmanim.Location{CountryCode: cc}
		}
		return p
	}
	tests := []struct {
		name string
		p    *Params
		want bool
	}{
		{"US defaults 12h", mk("US", false, 0), true},
		{"Ghana defaults 24h", mk("GH", false, 0), false},
		{"Israel defaults 24h", mk("IL", true, 0), false},
		{"no location, il -> 24h", mk("", true, 0), false},
		{"no location, diaspora -> 12h (US)", mk("", false, 0), true},
		{"force 12h wins over Ghana", mk("GH", false, 1), true},
		{"force 24h wins over US", mk("US", false, 2), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.use12Hour(); got != tt.want {
				t.Errorf("use12Hour() = %v, want %v", got, tt.want)
			}
		})
	}
}

// lg=ah and lg=sh, and only those, append the Hebrew name to each subject.
func TestAppendHebrewLocales(t *testing.T) {
	tests := map[string]bool{
		"ah": true, "sh": true,
		"a": false, "s": false, "h": false, "de": false, "": false,
	}
	for lg, want := range tests {
		p, err := DecodeParams(encode(t, &pb.Download{Year: 2026, Major: true, Locale: lg}), nil)
		if err != nil {
			t.Fatalf("lg=%q: %v", lg, err)
		}
		if p.AppendHebrew != want {
			t.Errorf("lg=%q: AppendHebrew = %v, want %v", lg, p.AppendHebrew, want)
		}
	}
}

// altDateBrief drops the year, except for Rosh Hashana, and fixes the Tamuz
// spelling.
func TestAltDateBrief(t *testing.T) {
	tests := []struct {
		name   string
		hd     hdate.HDate
		locale string
		want   string
	}{
		{"english drops year", hdate.New(5770, hdate.Sivan, 19), "ashkenazi", "19th of Sivan"},
		{"tamuz spelling", hdate.New(5770, hdate.Tamuz, 1), "en", "1st of Tamuz"},
		{"rosh hashana keeps year", hdate.New(5770, hdate.Tishrei, 1), "en", "1st of Tishrei, 5770"},
		{"spanish ordinal", hdate.New(5786, hdate.Tevet, 12), "es", "12º Tevet"},
		{"hebrew gematriya", hdate.New(5786, hdate.Tevet, 12), "he", "י״ב טֵבֵת"},
		{"german keeps hebcal-go's ordinal", hdate.New(5786, hdate.Tevet, 12), "de", "12 Tewet"},
		{"smart apostrophe", hdate.New(5786, hdate.Shvat, 1), "en", "1st of Sh’vat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := altDateBrief(tt.hd, tt.locale); got != tt.want {
				t.Errorf("altDateBrief = %q, want %q", got, tt.want)
			}
		})
	}
}

// Years outside hebcal-web's supported range yield an OutOfRangeError, which the
// handler turns into 410 Gone, and never reach the generator.
func TestYearRange(t *testing.T) {
	tests := []struct {
		name    string
		msg     *pb.Download
		wantErr bool
	}{
		{"year 9999 gone", &pb.Download{Year: 9999, Major: true}, true},
		{"year 50 gone", &pb.Download{Year: 50, Major: true}, true},
		{"year 2999 ok", &pb.Download{Year: 2999, Major: true}, false},
		{"year 100 ok", &pb.Download{Year: 100, Major: true}, false},
		{"hebrew 3859 gone", &pb.Download{Year: 3859, IsHebrewYear: true, Major: true}, true},
		{"hebrew 5800 ok", &pb.Download{Year: 5800, IsHebrewYear: true, Major: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeParams(encode(t, tt.msg), nil)
			var oor *OutOfRangeError
			if tt.wantErr {
				if !errors.As(err, &oor) {
					t.Errorf("err = %v, want an OutOfRangeError", err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// gregorianAltText renders the Gregorian date a Hebrew-month calendar shows on
// its day-number line: "MMM D" in English, "D MMM" (localized) elsewhere, and no
// year, matching GregorianDateEvent.render().
func TestGregorianAltText(t *testing.T) {
	hd := hdate.New(5786, hdate.Cheshvan, 1) // 2025-10-23
	tests := map[string]string{
		"en":        "Oct 23",
		"ashkenazi": "Oct 23",
		"he":        "23 אוק",
	}
	for locale, want := range tests {
		if got := gregorianAltText(hd, locale); got != want {
			t.Errorf("gregorianAltText(%q) = %q, want %q", locale, got, want)
		}
	}
}

// In Hebrew-month mode, addAltDates inserts a Gregorian alternate date for every
// day in the event range, while addAltDatesForEvents inserts one only for days
// that already have events. Both mark the events AltDate so they render on the
// day-number line rather than as rows, and the list stays in date order.
func TestAddGregorianAltDates(t *testing.T) {
	mk := func(day int) Event {
		hd := hdate.New(5786, hdate.Cheshvan, day)
		return Event{HD: hd, Greg: hd.Gregorian()}
	}
	events := []Event{mk(1), mk(1), mk(4)} // two events on the 1st, one on the 4th

	forEvents := addGregorianAltDates(events, &Params{MonthMode: HebrewArabic, AddAltDatesForEvents: true})
	if got := countAlt(forEvents); got != 2 {
		t.Errorf("addAltDatesForEvents: %d alt dates, want 2 (one per event-day)", got)
	}

	all := addGregorianAltDates(events, &Params{MonthMode: HebrewArabic, AddAltDates: true})
	if got := countAlt(all); got != 4 {
		t.Errorf("addAltDates: %d alt dates, want 4 (Cheshvan 1-4)", got)
	}
	// Date order is a precondition of SplitByHebrewMonth.
	for i := 1; i < len(all); i++ {
		if all[i-1].HD.Abs() > all[i].HD.Abs() {
			t.Fatalf("events out of date order at %d", i)
		}
	}
}

func countAlt(evs []Event) int {
	n := 0
	for i := range evs {
		if evs[i].AltDate {
			n++
		}
	}
	return n
}

// A resolved location that is not in Israel keeps the diaspora schedule and the
// default 18-minute offset.
func TestDiasporaLocationDefaults(t *testing.T) {
	msg := &pb.Download{
		Year: 2026, Major: true, GeoPos: true,
		LatOneof:  &pb.Download_Latitude{Latitude: 5.43},
		LongOneof: &pb.Download_Longitude{Longitude: -2.14},
		Tzid:      "Africa/Accra", CityName: "Prestea",
	}
	p, err := DecodeParams(encode(t, msg), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.IL {
		t.Error("a Ghana location should not force the Israel schedule")
	}
	if p.Opts.CandleLightingMins != 0 {
		t.Errorf("CandleLightingMins = %d, want 0 (default 18)", p.Opts.CandleLightingMins)
	}
	var _ hebcal.CalOptions = p.Opts
}
