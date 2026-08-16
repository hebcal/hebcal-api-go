package pdf

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/service/location"
	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// The legacy /v2/h/<base64-querystring>/<filename>.pdf download URLs, still
// linked from a decade of pages and still crawled.
//
// hebcal-web answers these with a 301 to the /v4/ form: src/app-download.js's
// redirV2 middleware decodes the path with parseV2Path(), parses the query
// string, and re-encodes it as a protobuf with downloadHref2()
// (src/makeDownloadProps.js). This service serves them directly with a 200
// instead, which is the same calendar without the round trip -- so the code
// below is that same query-string-to-protobuf conversion, after which a /v2/
// request is indistinguishable from a /v4/ one and takes the identical path
// through ParamsFromMessage, Generate and the renderer.

// v2Prefix is the only /v2/ family this service serves. hebcal-web's
// parseV2Path() splits at the first slash after this prefix for every /v2/
// path; the others are yahrzeit calendars (/v3/, and /v2/ URLs whose v= is a
// "y..." value), which this service does not render.
const v2Prefix = "/v2/h/"

// v2Query is a legacy download URL's decoded query string.
//
// It is a plain map rather than url.Values because that is the shape
// downloadHref2() sees: redirV2 builds it with
// Object.fromEntries(new URLSearchParams(qs).entries()), where a repeated
// parameter keeps its *last* value, while url.Values.Get keeps the first.
type v2Query map[string]string

// on is makeDownloadProps.js's on(): the two spellings a checkbox arrives in.
func (q v2Query) on(key string) bool {
	return jsutil.IsOn(q[key])
}

// off is urlArgs.js's off(). Note that a missing parameter is "off", which is
// not the negation of on() -- "yes" is neither.
func (q v2Query) off(key string) bool {
	v, ok := q[key]
	return !ok || v == "off" || v == "0"
}

// empty is empty.js's empty(): absent or the empty string.
func (q v2Query) empty(key string) bool { return q[key] == "" }

// values re-inflates the query for internal/service/location, which reads the
// url.Values a live request carries rather than this map.
func (q v2Query) values() url.Values {
	out := make(url.Values, len(q))
	for key, val := range q {
		out.Set(key, val)
	}
	return out
}

// truthy is a bare `if (q.x)` in makeDownloadProps.js, which is not on(): the
// string "0" is truthy in JavaScript, so `euro=0` sets euro. Faithful to the
// 301 hebcal-web issues today.
func (q v2Query) truthy(key string) bool { return q[key] != "" }

// getInt is makeDownloadProps.js's getInt(): Number.parseInt(s, 10) with the
// int32 range check that keeps an oversized value from throwing deep inside
// the protobuf serializer. ok is false where JavaScript would produce NaN.
func (q v2Query) getInt(key string) (n int32, ok bool, err error) {
	i, ok := jsutil.ParseInt(q[key])
	if !ok {
		return 0, false, nil
	}
	if i < -2147483648 || i > 2147483647 {
		return 0, false, model.BadRequest("Numeric value out of range: %s", q[key])
	}
	return int32(i), true, nil
}

// getFloat is Number.parseFloat(): ok is false where JavaScript gives NaN.
func (q v2Query) getFloat(key string) (float64, bool) {
	f, err := jsutil.ParseFloat(q[key])
	return f, err == nil
}

// primaryGeoKeys and allGeoKeys are urlArgs.js's lists, including the
// long-retired degrees/minutes location form.
var (
	primaryGeoKeys = []string{"geonameid", "zip", "city"}
	allGeoKeys     = []string{
		"geonameid", "zip", "city", "latitude", "longitude", "elev", "tzid",
		"ladeg", "lamin", "lodeg", "lomin", "city-typeahead",
	}
)

// geoKeysToRemove is urlArgs.js's getGeoKeysToRemove(): `geo` names which one
// of several location forms in the query is the live one, and the rest are
// dropped before anything reads them.
func geoKeysToRemove(geo string) []string {
	switch geo {
	case "":
		return nil
	case "pos":
		return primaryGeoKeys
	case "none":
		return append(append([]string(nil), allGeoKeys...), "b", "m", "td", "M", "ue")
	case "geoname":
		return allGeoKeysExcept("geonameid")
	default:
		return allGeoKeysExcept(geo)
	}
}

