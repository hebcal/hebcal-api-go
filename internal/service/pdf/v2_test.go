package pdf

import (
	"encoding/base64"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/hebcal/hebcal-api-go/internal/model"
	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// isNotFound and isBadRequest name the two statuses hebcal-web's download
// dispatcher answers a legacy URL it will not rewrite with.
func isNotFound(err error) bool {
	var nf *NotFoundError
	return errors.As(err, &nf)
}

func isBadRequest(err error) bool {
	var herr *model.HTTPError
	return errors.As(err, &herr) && herr.Status == http.StatusBadRequest
}

// v2Path wraps a legacy query string in the URL that carries it.
func v2Path(qs, filename string) string {
	return v2Prefix + strings.TrimRight(base64.StdEncoding.EncodeToString([]byte(qs)), "=") +
		"/" + filename
}

// decodeV2 is the whole legacy path in one call, for the table tests below.
func decodeV2(t *testing.T, qs string) *pb.Download {
	t.Helper()
	q, err := ParseV2Path(v2Path(qs, "hebcal.pdf"))
	if err != nil {
		t.Fatalf("ParseV2Path(%q): %v", qs, err)
	}
	msg, err := DecodeV2(q)
	if err != nil {
		t.Fatalf("DecodeV2(%q): %v", qs, err)
	}
	return msg
}

// The three URLs below are real requests taken from a download.hebcal.com
// access log, and the payloads they are checked against are what hebcal-web's
// downloadHref2() produces for them today -- the /v4/ URL its 301 points at.
// Rendering the same protobuf is what makes serving these with a 200 the same
// calendar rather than merely a similar one.
func TestDecodeV2MatchesProductionRedirect(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string // the /v4/ payload hebcal-web redirects to
	}{
		{
			"geonameid with a havdalah offset",
			"/v2/h/dj0xJmdlb25hbWVpZD0zNTg0MDAzJm09NTAmeWVhcj0yMDIxJmM9MSZzPTEmbWFqPTEmbWluPTEmbW9kPTEmbWY9MSZzcz0xJm54PTE/hebcal_2021_Puerto_El_Triunfo.pdf",
			"CAEQARgBIAEoATABUAFYg-DaAWDlD3AyiAEB",
		},
		{
			"M=1, the numeric spelling of havdalah at tzeit",
			"/v2/h/dj0xJmdlb25hbWVpZD0yMDE5OTQ1Jk09MSZsZz1zJnllYXI9MjAyMSZjPTEmcz0xJm1haj0xJm1pbj0xJm1vZD0xJm1mPTEmc3M9MSZueD0x/hebcal_2021_Mishelevka.pdf",
			"CAEQARgBIAEoATABQAFQAVjppHtg5Q9qAXOIAQE",
		},
		{
			// The base64 here is 103 characters, so its last group is short a
			// character; Node's Buffer.from decodes the partial group rather
			// than dropping it, and so must decodeBase64, or the trailing nx=1
			// is lost and the calendar comes out without Rosh Chodesh.
			"unpadded base64 with a partial final group",
			"/v2/h/dj0xJmdlb25hbWVpZD0xODk3MTE4Jk09MSZsZz1zJnllYXI9MjAyMSZjPTEmcz0xJm1haj0xJm1pbj0xJm1vZD0xJm1mPTEmc3M9MSZueD0x/hebcal_2021_Hwado.pdf",
			"CAEQARgBIAEoATABQAFQAVie5XNg5Q9qAXOIAQE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := ParseV2Path(tt.path)
			if err != nil {
				t.Fatalf("ParseV2Path: %v", err)
			}
			got, err := DecodeV2(q)
			if err != nil {
				t.Fatalf("DecodeV2: %v", err)
			}
			raw, err := decodeBase64(tt.want)
			if err != nil {
				t.Fatal(err)
			}
			var want pb.Download
			if err := proto.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(&want, got) {
				t.Errorf("legacy URL decodes to\n  %v\nwant\n  %v", got, &want)
			}
		})
	}
}

