package pdf

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// MessageToQuery renders a decoded Download protobuf back into the query string
// hebcal-web's deserializeDownload.js would produce from the same message
// (e.g. "v=1&M=on&yt=H&year=5787&lg=s&dksa=on&mm=0"). It is a diagnostic: a
// /v4/<base64>/<name>.pdf URL is opaque in the access log, and this makes the
// request readable and reproducible under the log's "qs" key.
//
// It is a faithful port of deserializeDownload.js, field-for-field and in the
// same order, so the output round-trips through DecodeV2 and can be pasted onto
// a hebcal.com URL. Keep it in step with that file and with the daily-learning
// list in learningQueryParams below.
func MessageToQuery(msg *downloadpb.Download) string {
	var q queryBuilder
	q.set("v", "1")
	q.onIf("maj", msg.GetMajor())
	q.onIf("min", msg.GetMinor())
	q.onIf("nx", msg.GetRoshChodesh())
	q.onIf("mod", msg.GetModern())
	q.onIf("mf", msg.GetMinorFast())
	q.onIf("ss", msg.GetSpecialShabbat())
	q.onIf("i", msg.GetIsrael())
	if msg.GetHavdalahTzeit() {
		q.set("M", "on")
	} else {
		q.set("M", "off")
		// deserializeDownload sets q.m = havdalahMins whenever M=off, even when 0.
		q.set("m", strconv.Itoa(int(msg.GetHavdalahMins())))
	}
	if tz := msg.GetTzeit(); tz != 0 {
		q.set("td", formatFloat(float64(tz)))
	}
	if msg.GetIsHebrewYear() {
		q.set("yt", "H")
	} else {
		q.set("yt", "G")
	}
	q.onIf("c", msg.GetCandlelighting())
	if id := msg.GetGeonameid(); id != 0 {
		q.set("geonameid", strconv.Itoa(int(id)))
	}
	if msg.GetYearNow() {
		q.set("year", "now")
	} else if y := msg.GetYear(); y != 0 {
		q.set("year", strconv.Itoa(int(y)))
	}
	if lg := msg.GetLocale(); lg != "" {
		q.set("lg", lg)
	} else {
		q.set("lg", "s")
	}
	if b := msg.GetCandleLightingMins(); b != 0 {
		q.set("b", strconv.Itoa(int(b)))
	}
	q.setIf("emoji", "1", msg.GetEmoji())
	q.setIf("euro", "1", msg.GetEuro())
	switch msg.GetHour12() {
	case 1:
		q.set("h12", "1")
	case 2:
		q.set("h12", "0")
	}
	q.setIf("subscribe", "1", msg.GetSubscribe())
	if n := msg.GetNumYears(); n != 0 {
		q.set("ny", strconv.Itoa(int(n)))
	}
	if zip := msg.GetZip(); zip != "" {
		q.set("zip", zip)
	}
	q.onIf("o", msg.GetOmer())
	q.onIf("d", msg.GetAddAltDates())
	q.onIf("D", msg.GetAddAltDatesForEvents())
	q.onIf("ykk", msg.GetYomKippurKatan())
	for _, lp := range learningQueryParams {
		q.onIf(lp.param, lp.on(msg))
	}
	q.onIf("ue", msg.GetUseElevation())
	q.onIf("yzkr", msg.GetYizkor())
	q.onIf("mvch", msg.GetShabbatMevarchim())
	q.set("mm", strconv.Itoa(int(msg.GetMonthMode())))
	q.onIf("yto", msg.GetYomTovOnly())
	if m := msg.GetMonth(); m != 0 {
		q.set("month", strconv.Itoa(int(m)))
	}
	if msg.GetGeoPos() {
		lat := float64(msg.GetLatitude())
		if _, ok := msg.GetLatOneof().(*downloadpb.Download_OldLatitude); ok {
			lat = msg.GetOldLatitude()
		}
		long := float64(msg.GetLongitude())
		if _, ok := msg.GetLongOneof().(*downloadpb.Download_OldLongitude); ok {
			long = msg.GetOldLongitude()
		}
		q.set("latitude", formatFloat(lat))
		q.set("longitude", formatFloat(long))
		if elev := msg.GetElev(); elev > 0 {
			q.set("elev", strconv.Itoa(int(elev)))
		}
		if city := msg.GetCityName(); city != "" {
			q.set("city-typeahead", city)
		}
		q.set("geo", "pos")
	}
	if tzid := msg.GetTzid(); tzid != "" {
		q.set("tzid", tzid)
	}
	if _, ok := msg.GetStartOneof().(*downloadpb.Download_StartStr); ok {
		q.set("start", msg.GetStartStr())
	} else if secs := msg.GetStart(); secs != 0 {
		q.set("start", time.Unix(secs, 0).UTC().Format("2006-01-02"))
	}
	if _, ok := msg.GetEndOneof().(*downloadpb.Download_EndStr); ok {
		q.set("end", msg.GetEndStr())
	} else if secs := msg.GetEnd(); secs != 0 {
		q.set("end", time.Unix(secs, 0).UTC().Format("2006-01-02"))
	}
	return q.String()
}