func allGeoKeysExcept(keep string) []string {
	out := make([]string, 0, len(allGeoKeys))
	for _, k := range allGeoKeys {
		if k != keep {
			out = append(out, k)
		}
	}
	return out
}

// normalize is urlArgs.js's urlArgsObj(), the pass downloadHref2() runs its
// argument through before reading a single field.
//
// Its fourth loop, which rewrites a falsy maj/min/nx/mod/mf/ss to the string
// "off", is omitted: on() already reads "0" and "off" alike as false, so it
// changes nothing the protobuf sees.
func (q v2Query) normalize() {
	for _, key := range geoKeysToRemove(q["geo"]) {
		delete(q, key)
	}
	// Havdalah: an explicit tzeit, or an M=off carrying no offset, both mean
	// "use tzeit" -- and the offset is dropped so the m branch below cannot
	// contradict it.
	if q["M"] == "on" || !q.empty("td") || (q["M"] == "off" && q.empty("m")) {
		q["M"] = "on"
		delete(q, "m")
	}
	delete(q, ".s")
	delete(q, "vis")
}

// ParseV2Path decodes a legacy /v2/h/<base64>/<filename>.pdf path into the
// query string it carries. Port of parseV2Path() in src/app-download.js.
func ParseV2Path(path string) (v2Query, error) {
	if !strings.HasPrefix(path, v2Prefix) {
		return nil, fmt.Errorf("expected /v2/h/<data>/<filename>.pdf, got %q", path)
	}
	data, filename, ok := strings.Cut(path[len(v2Prefix):], "/")
	// parseV2Path() defaults a path with no filename to hebcal.ics, which is
	// not a PDF request at all.
	if !ok || !strings.HasSuffix(filename, ".pdf") {
		return nil, errors.New("not a .pdf request")
	}
	if data == "" {
		return nil, errors.New("empty payload")
	}
	raw, err := decodeBase64(data)
	if err != nil {
		return nil, fmt.Errorf("base64: %w", err)
	}
	// The error is deliberately ignored: URLSearchParams never rejects a query
	// string, so a stray percent sign has to leave the other parameters intact
	// rather than fail the request. url.ParseQuery returns what it could parse
	// alongside the error.
	values, _ := url.ParseQuery(string(raw))
	q := make(v2Query, len(values))
	for key, vals := range values {
		q[key] = vals[len(vals)-1]
	}
	return q, nil
}

