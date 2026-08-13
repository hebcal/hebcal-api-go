package pdf

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// encode builds the base64 protobuf payload a /v4/ URL carries.
func encode(t *testing.T, msg *pb.Download) string {
	t.Helper()
	raw, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("+", "-", "/", "_", "=", "").Replace(
		base64.StdEncoding.EncodeToString(raw))
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{"well formed", "/v4/AbCd/hebcal_2026.pdf", "AbCd", false},
		{"not a pdf", "/v4/AbCd/hebcal_2026.ics", "", true},
		{"wrong prefix", "/v3/AbCd/hebcal_2026.pdf", "", true},
		{"missing filename", "/v4/AbCd", "", true},
		{"empty payload", "/v4//hebcal_2026.pdf", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParsePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// hebcal-web writes these with Node's Buffer.from(s, 'base64'), which accepts
// both alphabets with or without padding.
func TestDecodeBase64AcceptsBothAlphabets(t *testing.T) {
	// "hello world" in standard base64 is aGVsbG8gd29ybGQ= .
	for _, in := range []string{"aGVsbG8gd29ybGQ=", "aGVsbG8gd29ybGQ"} {
		got, err := decodeBase64(in)
		if err != nil {
			t.Fatalf("decodeBase64(%q) error: %v", in, err)
		}
		if string(got) != "hello world" {
			t.Errorf("decodeBase64(%q) = %q", in, got)
		}
	}
}

// Only Hebrew locales lay out right to left; the transliterated ones are Latin
// script and must not.
func TestRTLDetection(t *testing.T) {
	tests := []struct {
		lg  string
		rtl bool
	}{
		{"h", true},
		{"he", true},
		{"s", false},
		{"a", false},
		{"de", false},
	}
	for _, tt := range tests {
		msg := &pb.Download{Locale: tt.lg, Year: 2026}
		p, err := DecodeParams(encode(t, msg), nil)
		if err != nil {
			t.Fatalf("lg=%q: %v", tt.lg, err)
		}
		if p.RTL != tt.rtl {
			t.Errorf("lg=%q: RTL = %v, want %v", tt.lg, p.RTL, tt.rtl)
		}
	}
}

// The protobuf carries "show this" booleans; CalOptions carries "suppress
// this" ones. Getting the inversion wrong silently empties a calendar.
func TestSuppressionFlagsAreInverted(t *testing.T) {
	all := &pb.Download{
		Major: true, Minor: true, RoshChodesh: true, Modern: true,
		MinorFast: true, SpecialShabbat: true, Year: 2026,
	}
	p, err := DecodeParams(encode(t, all), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.NoHolidays || p.Opts.NoRoshChodesh || p.Opts.NoModern ||
		p.Opts.NoMinorFast || p.Opts.NoSpecialShabbat || p.NoMinorHolidays {
		t.Errorf("everything requested, but something is suppressed: %+v", p.Opts)
	}

	none := &pb.Download{Year: 2026}
	p, err = DecodeParams(encode(t, none), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Opts.NoHolidays || !p.Opts.NoRoshChodesh || !p.NoMinorHolidays {
		t.Errorf("nothing requested, but something is not suppressed: %+v", p.Opts)
	}
}

func TestMonthMode(t *testing.T) {
	tests := []struct {
		mm        pb.Download_MonthMode
		want      MonthMode
		gematriya bool
	}{
		{pb.Download_GREGORIAN_ARABIC, GregorianArabic, false},
		{pb.Download_HEBREW_ARABIC, HebrewArabic, false},
		{pb.Download_HEBREW_HEBREW, HebrewHebrew, true},
	}
	for _, tt := range tests {
		p, err := DecodeParams(encode(t, &pb.Download{MonthMode: tt.mm, Year: 2026}), nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.MonthMode != tt.want {
			t.Errorf("MonthMode = %v, want %v", p.MonthMode, tt.want)
		}
		if p.useGematriya() != tt.gematriya {
			t.Errorf("mm=%v: useGematriya() = %v, want %v", tt.mm, p.useGematriya(), tt.gematriya)
		}
	}
}

// A start/end pair wins over year, and the epoch-seconds form is equivalent to
// the string form.
func TestDateRange(t *testing.T) {
	byStr := &pb.Download{
		StartOneof: &pb.Download_StartStr{StartStr: "2026-08-01"},
		EndOneof:   &pb.Download_EndStr{EndStr: "2026-08-31"},
	}
	p, err := DecodeParams(encode(t, byStr), nil)
	if err != nil {
		t.Fatal(err)
	}
	gy, gm, gd := p.Opts.Start.ProlepticGreg()
	if gy != 2026 || int(gm) != 8 || gd != 1 {
		t.Errorf("start = %d-%02d-%02d, want 2026-08-01", gy, int(gm), gd)
	}

	epoch := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).Unix()
	byEpoch := &pb.Download{
		StartOneof: &pb.Download_Start{Start: epoch},
		EndOneof:   &pb.Download_End{End: epoch + 30*86400},
	}
	p2, err := DecodeParams(encode(t, byEpoch), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Opts.Start.Abs() != p.Opts.Start.Abs() {
		t.Errorf("epoch-seconds start does not match the string form")
	}
}

func TestEndBeforeStartIsRejected(t *testing.T) {
	msg := &pb.Download{
		StartOneof: &pb.Download_StartStr{StartStr: "2026-08-31"},
		EndOneof:   &pb.Download_EndStr{EndStr: "2026-08-01"},
	}
	if _, err := DecodeParams(encode(t, msg), nil); err == nil {
		t.Error("expected an error when the end date precedes the start")
	}
}

// A multi-year calendar is one page per month, so an unbounded numYears is a
// cheap way to tie up the process.
func TestNumYearsIsBounded(t *testing.T) {
	msg := &pb.Download{Year: 2026, NumYears: 5000}
	p, err := DecodeParams(encode(t, msg), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.NumYears > maxNumYears {
		t.Errorf("NumYears = %d, want it clamped to %d", p.Opts.NumYears, maxNumYears)
	}
}

// Candle-lighting without a location is dropped, not rejected: src/calendar.js
// deletes options.candlelighting when getLocationFromQuery returns nothing, so
// the request still renders -- just with no times -- rather than erroring.
func TestCandleLightingWithoutLocationIsDropped(t *testing.T) {
	msg := &pb.Download{Candlelighting: true, Year: 2026}
	p, err := DecodeParams(encode(t, msg), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.CandleLighting {
		t.Error("candle-lighting should be dropped when no location is given")
	}
}

func TestGeoPosLocation(t *testing.T) {
	msg := &pb.Download{
		Candlelighting: true, GeoPos: true, Year: 2026,
		LatOneof:  &pb.Download_Latitude{Latitude: 37.44},
		LongOneof: &pb.Download_Longitude{Longitude: -122.14},
		Tzid:      "America/Los_Angeles",
		CityName:  "Palo Alto",
	}
	p, err := DecodeParams(encode(t, msg), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.Location == nil {
		t.Fatal("Location is nil")
	}
	if p.Opts.Location.TimeZoneId != "America/Los_Angeles" {
		t.Errorf("tzid = %q", p.Opts.Location.TimeZoneId)
	}
}

// A lat/long location without a timezone cannot be turned into clock times.
func TestGeoPosWithoutTzidIsRejected(t *testing.T) {
	msg := &pb.Download{
		Candlelighting: true, GeoPos: true, Year: 2026,
		LatOneof:  &pb.Download_Latitude{Latitude: 37.44},
		LongOneof: &pb.Download_Longitude{Longitude: -122.14},
	}
	if _, err := DecodeParams(encode(t, msg), nil); err == nil {
		t.Error("expected an error for a geoPos location with no tzid")
	}
}

// Requested series are enabled through CalOptions' generic DailyLearning
// list, which hebcal-go resolves against the registry, and the ones with no
// schedule are reported so the handler can hand the request back to Node.
func TestDailyLearning(t *testing.T) {
	supported := &pb.Download{
		Year: 2026, Dafyomi: true, MishnaYomi: true, NachYomi: true,
		YerushalmiYomi: true, Rambam1: true, TanakhYomi: true, Nine29: true,
	}
	p, err := DecodeParams(encode(t, supported), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Opts.DailyLearning) != 7 {
		t.Errorf("DailyLearning = %v, want all seven", p.Opts.DailyLearning)
	}
	if got := unsupportedSeries(supported); len(got) != 0 {
		t.Errorf("unsupportedSeries() = %v, want none", got)
	}

	// These have no schedule in github.com/hebcal/learning.
	unsupported := &pb.Download{Year: 2026, ChofetzChaim: true, SeferHaMitzvot: true}
	got := unsupportedSeries(unsupported)
	if len(got) != 2 {
		t.Errorf("unsupportedSeries() = %v, want two entries", got)
	}
}

// Tzeit-based havdalah uses degrees; a fixed offset uses minutes. Setting both
// would make the two disagree.
func TestHavdalahModes(t *testing.T) {
	mins := &pb.Download{Year: 2026, HavdalahMins: 50}
	p, err := DecodeParams(encode(t, mins), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.HavdalahMins != 50 || p.Opts.HavdalahDeg != 0 {
		t.Errorf("minutes mode: mins=%d deg=%v", p.Opts.HavdalahMins, p.Opts.HavdalahDeg)
	}

	tzeit := &pb.Download{Year: 2026, HavdalahTzeit: true, Tzeit: 7.083}
	p, err = DecodeParams(encode(t, tzeit), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.HavdalahMins != 0 {
		t.Errorf("tzeit mode should leave HavdalahMins zero, got %d", p.Opts.HavdalahMins)
	}
	if p.Opts.HavdalahDeg == 0 {
		t.Error("tzeit mode should set HavdalahDeg")
	}
}

// Asking for Rosh Chodesh, the special Shabbatot and the weekly Torah reading
// together implies Shabbat Mevarchim. hebcal-web does this by setting the
// SHABBAT_MEVARCHIM bit in its query mask, which @hebcal/core turns back into
// options.shabbatMevarchim; without it a Hebrew calendar is missing the
// "Mevarchim Chodesh" line the Node service prints.
func TestShabbatMevarchimIsImplied(t *testing.T) {
	tests := []struct {
		name                 string
		rc, ss, sedrot, mvch bool
		want                 bool
	}{
		{name: "all three imply it", rc: true, ss: true, sedrot: true, want: true},
		{name: "asked for directly", mvch: true, want: true},
		{name: "missing Rosh Chodesh", ss: true, sedrot: true, want: false},
		{name: "missing special Shabbat", rc: true, sedrot: true, want: false},
		{name: "missing Torah readings", rc: true, ss: true, want: false},
		{name: "none of them", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &pb.Download{
				Year: 2029, RoshChodesh: tt.rc, SpecialShabbat: tt.ss,
				Sedrot: tt.sedrot, ShabbatMevarchim: tt.mvch,
			}
			p, err := DecodeParams(encode(t, msg), nil)
			if err != nil {
				t.Fatal(err)
			}
			if p.Opts.ShabbatMevarchim != tt.want {
				t.Errorf("ShabbatMevarchim = %v, want %v", p.Opts.ShabbatMevarchim, tt.want)
			}
		})
	}
}

// The protobuf's shabbatMevarchim field was previously wired to CalOptions.Molad,
// which generates a different event entirely.
func TestShabbatMevarchimDoesNotEnableMolad(t *testing.T) {
	msg := &pb.Download{Year: 2029, ShabbatMevarchim: true}
	p, err := DecodeParams(encode(t, msg), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.Molad {
		t.Error("Molad should not be enabled by the shabbatMevarchim field")
	}
}

// A location implies candle-lighting, even when the request did not ask for it:
// src/calendar.js sets options.candlelighting = true for any resolved location.
// This is the hebcal_2008_prestea bug -- a calendar with a location but no c=on
// came out with 133 times in production and none here. Ghana is after 1900, so
// the early-year cutoff does not apply.
func TestLocationImpliesCandleLighting(t *testing.T) {
	msg := &pb.Download{
		Year: 2008, Major: true, GeoPos: true,
		LatOneof:  &pb.Download_Latitude{Latitude: 5.43},
		LongOneof: &pb.Download_Longitude{Longitude: -2.14},
		Tzid:      "Africa/Accra",
		CityName:  "Prestea",
		// Candlelighting deliberately unset.
	}
	p, err := DecodeParams(encode(t, msg), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Opts.CandleLighting {
		t.Error("a resolved location should imply candle-lighting")
	}
	if p.Opts.Location == nil {
		t.Fatal("location should be resolved")
	}
	if p.CityName != "Prestea" {
		t.Errorf("CityName = %q, want Prestea", p.CityName)
	}
}

// A calendar with no location at all is still valid, and names itself by
// schedule.
func TestNoLocationIsFineWithoutCandleLighting(t *testing.T) {
	p, err := DecodeParams(encode(t, &pb.Download{Year: 2026, Major: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Opts.Location != nil {
		t.Error("no location was given, so none should be set")
	}
	if got := leftFooterText(p); got != "Diaspora holiday schedule" {
		t.Errorf("footer = %q", got)
	}
}