func TestParseV2Path(t *testing.T) {
	// "v=1" is dmVsMQ.. -- spell the payload out rather than computing it, so
	// the shape being tested is visible.
	const data = "dj0x"
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"well formed", v2Prefix + data + "/hebcal_2026.pdf", false},
		{"an ics feed, not ours", v2Prefix + data + "/hebcal.ics", true},
		// parseV2Path() defaults a filename-less path to hebcal.ics.
		{"no filename", v2Prefix + data, true},
		{"empty payload", v2Prefix + "/hebcal_2026.pdf", true},
		{"a yahrzeit calendar", "/v2/y/" + data + "/yahrzeit.pdf", true},
		{"not base64", v2Prefix + "!!!!/hebcal_2026.pdf", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := ParseV2Path(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseV2Path(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if err == nil && q["v"] != "1" {
				t.Errorf("ParseV2Path(%q) query = %v", tt.path, q)
			}
		})
	}
}

// redirV2 only rewrites a URL whose v=1; everything else reaches the download
// dispatcher, which answers a missing v with 404 and any other value with 400.
func TestDecodeV2RequiresV1(t *testing.T) {
	tests := []struct {
		name string
		qs   string
		is   func(error) bool
	}{
		{"no v at all", "geonameid=5128581&year=2026&maj=on", isNotFound},
		{"a yahrzeit calendar", "v=yahrzeit&y1=1990&m1=5&d1=5", isBadRequest},
		{"some other version", "v=2&year=2026&maj=on", isBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := ParseV2Path(v2Path(tt.qs, "hebcal.pdf"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeV2(q)
			if err == nil {
				t.Fatalf("DecodeV2(%q) = nil error", tt.qs)
			}
			if !tt.is(err) {
				t.Errorf("DecodeV2(%q) = %v, wrong error kind", tt.qs, err)
			}
		})
	}
}

// The `on()` these URLs are read with accepts both spellings, and a legacy URL
// uses the numeric one throughout.
func TestDecodeV2BooleanSpellings(t *testing.T) {
	numeric := decodeV2(t, "v=1&maj=1&min=1&nx=1&mod=1&mf=1&ss=1&c=1&s=1&i=1")
	word := decodeV2(t, "v=1&maj=on&min=on&nx=on&mod=on&mf=on&ss=on&c=on&s=on&i=on")
	if !proto.Equal(numeric, word) {
		t.Errorf("1 and on disagree:\n  %v\n  %v", numeric, word)
	}
	off := decodeV2(t, "v=1&maj=off&min=0&nx=off&mod=0&mf=off&ss=0&c=off&s=0&i=off")
	if off.GetMajor() || off.GetMinor() || off.GetRoshChodesh() || off.GetModern() ||
		off.GetMinorFast() || off.GetSpecialShabbat() || off.GetCandlelighting() ||
		off.GetSedrot() || off.GetIsrael() {
		t.Errorf("off/0 set an option: %v", off)
	}
}

// Sedrot has no line of its own in downloadHref2(): it rides in on the
// dailyLearningConfig loop, whose last entry maps `s` to it.
func TestDecodeV2Sedrot(t *testing.T) {
	if !decodeV2(t, "v=1&s=on").GetSedrot() {
		t.Error("s=on did not set sedrot")
	}
}

func TestDecodeV2DailyLearning(t *testing.T) {
	msg := decodeV2(t, "v=1&F=on&myomi=on&dpy=on&nyomi=on&dty=on&dps=on&d929=on&"+
		"dr1=on&dr3=on&dsm=on&yyomi=on&yys=on&dcc=on&dshl=on&ayd=on&dw=on&"+
		"dpa=on&ahsy=on&dksa=on")
	for name, got := range map[string]bool{
		"dafyomi": msg.GetDafyomi(), "mishnaYomi": msg.GetMishnaYomi(),
		"perekYomi": msg.GetPerekYomi(), "nachYomi": msg.GetNachYomi(),
		"tanakhYomi": msg.GetTanakhYomi(), "psalms": msg.GetPsalms(),
		"nine29": msg.GetNine29(), "rambam1": msg.GetRambam1(),
		"rambam3": msg.GetRambam3(), "seferHaMitzvot": msg.GetSeferHaMitzvot(),
		"yerushalmiYomi": msg.GetYerushalmiYomi(), "yySchottenstein": msg.GetYySchottenstein(),
		"chofetzChaim": msg.GetChofetzChaim(), "shemiratHaLashon": msg.GetShemiratHaLashon(),
		"dirshuAmudYomi": msg.GetDirshuAmudYomi(), "dafWeekly": msg.GetDafWeekly(),
		"pirkeiAvotSummer": msg.GetPirkeiAvotSummer(), "arukhHaShulchanYomi": msg.GetArukhHaShulchanYomi(),
		"kitzurShulchanAruch": msg.GetKitzurShulchanAruch(),
	} {
		if !got {
			t.Errorf("%s was not set", name)
		}
	}
}

// The four ways a legacy URL can name a Havdalah rule, as urlArgsObj()
// collapses them. The last is the interesting one: a URL that says nothing at
// all still means tzeit, because getInt(undefined) is null.
func TestDecodeV2Havdalah(t *testing.T) {
	tests := []struct {
		name      string
		qs        string
		wantTzeit bool
		wantMins  int32
		wantDeg   float32
	}{
		{"an explicit offset", "v=1&m=50", false, 50, 0},
		{"M=1, the numeric spelling", "v=1&M=1", true, 0, 0},
		{"M=on wins over any offset", "v=1&M=on&m=50", true, 0, 0},
		{"M=off with no offset is still tzeit", "v=1&M=off", true, 0, 0},
		{"nothing said at all", "v=1&year=2026", true, 0, 0},
		{"a custom depression angle", "v=1&td=7.083", true, 0, 7.083},
		// 8.5 degrees is the default, so it travels as an unset field.
		{"the default angle is not carried", "v=1&td=8.5", true, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := decodeV2(t, tt.qs)
			if msg.GetHavdalahTzeit() != tt.wantTzeit {
				t.Errorf("havdalahTzeit = %v, want %v", msg.GetHavdalahTzeit(), tt.wantTzeit)
			}
			if msg.GetHavdalahMins() != tt.wantMins {
				t.Errorf("havdalahMins = %d, want %d", msg.GetHavdalahMins(), tt.wantMins)
			}
			if msg.GetTzeit() != tt.wantDeg {
				t.Errorf("tzeit = %v, want %v", msg.GetTzeit(), tt.wantDeg)
			}
		})
	}
}

