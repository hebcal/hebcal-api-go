package csv

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := geodb.New("../../handler/testdata/zips.sqlite3", "../../handler/testdata/geonames.sqlite3")
	if err != nil {
		t.Fatalf("geodb: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &Service{Geo: db}
}

func render(t *testing.T, s *Service, path string) string {
	t.Helper()
	u, err := url.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	cal, err := s.Prepare(context.Background(), u)
	if err != nil {
		t.Fatalf("Prepare(%s): %v", path, err)
	}
	return string(Render(cal))
}

// The expected rows below are hebcal-web's output for the same URLs.

func TestRenderDocument(t *testing.T) {
	s := testService(t)
	// every holiday option, Hebrew year 5787, no location
	doc := render(t, s, "/v4/CAEQARgBIAEoATABQAFIAWCbLWoBcw/hebcal_5787h_usa.csv")
	if !strings.HasPrefix(doc, bom+Header+"\r\n") {
		t.Fatalf("document does not begin with the BOM and header: %q", doc[:min(len(doc), 140)])
	}
	if !strings.HasSuffix(doc, "\r\n") || strings.Contains(strings.ReplaceAll(doc, "\r\n", ""), "\n") {
		t.Error("every line must end in CRLF, and only there")
	}
	// No count of years was given, so the calendar spans seven.
	if n := strings.Count(doc, "\r\n"); n != 617 {
		t.Errorf("got %d lines, want 617", n)
	}
	for _, want := range []string{
		`"Erev Rosh Hashana","9/11/2026",,,,"true","The Jewish New Year","3","Jewish Holidays"`,
		`"Rosh Hashana 5787","9/12/2026",,,,"true","The Jewish New Year","4","Jewish Holidays"`,
		`"Sukkot III (CH’’M)","9/28/2026",,,,"true","Feast of Booths","3","Jewish Holidays"`,
		// commas in a description become semicolons
		`"Swords of Iron War Memorial Day","10/5/2026",,,,"true","Israeli national memorial day commemorating the fallen victims of the October 7 attacks; and the soldiers of the Israel Defense Forces","3","Jewish Holidays"`,
		`"Rosh Chodesh Cheshvan","10/11/2026",,,,"true","Start of month of Cheshvan on the Hebrew calendar","3","Jewish Holidays"`,
	} {
		if !strings.Contains(doc, "\r\n"+want+"\r\n") {
			t.Errorf("missing row\n%s", want)
		}
	}
}

func TestRenderMemos(t *testing.T) {
	s := testService(t)
	tests := []struct {
		name string
		path string
		rows []string
	}{
		{
			name: "New York 5789",
			path: "/v4/CAEQARgBIAEoATABQAFIAVABWIWDuQJgnS1qAXN4EogBAQ/x.csv",
			rows: []string{
				// a fast's start and end name the fast
				`"Fast begins","9/24/2028","5:25 AM","9/24/2028","5:25 AM","false","Tzom Gedaliah","4","New York"`,
				`"Fast ends","9/24/2028","7:22 PM","9/24/2028","7:22 PM","false","Tzom Gedaliah","4","New York"`,
				// erev-Yom-Tov candle-lighting names the holiday
				`"Candle lighting","9/29/2028","6:22 PM","9/29/2028","6:22 PM","false","Erev Yom Kippur","4","New York"`,
				// Saturday-night Havdalah has nothing to name
				`"Havdalah","9/23/2028","7:31 PM","9/23/2028","7:31 PM","false","","4","New York"`,
				// Shabbat Mevarchim announces the molad
				`"Mevarchim Chodesh Cheshvan","10/14/2028",,,,"true","Molad Cheshvan: Wednesday; 4:04pm and 9 chalakim","3",""`,
				// a portion read on the eve of a new month
				`"Parashat Toldot","11/18/2028",,,,"true","Machar Chodesh","3","Torah Reading"`,
				// a portion read with a special Shabbat and Rosh Chodesh
				`"Parashat Vayikra","3/17/2029",,,,"true","Shabbat HaChodesh + Rosh Chodesh Nisan","3","Torah Reading"`,
				`"Parashat Mishpatim","2/10/2029",,,,"true","Shabbat Shekalim","3","Torah Reading"`,
			},
		},
		{
			name: "Jerusalem 5777",
			path: "/v4/CAEQARgBIAEoATABOAFAAUgBUAFY4JQRYJEteBSIAQE/x.csv",
			rows: []string{
				// a festival displaces the coming Shabbat's portion
				`"Candle lighting","10/21/2016","17:40","10/21/2016","17:40","false","Sukkot Shabbat Chol ha-Moed","4","Jerusalem"`,
			},
		},
		{
			name: "London 2024, day/month",
			path: "/v4/CAEYAVifrqEBYOgPcDKIAQHYAQE/x.csv",
			rows: []string{
				`"Parashat Terumah","1/3/2025",,,,"true","Shabbat Shekalim + Rosh Chodesh Adar","3","Torah Reading"`,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := render(t, s, tc.path)
			for _, want := range tc.rows {
				if !strings.Contains(doc, "\r\n"+want+"\r\n") {
					t.Errorf("missing row\n%s", want)
				}
			}
		})
	}
}

func TestRenderEmpty(t *testing.T) {
	// A calendar with nothing selected is a header alone, not an error.
	s := testService(t)
	msg := &downloadpb.Download{Year: 2026}
	params, err := pdf.ParamsFromMessage(msg, s.Geo)
	if err != nil {
		t.Fatal(err)
	}
	params.Opts.Mask = 0
	params.Opts.NoHolidays = true
	got := Render(&Calendar{Params: params})
	if want := bom + Header + "\r\n"; !bytes.Equal(got, []byte(want)) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPrepareErrors(t *testing.T) {
	s := testService(t)
	for _, path := range []string{
		"/v4/CAEQARgBIAEoATABQAFIAWCbLWoBcw/hebcal_5787h_usa.pdf",
		"/v4/!!!/x.csv",
		"/v4/x.csv",
	} {
		u, _ := url.Parse(path)
		var nf *pdf.NotFoundError
		if _, err := s.Prepare(context.Background(), u); !errors.As(err, &nf) {
			t.Errorf("%s: got %v, want a NotFoundError", path, err)
		}
	}
}

func TestNumYears(t *testing.T) {
	// Candle-lighting needs a location to stay on.
	const newYork = 5128581
	s := testService(t)
	tests := []struct {
		name  string
		msg   *downloadpb.Download
		unsup int
		want  int
	}{
		{"default", &downloadpb.Download{}, 0, 7},
		{"explicit", &downloadpb.Download{NumYears: 3}, 0, 3},
		{"explicit capped", &downloadpb.Download{NumYears: 40}, 0, 10},
		{"candle-lighting", &downloadpb.Download{Candlelighting: true, Geonameid: newYork}, 0, 4},
		{"sedrot", &downloadpb.Download{Sedrot: true}, 0, 5},
		{"omer and sedrot", &downloadpb.Download{Omer: true, Sedrot: true}, 0, 4},
		{"candles and daily alt dates", &downloadpb.Download{Candlelighting: true, Geonameid: newYork, AddAltDates: true}, 0, 2},
		{"candles and event alt dates", &downloadpb.Download{Candlelighting: true, Geonameid: newYork, AddAltDatesForEvents: true}, 0, 3},
		{"one daily series", &downloadpb.Download{Dafyomi: true}, 0, 2},
		{"two daily series", &downloadpb.Download{Dafyomi: true, MishnaYomi: true}, 0, 1},
		{"daily series and daily alt dates", &downloadpb.Download{Dafyomi: true, AddAltDates: true}, 0, 1},
		{"one series from readings-svc", &downloadpb.Download{}, 1, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := pdf.ParamsFromMessage(tc.msg, s.Geo)
			if err != nil {
				t.Fatal(err)
			}
			if got := numYears(tc.msg, p, tc.unsup); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestChagReadingName(t *testing.T) {
	tests := []struct {
		hd   hdate.HDate
		il   bool
		want string
	}{
		{hdate.New(5787, hdate.Tishrei, 1), false, "Rosh Hashana"},
		{hdate.New(5785, hdate.Tishrei, 10), false, "Yom Kippur"},
		{hdate.New(5785, hdate.Tishrei, 22), false, "Shmini Atzeret"},
		{hdate.New(5777, hdate.Tishrei, 19), true, "Sukkot Shabbat Chol ha-Moed"},
		{hdate.New(5781, hdate.Nisan, 15), false, "Pesach I"},
		{hdate.New(5781, hdate.Nisan, 15), true, "Pesach"},
		{hdate.New(5784, hdate.Nisan, 22), false, "Pesach VIII"},
	}
	for _, tc := range tests {
		if got := chagReadingName(tc.hd, tc.il); got != tc.want {
			t.Errorf("chagReadingName(%v, il=%v) = %q, want %q", tc.hd, tc.il, got, tc.want)
		}
	}
}