// DecodeV2 turns a legacy download query string into the Download message the
// equivalent /v4/ URL would carry. Port of downloadHref2() in
// src/makeDownloadProps.js, minus the base64 encoding it exists to produce.
//
// Two of downloadHref2()'s location forms are deliberately not handled here,
// because it does not handle them either: a legacy `city=` name and the
// degrees/minutes `ladeg`/`lamin` pair both leave the message with no
// location, exactly as they do in the 301 production issues today.
func DecodeV2(q v2Query) (*downloadpb.Download, error) {
	// redirV2 only rewrites a URL whose v=1; anything else falls through to
	// the yahrzeit branch or to hebcal-download.js's own rejection.
	switch q["v"] {
	case "1":
	case "":
		return nil, NotFoundf("Invalid download URL: v=undefined")
	default:
		return nil, model.BadRequest("Invalid download URL: v=%s", q["v"])
	}
	q.normalize()

	msg := &downloadpb.Download{}
	msg.Major = q.on("maj")
	msg.Minor = q.on("min")
	msg.RoshChodesh = q.on("nx")
	msg.Modern = q.on("mod")
	msg.MinorFast = q.on("mf")
	msg.SpecialShabbat = q.on("ss")
	msg.Israel = q.on("i")
	msg.IsHebrewYear = q["yt"] == "H"
	msg.Candlelighting = q.on("c")

	if geonameid, ok, err := q.getInt("geonameid"); err != nil {
		return nil, err
	} else if ok {
		msg.Geonameid = geonameid
	}

	// A year, "now", or neither. Either of the first two makes an explicit
	// start/end range moot, and downloadHref2 drops it.
	year, hasYear, err := q.getInt("year")
	if err != nil {
		return nil, err
	}
	switch {
	case hasYear:
		msg.Year = year
		delete(q, "start")
		delete(q, "end")
	case q["year"] == "now":
		msg.YearNow = true
		delete(q, "start")
		delete(q, "end")
	}

	if !q.empty("lg") {
		msg.Locale = q["lg"]
	}
	havdalahMins, hasHavdalahMins, err := q.getInt("m")
	if err != nil {
		return nil, err
	}
	if hasHavdalahMins {
		msg.HavdalahMins = havdalahMins
	}
	// normalize() has already collapsed the tzeit spellings onto M=on, so the
	// remaining case is a URL that named no Havdalah rule at all, which is
	// tzeit by default.
	if q["M"] == "on" || !hasHavdalahMins {
		msg.HavdalahTzeit = true
	}
	if !q.empty("td") {
		if tzeit, ok := q.getFloat("td"); ok && tzeit != 0 {
			msg.HavdalahTzeit = true
			// 8.5 degrees is hebcal-go's default, so it travels as an unset
			// field rather than as itself.
			if tzeit != 8.5 {
				msg.Tzeit = float32(tzeit)
			}
		}
	}
	if b, ok, err := q.getInt("b"); err != nil {
		return nil, err
	} else if ok {
		msg.CandleLightingMins = b
	}

	msg.Emoji = q.on("emoji")
	msg.Euro = q.truthy("euro")
	if !q.empty("h12") {
		if q.off("h12") {
			msg.Hour12 = downloadpb.Download_OFF
		} else {
			msg.Hour12 = downloadpb.Download_ON
		}
	}
	msg.Subscribe = q.truthy("subscribe")
	if ny, ok, err := q.getInt("ny"); err != nil {
		return nil, err
	} else if ok {
		msg.NumYears = ny
	}
	if !q.empty("zip") {
		msg.Zip = q["zip"]
	}

	msg.Omer = q.on("o")
	msg.YomKippurKatan = q.on("ykk")
	applyV2DailyLearning(q, msg)
	msg.Yizkor = q.on("yzkr")
	msg.ShabbatMevarchim = q.on("mvch")
	msg.UseElevation = q.on("ue")

	msg.AddAltDates = q.on("d")
	msg.AddAltDatesForEvents = q.on("D")
	switch q["mm"] {
	case "2":
		msg.MonthMode = downloadpb.Download_HEBREW_HEBREW
	case "1":
		msg.MonthMode = downloadpb.Download_HEBREW_ARABIC
	default:
		msg.MonthMode = downloadpb.Download_GREGORIAN_ARABIC
	}
	msg.YomTovOnly = q.on("yto")

	// month=x is the form's "entire year" option.
	if !q.empty("month") && q["month"] != "x" {
		if month, ok, err := q.getInt("month"); err != nil {
			return nil, err
		} else if ok {
			msg.Month = month
		}
	}

	// downloadHref2 converts these to epoch seconds; the message can carry the
	// ISO string instead, which applyDateRange prefers and which cannot pick
	// up a timezone on the way through.
	if !q.empty("start") {
		if err := checkISODate(q["start"]); err != nil {
			return nil, err
		}
		msg.StartOneof = &downloadpb.Download_StartStr{StartStr: q["start"]}
	}
	if !q.empty("end") {
		if err := checkISODate(q["end"]); err != nil {
			return nil, err
		}
		msg.EndOneof = &downloadpb.Download_EndStr{EndStr: q["end"]}
	}

	if err := applyV2Location(q, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// applyV2Location writes the request's location into the message.
//
// The first branch is downloadHref2's, verbatim. The other two are the
// locations it has no branch for, so its 301 hands /v4/ a calendar with no
// location at all -- and, since a location implies candle-lighting, no times
// and a "Hebcal Diaspora" title. That is a regression the redirect introduced:
// before redirV2 existed these URLs were rewritten to /export/ and rendered by
// hebcalDownload, whose makeHebcalOptions calls getLocationFromQuery, and that
// function resolves both. So this is a deliberate divergence from the 301 and a
// return to what the URLs used to draw.
//
// Neither costs anything to support here: the legacy city name is resolved by
// the same LookupLegacyCity branch of applyLocation a /geo request uses, and
// the degrees/minutes form by internal/service/location, which already ports
// that whole branch of getLocationFromQuery -- range checks, the legacy
// numeric-timezone mapping and the "40°42′N 74°0′W" city name included.
func applyV2Location(q v2Query, msg *downloadpb.Download) error {
	switch {
	case q["geo"] == "pos":
		msg.GeoPos = true
		if lat, ok := q.getFloat("latitude"); ok {
			msg.LatOneof = &downloadpb.Download_Latitude{Latitude: float32(lat)}
		}
		if long, ok := q.getFloat("longitude"); ok {
			msg.LongOneof = &downloadpb.Download_Longitude{Longitude: float32(long)}
		}
		if !q.empty("tzid") {
			msg.Tzid = q["tzid"]
		}
		if !q.empty("city-typeahead") {
			msg.CityName = q["city-typeahead"]
		}
		if elev, ok := q.getFloat("elev"); ok && elev > 0 {
			msg.Elev = int32(elev)
		}

	case !q.empty("city"):
		// A legacy Hebcal city identifier ("GB-London"). The protobuf has no
		// field of its own for it, but cityName is free here -- downloadHref2
		// only ever sets that alongside geoPos -- and applyLocation already
		// resolves a cityName without a geoPos through LookupLegacyCity.
		msg.CityName = strings.TrimSpace(q["city"])

	default:
		// getLocationFromQuery reaches its legacy branch only after the named
		// forms and the decimal lat/long, which is why this is last.
		loc, err := location.FromLegacyLatLong(q.values())
		if err != nil {
			return err
		}
		if loc == nil {
			return nil
		}
		msg.GeoPos = true
		msg.LatOneof = &downloadpb.Download_Latitude{Latitude: float32(loc.Latitude)}
		msg.LongOneof = &downloadpb.Download_Longitude{Longitude: float32(loc.Longitude)}
		msg.Tzid = loc.TimeZoneID
		// Either the city-typeahead label or the degrees/minutes rendering of
		// the coordinates that fromLatLongLegacy built from them.
		msg.CityName = loc.Name
	}
	return nil
}

// applyV2DailyLearning is downloadHref2's dailyLearningConfig loop. The list is
// src/dailyLearningConfig.json, whose last entry maps `s` to sedrot -- which is
// why downloadHref2 has no separate line setting it.
//
// This is a fourth copy of the mapping named in CLAUDE.md's daily-learning
// section (alongside learningSchedules, unsupportedSeries and readings-svc's
// queryToDailyLearningName); they move together.
func applyV2DailyLearning(q v2Query, msg *downloadpb.Download) {
	for _, s := range []struct {
		param string
		set   func(*downloadpb.Download)
	}{
		{"F", func(m *downloadpb.Download) { m.Dafyomi = true }},
		{"myomi", func(m *downloadpb.Download) { m.MishnaYomi = true }},
		{"dpy", func(m *downloadpb.Download) { m.PerekYomi = true }},
		{"nyomi", func(m *downloadpb.Download) { m.NachYomi = true }},
		{"dty", func(m *downloadpb.Download) { m.TanakhYomi = true }},
		{"dps", func(m *downloadpb.Download) { m.Psalms = true }},
		{"d929", func(m *downloadpb.Download) { m.Nine29 = true }},
		{"dr1", func(m *downloadpb.Download) { m.Rambam1 = true }},
		{"dr3", func(m *downloadpb.Download) { m.Rambam3 = true }},
		{"dsm", func(m *downloadpb.Download) { m.SeferHaMitzvot = true }},
		{"yyomi", func(m *downloadpb.Download) { m.YerushalmiYomi = true }},
		{"yys", func(m *downloadpb.Download) { m.YySchottenstein = true }},
		{"dcc", func(m *downloadpb.Download) { m.ChofetzChaim = true }},
		{"dshl", func(m *downloadpb.Download) { m.ShemiratHaLashon = true }},
		{"ayd", func(m *downloadpb.Download) { m.DirshuAmudYomi = true }},
		{"dw", func(m *downloadpb.Download) { m.DafWeekly = true }},
		{"dpa", func(m *downloadpb.Download) { m.PirkeiAvotSummer = true }},
		{"ahsy", func(m *downloadpb.Download) { m.ArukhHaShulchanYomi = true }},
		{"dksa", func(m *downloadpb.Download) { m.KitzurShulchanAruch = true }},
		{"s", func(m *downloadpb.Download) { m.Sedrot = true }},
	} {
		if q.on(s.param) {
			s.set(msg)
		}
	}
}

// checkISODate is the YYYY-MM-DD guard isoDateStringToDate() throws 400 from.
// The value itself is parsed later, by applyDateRange.
func checkISODate(s string) error {
	if _, err := parseISODate(s); err != nil {
		return model.BadRequest("Date does not match format YYYY-MM-DD: %s", s)
	}
	return nil
}