// learningQueryParams maps each daily-learning protobuf field to its query
// parameter, in the order deserializeDownload.js walks dailyLearningConfig.json
// (so the "qs" field lists them the way hebcal-web would). The last entry is
// sedrot, whose config row carries the plain `s` Torah-readings parameter.
var learningQueryParams = []struct {
	param string
	on    func(*downloadpb.Download) bool
}{
	{"F", func(m *downloadpb.Download) bool { return m.GetDafyomi() }},
	{"myomi", func(m *downloadpb.Download) bool { return m.GetMishnaYomi() }},
	{"dpy", func(m *downloadpb.Download) bool { return m.GetPerekYomi() }},
	{"nyomi", func(m *downloadpb.Download) bool { return m.GetNachYomi() }},
	{"dty", func(m *downloadpb.Download) bool { return m.GetTanakhYomi() }},
	{"dps", func(m *downloadpb.Download) bool { return m.GetPsalms() }},
	{"d929", func(m *downloadpb.Download) bool { return m.GetNine29() }},
	{"dr1", func(m *downloadpb.Download) bool { return m.GetRambam1() }},
	{"dr3", func(m *downloadpb.Download) bool { return m.GetRambam3() }},
	{"dsm", func(m *downloadpb.Download) bool { return m.GetSeferHaMitzvot() }},
	{"yyomi", func(m *downloadpb.Download) bool { return m.GetYerushalmiYomi() }},
	{"yys", func(m *downloadpb.Download) bool { return m.GetYySchottenstein() }},
	{"dcc", func(m *downloadpb.Download) bool { return m.GetChofetzChaim() }},
	{"dshl", func(m *downloadpb.Download) bool { return m.GetShemiratHaLashon() }},
	{"ayd", func(m *downloadpb.Download) bool { return m.GetDirshuAmudYomi() }},
	{"dw", func(m *downloadpb.Download) bool { return m.GetDafWeekly() }},
	{"dpa", func(m *downloadpb.Download) bool { return m.GetPirkeiAvotSummer() }},
	{"ahsy", func(m *downloadpb.Download) bool { return m.GetArukhHaShulchanYomi() }},
	{"dksa", func(m *downloadpb.Download) bool { return m.GetKitzurShulchanAruch() }},
	{"s", func(m *downloadpb.Download) bool { return m.GetSedrot() }},
}

// queryBuilder accumulates key=value pairs in insertion order, so the rendered
// query matches deserializeDownload.js's field order rather than being sorted.
type queryBuilder struct {
	b     strings.Builder
	wrote bool
}

func (q *queryBuilder) set(key, val string) {
	if q.wrote {
		q.b.WriteByte('&')
	}
	q.wrote = true
	q.b.WriteString(key)
	q.b.WriteByte('=')
	// Encode like JavaScript's encodeURIComponent (spaces as %20, not +), which
	// deserializeDownload's consumers use, so the query pastes cleanly into a URL.
	q.b.WriteString(strings.ReplaceAll(url.QueryEscape(val), "+", "%20"))
}

// onIf sets key=on when cond holds, the shape most boolean options take.
func (q *queryBuilder) onIf(key string, cond bool) {
	if cond {
		q.set(key, "on")
	}
}

// setIf sets key=val when cond holds.
func (q *queryBuilder) setIf(key, val string, cond bool) {
	if cond {
		q.set(key, val)
	}
}

func (q *queryBuilder) String() string { return q.b.String() }

// formatFloat renders a float the way JavaScript's String(number) does: the
// shortest form that round-trips, no trailing zeros.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