// `geo` names which of several location forms in the query is live, and
// getGeoKeysToRemove() drops the rest before downloadHref2 reads any of them.
func TestDecodeV2GeoKeysToRemove(t *testing.T) {
	tests := []struct {
		name         string
		qs           string
		wantGeoname  int32
		wantZip      string
		wantGeoPos   bool
		wantCityName string
	}{
		{
			"geo=geoname drops a stale lat/long",
			"v=1&geo=geoname&geonameid=4887398&latitude=41.85&longitude=-87.65&tzid=America/Chicago",
			4887398, "", false, "",
		},
		{
			"geo=zip drops the geonameid beside it",
			"v=1&geo=zip&zip=02138&geonameid=4887398",
			0, "02138", false, "",
		},
		{
			"geo=pos drops the named locations",
			"v=1&geo=pos&geonameid=4887398&zip=02138&latitude=37.44&longitude=-122.14&tzid=America/Los_Angeles&city-typeahead=Palo Alto",
			0, "", true, "Palo Alto",
		},
		{
			"geo=none drops every location and the times with it",
			"v=1&geo=none&geonameid=4887398&zip=02138&b=18&m=50&ue=on",
			0, "", false, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := decodeV2(t, tt.qs)
			if msg.GetGeonameid() != tt.wantGeoname {
				t.Errorf("geonameid = %d, want %d", msg.GetGeonameid(), tt.wantGeoname)
			}
			if msg.GetZip() != tt.wantZip {
				t.Errorf("zip = %q, want %q", msg.GetZip(), tt.wantZip)
			}
			if msg.GetGeoPos() != tt.wantGeoPos {
				t.Errorf("geoPos = %v, want %v", msg.GetGeoPos(), tt.wantGeoPos)
			}
			if msg.GetCityName() != tt.wantCityName {
				t.Errorf("cityName = %q, want %q", msg.GetCityName(), tt.wantCityName)
			}
		})
	}
}

