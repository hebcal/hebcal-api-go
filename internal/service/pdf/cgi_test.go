package pdf

import (
	"testing"

	"google.golang.org/protobuf/proto"

	pb "github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// decodeCGI runs the whole classic-CGI path in one call: a filename and the raw
// query string, as they arrive on /hebcal/index.cgi/<filename>?<rawQuery>.
func decodeCGI(t *testing.T, filename, rawQuery string) *pb.Download {
	t.Helper()
	q, err := ParseCGIPath(cgiPrefix+filename, rawQuery)
	if err != nil {
		t.Fatalf("ParseCGIPath(%q, %q): %v", filename, rawQuery, err)
	}
	msg, err := DecodeV2(q)
	if err != nil {
		t.Fatalf("DecodeV2(%q): %v", rawQuery, err)
	}
	return msg
}

// The same request arrives in three encodings, and all three must decode to one
// protobuf: a plain &-separated query, an old semicolon-separated one, and the
// doubly-encoded form fixup2 spots by its dl=1%3B prefix (its ';' as %3B and
// most of its '=' as %3D). The plain form is the reference.
func TestCGIEncodingsAgree(t *testing.T) {
	const (
		plain = "dl=1&v=1&geo=city&city=SN-Dakar&vis=on&month=10&year=2013&" +
			"nh=on&nx=on&s=on&c=on&mf=on&ss=on"
		semicolon = "dl=1;v=1;geo=city;city=SN-Dakar;vis=on;month=10;year=2013;" +
			"nh=on;nx=on;s=on;c=on;mf=on;ss=on"
		// The real request from the access log, %3B for ';' and %3D for '='.
		doubled = "dl=1%3Bv%3D1%3Bgeo%3Dcity%3Bcity%3DSN-Dakar%3Bvis%3Don%3B" +
			"month%3D10%3Byear%3D2013%3Bnh%3Don%3Bnx%3Don%3Bs%3Don%3Bc%3Don%3B" +
			"mf%3Don%3Bss%3Don"
	)
	want := decodeCGI(t, "hebcal_2013_oct_sn_dakar.pdf", plain)
	for _, tt := range []struct {
		name, qs string
	}{
		{"semicolon", semicolon},
		{"doubly-encoded", doubled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeCGI(t, "hebcal_2013_oct_sn_dakar.pdf", tt.qs)
			if !proto.Equal(got, want) {
				t.Errorf("%s decoded differently:\n got %v\nwant %v", tt.name, got, want)
			}
		})
	}
	// The reference itself must be the calendar the URL describes.
	if want.GetCityName() != "SN-Dakar" {
		t.Errorf("cityName = %q, want SN-Dakar", want.GetCityName())
	}
	if !want.GetMajor() || !want.GetMinor() || !want.GetModern() {
		t.Errorf("nh=on did not turn on the holiday categories: %v", want)
	}
}

// The mixed encoding a second real request carries: %3B for the separators but a
// literal '=' throughout and a %20 space in the city name.
func TestCGIMixedEncodingSpace(t *testing.T) {
	const mixed = "dl=1%3Bv=1%3Bgeo=city%3Bcity=US-White%20Plains-NY%3Bm=43%3B" +
		"vis=on%3Bmonth=10%3Byear=2013%3Bnh=on%3Bnx=on%3Bs=on%3Bc=on%3Bmf=on%3Bss=on"
	msg := decodeCGI(t, "hebcal_2013_oct_us_white_plains_ny.pdf", mixed)
	if msg.GetCityName() != "US-White Plains-NY" {
		t.Errorf("cityName = %q, want %q", msg.GetCityName(), "US-White Plains-NY")
	}
	if msg.GetHavdalahMins() != 43 {
		t.Errorf("m=43 havdalahMins = %d", msg.GetHavdalahMins())
	}
}

// nh=on is the very old "all holiday categories" switch: it expands to the five
// negative options other than Rosh Chodesh, and does so unconditionally, so it
// overrides an explicit maj/min/mod/mf/ss beside it.
func TestCGINhExpands(t *testing.T) {
	msg := decodeCGI(t, "x.pdf", "v=1&year=2026&nh=on&maj=off")
	for _, c := range []struct {
		name string
		on   bool
	}{
		{"major", msg.GetMajor()},
		{"minor", msg.GetMinor()},
		{"modern", msg.GetModern()},
		{"minorFast", msg.GetMinorFast()},
		{"specialShabbat", msg.GetSpecialShabbat()},
	} {
		if !c.on {
			t.Errorf("nh=on left %s off: %v", c.name, msg)
		}
	}
	// nh does not touch Rosh Chodesh.
	if msg.GetRoshChodesh() {
		t.Errorf("nh=on set Rosh Chodesh: %v", msg)
	}
}

// Lowercase m=on is the old spelling of M=on (Havdalah at tzeit), told apart
// from the numeric havdalah offset by its literal value.
func TestCGILowercaseMOn(t *testing.T) {
	msg := decodeCGI(t, "x.pdf", "v=1&year=2026&c=on&geo=geoname&geonameid=5128581&m=on")
	if !msg.GetHavdalahTzeit() {
		t.Errorf("m=on did not set Havdalah at tzeit: %v", msg)
	}
	if msg.GetHavdalahMins() != 0 {
		t.Errorf("m=on left a havdalah offset: %d", msg.GetHavdalahMins())
	}
}

// The named geo forms the sample URLs use both resolve: a legacy city
// identifier and a geonameid.
func TestCGIGeoForms(t *testing.T) {
	t.Run("legacy city", func(t *testing.T) {
		msg := decodeCGI(t, "hebcal_2013_oct_Omaha__NE.pdf",
			"redir=1&dl=1&v=1&geo=city&city=US-Omaha-NE&m=43&vis=on&month=10&"+
				"year=2013&nh=on&nx=on&s=on&c=on&mf=on&ss=on")
		if msg.GetCityName() != "US-Omaha-NE" || msg.GetGeoPos() {
			t.Errorf("legacy city not carried in cityName: %v", msg)
		}
	})
	t.Run("geonameid", func(t *testing.T) {
		msg := decodeCGI(t, "hebcal_2013_nov_Vilnius.pdf",
			"dl=1&v=1&geo=geoname&m=50&vis=on&month=11&year=2013&nx=on&s=on&c=on&"+
				"mf=on&ss=on&maj=on&min=on&mod=on&geonameid=593116")
		if msg.GetGeonameid() != 593116 {
			t.Errorf("geonameid = %d, want 593116", msg.GetGeonameid())
		}
	})
}

// Only .pdf is ours, and a request naming no download version is not a download
// at all -- both are errors the handler maps to 404.
func TestCGIPathErrors(t *testing.T) {
	t.Run("not a pdf", func(t *testing.T) {
		if _, err := ParseCGIPath(cgiPrefix+"hebcal.ics", "v=1&year=2026"); err == nil {
			t.Error("a .ics request was accepted")
		}
	})
	t.Run("no query is not a download", func(t *testing.T) {
		// hebcal_2028_may.pdf with no query: v is absent, which DecodeV2 reports
		// as the 404 the router answers (ctx.throw(404) for v===undefined).
		q, err := ParseCGIPath(cgiPrefix+"hebcal_2028_may.pdf", "")
		if err != nil {
			t.Fatalf("ParseCGIPath: %v", err)
		}
		if _, err := DecodeV2(q); !isNotFound(err) {
			t.Errorf("no v = %v, want a 404", err)
		}
	})
}