// geo=none also strips the candle-lighting parameters, so a calendar that
// carried an offset comes out with none.
func TestDecodeV2GeoNoneDropsTimes(t *testing.T) {
	msg := decodeV2(t, "v=1&geo=none&b=40&m=50&ue=on&c=on")
	if msg.GetCandleLightingMins() != 0 || msg.GetHavdalahMins() != 0 || msg.GetUseElevation() {
		t.Errorf("geo=none kept a time parameter: %v", msg)
	}
}

// downloadHref2 has no branch for either of these, so hebcal-web's 301 hands
// /v4/ a calendar with no location -- and, since a location implies
// candle-lighting, no times. getLocationFromQuery does resolve both, and did so
// for these URLs before redirV2 existed, so both are supported here. See
// applyV2Location.
func TestDecodeV2LocationFormsTheRedirectDrops(t *testing.T) {
	t.Run("a legacy city identifier", func(t *testing.T) {
		// The name travels in cityName, which is free when geoPos is unset;
		// applyLocation resolves it through LookupLegacyCity.
		msg := decodeV2(t, "v=1&city=GB-London&year=2026&c=on&maj=on")
		if msg.GetCityName() != "GB-London" {
			t.Errorf("cityName = %q, want GB-London", msg.GetCityName())
		}
		if msg.GetGeoPos() {
			t.Error("a legacy city is not a geoPos location")
		}
	})

	t.Run("degrees, minutes and a direction", func(t *testing.T) {
		msg := decodeV2(t, "v=1&ladeg=40&lamin=42&ladir=n&lodeg=74&lomin=0&lodir=w&"+
			"tzid=America/New_York&year=2026&c=on&maj=on")
		if !msg.GetGeoPos() {
			t.Fatal("geoPos was not set")
		}
		if got := msg.GetLatitude(); math.Abs(float64(got)-40.7) > 1e-5 {
			t.Errorf("latitude = %v, want 40.7", got)
		}
		// West and south are a positive magnitude plus a direction letter,
		// where the decimal form would use a negative number.
		if got := msg.GetLongitude(); math.Abs(float64(got)+74) > 1e-5 {
			t.Errorf("longitude = %v, want -74", got)
		}
		if msg.GetTzid() != "America/New_York" {
			t.Errorf("tzid = %q", msg.GetTzid())
		}
		// With no city-typeahead the name is the coordinates themselves.
		if !strings.Contains(msg.GetCityName(), "40") {
			t.Errorf("cityName = %q, want the degrees/minutes rendering", msg.GetCityName())
		}
	})

	t.Run("the legacy numeric timezone", func(t *testing.T) {
		// tz + dst instead of a tzid, the form that predates IANA names.
		msg := decodeV2(t, "v=1&ladeg=31&lamin=46&ladir=n&lodeg=35&lomin=13&lodir=e&"+
			"tz=2&dst=israel&year=2026&c=on&maj=on")
		if msg.GetTzid() != "Asia/Jerusalem" {
			t.Errorf("tzid = %q, want Asia/Jerusalem", msg.GetTzid())
		}
	})

	t.Run("south and east keep their sign", func(t *testing.T) {
		msg := decodeV2(t, "v=1&ladeg=33&lamin=52&ladir=s&lodeg=151&lomin=12&lodir=e&"+
			"tzid=Australia/Sydney&year=2026&c=on")
		if got := msg.GetLatitude(); got > 0 {
			t.Errorf("latitude = %v, want a southern (negative) value", got)
		}
		if got := msg.GetLongitude(); got < 0 {
			t.Errorf("longitude = %v, want an eastern (positive) value", got)
		}
	})

	t.Run("an out-of-range degree is a 400", func(t *testing.T) {
		q, err := ParseV2Path(v2Path("v=1&ladeg=99&lamin=42&ladir=n&lodeg=74&lomin=0&"+
			"lodir=w&tzid=America/New_York", "hebcal.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeV2(q); !isBadRequest(err) {
			t.Errorf("ladeg=99 = %v, want a 400", err)
		}
	})

	// hasLatLongLegacy needs all six; anything less is not the legacy form and
	// leaves the calendar without a location, exactly as it does in hebcal-web.
	t.Run("an incomplete legacy form is not a location", func(t *testing.T) {
		msg := decodeV2(t, "v=1&ladeg=40&lamin=45&lodeg=73&lomin=59&year=2026&c=on")
		if msg.GetGeoPos() || msg.GetCityName() != "" {
			t.Errorf("an incomplete legacy form produced a location: %v", msg)
		}
	})
}

// geo= names the live location form, so a stale legacy or city parameter left
// beside a geonameid still loses to it.
func TestDecodeV2GeoWinsOverTheLegacyForms(t *testing.T) {
	msg := decodeV2(t, "v=1&geo=geoname&geonameid=4930956&city=GB-London&"+
		"ladeg=40&lamin=42&ladir=n&lodeg=74&lomin=0&lodir=w&tzid=America/New_York")
	if msg.GetGeonameid() != 4930956 {
		t.Errorf("geonameid = %d", msg.GetGeonameid())
	}
	if msg.GetCityName() != "" || msg.GetGeoPos() {
		t.Errorf("a stale location form survived geo=geoname: %v", msg)
	}
}

// A year, or "now", or a start/end range -- and either of the first two makes
// the range moot, which downloadHref2 handles by deleting it.
func TestDecodeV2YearBeatsRange(t *testing.T) {
	msg := decodeV2(t, "v=1&year=2026&start=2026-03-01&end=2026-05-31&maj=on")
	if msg.GetYear() != 2026 {
		t.Errorf("year = %d, want 2026", msg.GetYear())
	}
	if msg.GetStartStr() != "" || msg.GetStart() != 0 ||
		msg.GetEndStr() != "" || msg.GetEnd() != 0 {
		t.Errorf("an explicit year left the range in place: %v", msg)
	}

	msg = decodeV2(t, "v=1&start=2026-03-01&end=2026-05-31&maj=on")
	if msg.GetStartStr() != "2026-03-01" || msg.GetEndStr() != "2026-05-31" {
		t.Errorf("range = %q..%q", msg.GetStartStr(), msg.GetEndStr())
	}

	msg = decodeV2(t, "v=1&year=now&maj=on")
	if !msg.GetYearNow() || msg.GetYear() != 0 {
		t.Errorf("year=now: %v", msg)
	}
}

func TestDecodeV2RejectsBadDate(t *testing.T) {
	q, err := ParseV2Path(v2Path("v=1&start=March&end=2026-05-31", "hebcal.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeV2(q); !isBadRequest(err) {
		t.Errorf("DecodeV2 with a malformed start = %v, want a 400", err)
	}
}

// getInt()'s int32 guard, which keeps an oversized value from throwing deep
// inside the protobuf serializer.
func TestDecodeV2RejectsOutOfRangeInt(t *testing.T) {
	for _, qs := range []string{
		"v=1&geonameid=99999999999&year=2026",
		"v=1&year=99999999999",
		"v=1&year=2026&ny=99999999999",
	} {
		q, err := ParseV2Path(v2Path(qs, "hebcal.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeV2(q); !isBadRequest(err) {
			t.Errorf("DecodeV2(%q) = %v, want a 400", qs, err)
		}
	}
}

// month=x is the form's "entire year"; anything unparseable is simply not a
// month, because Number.parseInt gives NaN and getInt() reports it as unset.
func TestDecodeV2Month(t *testing.T) {
	tests := []struct {
		qs   string
		want int32
	}{
		{"v=1&month=6", 6},
		{"v=1&month=x", 0},
		{"v=1&month=", 0},
		{"v=1", 0},
	}
	for _, tt := range tests {
		if got := decodeV2(t, tt.qs).GetMonth(); got != tt.want {
			t.Errorf("%q: month = %d, want %d", tt.qs, got, tt.want)
		}
	}
}

func TestDecodeV2MonthMode(t *testing.T) {
	tests := []struct {
		qs   string
		want pb.Download_MonthMode
	}{
		{"v=1&mm=0", pb.Download_GREGORIAN_ARABIC},
		{"v=1&mm=1", pb.Download_HEBREW_ARABIC},
		{"v=1&mm=2", pb.Download_HEBREW_HEBREW},
		{"v=1&mm=9", pb.Download_GREGORIAN_ARABIC},
		{"v=1", pb.Download_GREGORIAN_ARABIC},
	}
	for _, tt := range tests {
		if got := decodeV2(t, tt.qs).GetMonthMode(); got != tt.want {
			t.Errorf("%q: monthMode = %v, want %v", tt.qs, got, tt.want)
		}
	}
}

// This looks like a bug and is not. downloadHref2 tests `euro` and `subscribe`
// with a bare `if (q.x)`, and every non-empty string is truthy in JavaScript,
// so euro=0 sets euro. h12 next to them uses off(), where "0" is false --
// which is why the three cannot share one helper.
func TestDecodeV2JavaScriptTruthiness(t *testing.T) {
	msg := decodeV2(t, "v=1&euro=0&subscribe=0&h12=0")
	if !msg.GetEuro() {
		t.Error(`euro=0 did not set euro; downloadHref2's bare if() treats "0" as true`)
	}
	if !msg.GetSubscribe() {
		t.Error("subscribe=0 did not set subscribe")
	}
	if msg.GetHour12() != pb.Download_OFF {
		t.Errorf("h12=0 gave %v, want OFF", msg.GetHour12())
	}
	if decodeV2(t, "v=1&h12=1").GetHour12() != pb.Download_ON {
		t.Error("h12=1 did not give ON")
	}
	// An absent h12 leaves the country default in place rather than choosing.
	if decodeV2(t, "v=1").GetHour12() != pb.Download_UNSET {
		t.Error("a missing h12 was resolved rather than left unset")
	}
}

// Number.parseInt stops at the first non-digit and Number.parseFloat does the
// same, so a legacy URL with trailing junk is read exactly as production reads
// it rather than rejected.
func TestParseIntAndFloatJS(t *testing.T) {
	intTests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"50", 50, true},
		{"50abc", 50, true},
		{" 50", 50, true},
		{"-5", -5, true},
		{"+5", 5, true},
		{"", 0, false},
		{"abc", 0, false},
		{"2026.pdf", 2026, true},
	}
	for _, tt := range intTests {
		got, ok := parseIntJS(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("parseIntJS(%q) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
	floatTests := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"7.083", 7.083, true},
		{"8.5deg", 8.5, true},
		{"-122.143", -122.143, true},
		{"", 0, false},
		{"east", 0, false},
	}
	for _, tt := range floatTests {
		got, ok := parseFloatJS(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("parseFloatJS(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// A repeated parameter keeps its last value, because redirV2 builds its query
// object with Object.fromEntries(), where later entries overwrite earlier
// ones. url.Values.Get would keep the first.
func TestParseV2PathLastValueWins(t *testing.T) {
	q, err := ParseV2Path(v2Path("v=1&year=2020&year=2026", "hebcal.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if q["year"] != "2026" {
		t.Errorf("year = %q, want the last value 2026", q["year"])
	}
}
