package pdf

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"
	// Registers every daily-learning schedule with hebcal-go's dailylearning
	// registry through their init functions. Imported for the side effect only;
	// learningSchedules below then resolves them by name. It belongs here, in
	// the package that does the resolving, rather than in main: dropping it
	// silently reduces a calendar to the four schedules hebcal-go hard-wires.
	_ "github.com/hebcal/learning"
	"google.golang.org/protobuf/proto"

	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// MonthMode mirrors the Download.MonthMode enum: how the calendar is paginated
// and how each page is titled.
type MonthMode int32

const (
	// GregorianArabic is one page per Gregorian month.
	GregorianArabic MonthMode = 0
	// HebrewArabic is one page per Hebrew month, titled with Arabic numerals.
	HebrewArabic MonthMode = 1
	// HebrewHebrew is one page per Hebrew month, titled with gematriya.
	HebrewHebrew MonthMode = 2
)

// Params is everything the renderer needs, decoded from the URL. It is the Go
// equivalent of what hebcal-web builds by running deserializeDownload() into
// makeDownloadProps(): the protobuf carries the user's calendar choices, and
// these fields are the resolved form of them.
type Params struct {
	Opts hebcal.CalOptions

	// MonthMode selects Gregorian- or Hebrew-month pagination.
	MonthMode MonthMode
	// Locale is the resolved locale name ("en", "he", "ashkenazi").
	Locale string
	// LG is the raw `lg` code from the request ("s", "h", "de"). The resolved
	// name drives rendering; this is what hebcal-web's own query strings want.
	LG string
	// RTL is true when the calendar renders right-to-left (Hebrew locales).
	RTL bool
	// AddAltDates prints the alternate (Hebrew or Gregorian) date in each cell.
	AddAltDates bool
	// AddAltDatesForEvents prints an alternate date alongside each event.
	AddAltDatesForEvents bool
	// Emoji keeps holiday emoji in rendered event titles.
	Emoji bool
	// Euro selects DD/MM day ordering in Gregorian mode.
	Euro bool
	// Hour12 is 1 to force 12-hour times, 2 to force 24-hour, 0 for locale default.
	Hour12 int32
	// CityName is the typeahead label for a lat/long location, used in the subtitle.
	CityName string
	// CityNameAscii is the location's plain-ASCII geonames name ("Zuerich",
	// "New York City"), which the link campaign uses in place of CityName.
	// getCalendarTitle takes it whenever it is a string, so it is empty for the
	// locations that have none -- lat/long and ZIP -- and CityName stands in.
	// See CampaignName.
	CityNameAscii string
	// Subscribe marks the calendar as a subscription (affects the footer only).
	Subscribe bool
	// YomTovOnly suppresses non-yom-tov holidays.
	YomTovOnly bool
	// LocationName is the full location name ("Palo Alto, CA 94303"), used in
	// the document keywords. CityName carries the short form used in titles.
	LocationName string
	// NoMinorHolidays drops MINOR_HOLIDAY events after generation. hebcal-go's
	// CalOptions has NoHolidays, NoMinorFast, NoModern, NoRoshChodesh and
	// NoSpecialShabbat but no equivalent for minor holidays, so this one is
	// applied as a flag filter in Generate rather than as a calendar option.
	NoMinorHolidays bool
	// AppendHebrew appends each event's Hebrew name to its rendered subject, the
	// `appendHebrewToSubject` option that lg=ah and lg=sh set in src/calendar.js.
	AppendHebrew bool
	// PerEventCampaign takes each link's uc= / utm_campaign value from that
	// event's own Hebrew year ("pdf-5787") instead of from the document title.
	// That is renderPdfEvent's fallback when options.utmCampaign is unset, which
	// is how the /holidays/ calendars are rendered: hebcal-download.js sets the
	// campaign from the title, holidayPdf.js sets nothing.
	PerEventCampaign bool
}

// hebrewLocales are the resolved locale names that render right-to-left.
var hebrewLocales = map[string]bool{
	"he": true, "he-x-nonikud": true,
}

// maxNumYears bounds a multi-year calendar. hebcal-web caps an explicitly
// requested span at 10 in getNumYears() (src/calendar.js), which makeHebcalOptions
// applies to every PDF request; matching that keeps a 10-year request the same
// length in both, and also bounds the work a scanner can ask for (see the
// event-loop note in hebcal-web's CLAUDE.md).
const maxNumYears = 10

// useGematriya reports whether day numbers and years are written in Hebrew
// numerals, which is what mm=2 (HEBREW_HEBREW) asks for.
func (p *Params) useGematriya() bool {
	return p.MonthMode == HebrewHebrew
}

// ParsePath extracts the base64url protobuf payload from a download URL of the
// form /v4/<base64>/<filename>.pdf. It returns an error for any other shape.
func ParsePath(path string) (string, error) {
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	if len(parts) != 3 || parts[0] != "v4" {
		return "", fmt.Errorf("expected /v4/<data>/<filename>.pdf, got %q", path)
	}
	if !strings.HasSuffix(parts[2], ".pdf") {
		return "", errors.New("not a .pdf request")
	}
	if parts[1] == "" {
		return "", errors.New("empty payload")
	}
	return parts[1], nil
}

// decodeBase64 accepts both the standard and URL-safe alphabets, with or
// without padding. hebcal-web writes these with Node's Buffer.from(s,'base64'),
// which is permissive in the same way.
func decodeBase64(s string) ([]byte, error) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "-", "+"), "_", "/")
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return base64.StdEncoding.DecodeString(s)
}

// DecodeMessage turns the base64 protobuf payload from a /v4/ URL back into
// the Download message it encodes.
func DecodeMessage(payload string) (*downloadpb.Download, error) {
	raw, err := decodeBase64(payload)
	if err != nil {
		return nil, fmt.Errorf("base64: %w", err)
	}
	var msg downloadpb.Download
	if err := proto.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("protobuf: %w", err)
	}
	return &msg, nil
}

// DecodeParams turns the base64 protobuf payload from a /v4/ URL into Params.
func DecodeParams(payload string, db *geodb.DB) (*Params, error) {
	msg, err := DecodeMessage(payload)
	if err != nil {
		return nil, err
	}
	return ParamsFromMessage(msg, db)
}

// ParamsFromMessage resolves a decoded Download message into Params.
//
// This is the Go port of hebcal-web's src/deserializeDownload.js followed by
// the parts of src/makeDownloadProps.js that matter to the PDF: rather than
// round-tripping through a query-string map the way the Node code does, it
// writes straight into hebcal.CalOptions.
func ParamsFromMessage(msg *downloadpb.Download, db *geodb.DB) (*Params, error) {
	p := &Params{
		MonthMode:            MonthMode(msg.GetMonthMode()),
		AddAltDates:          msg.GetAddAltDates(),
		AddAltDatesForEvents: msg.GetAddAltDatesForEvents(),
		Emoji:                msg.GetEmoji(),
		Euro:                 msg.GetEuro(),
		Hour12:               int32(msg.GetHour12()),
		CityName:             msg.GetCityName(),
		Subscribe:            msg.GetSubscribe(),
		YomTovOnly:           msg.GetYomTovOnly(),
		NoMinorHolidays:      !msg.GetMinor(),
	}

	p.LG = msg.GetLocale()
	p.Locale = model.AliasLocale(p.LG)
	p.RTL = hebrewLocales[strings.ToLower(p.Locale)]
	// lg=ah and lg=sh render the transliteration and then the Hebrew name of each
	// event (src/calendar.js sets appendHebrewToSubject for exactly these two).
	p.AppendHebrew = p.LG == "ah" || p.LG == "sh"

	o := &p.Opts
	// deserializeDownload.js maps the booleans to `maj`/`min`/... query params,
	// which makeDownloadProps then inverts into these No* suppression flags.
	// Going straight to CalOptions skips that double negative.
	o.NoHolidays = !msg.GetMajor()
	o.NoRoshChodesh = !msg.GetRoshChodesh()
	o.NoModern = !msg.GetModern()
	o.NoMinorFast = !msg.GetMinorFast()
	o.NoSpecialShabbat = !msg.GetSpecialShabbat()

	o.IL = msg.GetIsrael()
	o.Sedrot = msg.GetSedrot()
	o.CandleLighting = msg.GetCandlelighting()
	o.IsHebrewYear = msg.GetIsHebrewYear()
	o.Omer = msg.GetOmer()
	o.YomKippurKatan = msg.GetYomKippurKatan()
	o.ShabbatMevarchim = msg.GetShabbatMevarchim()

	// Asking for Rosh Chodesh, the special Shabbatot and the weekly Torah
	// reading together implies Shabbat Mevarchim, the Shabbat that announces
	// the coming month. hebcal-web does this in src/calendar.js by setting the
	// SHABBAT_MEVARCHIM bit in the query mask, which @hebcal/core then turns
	// back into options.shabbatMevarchim.
	//
	// It belongs here rather than in hebcal-go: it is a convention about what
	// a user who ticked three boxes on a form probably wants, not a rule about
	// the calendar. hebcal-go has no mask and no notion of query parameters.
	if msg.GetRoshChodesh() && msg.GetSpecialShabbat() && msg.GetSedrot() {
		o.ShabbatMevarchim = true
	}
	// In Gregorian-month mode the alternate date is the Hebrew date, so hebcal-go
	// generates it. In Hebrew-month mode (mm=1/mm=2) the alternate date is the
	// Gregorian date, which hebcal-go does not generate -- src/calendar.js only
	// sets addHebrewDates when !hebrewMonths, and instead inserts its own
	// GregorianDateEvents. Generate() does the equivalent from p.AddAltDates /
	// p.AddAltDatesForEvents.
	if p.MonthMode == GregorianArabic {
		o.AddHebrewDates = msg.GetAddAltDates()
		o.AddHebrewDatesForEvents = msg.GetAddAltDatesForEvents()
	}
	o.UseElevation = msg.GetUseElevation()

	if msg.GetHavdalahTzeit() {
		// Tzeit-based havdalah: leave HavdalahMins zero so hebcal-go uses
		// degrees, and honour a custom depression angle if one was given.
		if tz := msg.GetTzeit(); tz != 0 {
			o.HavdalahDeg = float64(tz)
		}
	} else {
		o.HavdalahMins = int(msg.GetHavdalahMins())
		// deserializeDownload.js sets q.m = havdalahMins whenever M=off, and
		// makeHebcalOptions leaves options.havdalahMins === 0 in that case, which
		// @hebcal/core reads as "no Havdalah" (calendar.js suppresses any
		// HavdalahEvent when havdalahMins===0). hebcal-go instead reads a zero
		// HavdalahMins as "use the default tzeit", so it would draw Havdalah where
		// production draws none; ask it to suppress. A non-default offset (m>0) or
		// tzeit (M=on, handled above) both keep Havdalah.
		o.SuppressHavdalah = o.HavdalahMins == 0
	}
	o.CandleLightingMins = int(msg.GetCandleLightingMins())

	if err := applyDateRange(msg, p); err != nil {
		return nil, err
	}
	// A single-year request outside the supported range is 410, matching
	// hebcal-download.js. A start/end range leaves Year zero and is not checked.
	if o.Year != 0 && !YearIsSupported(o.Year, o.IsHebrewYear) {
		return nil, &OutOfRangeError{Year: o.Year}
	}
	if err := applyLocation(msg, p, db); err != nil {
		return nil, err
	}
	// Candle-lighting is switched off for years before the modern zmanim tables
	// begin -- Gregorian before 1900, Hebrew before 5661 -- even when a location
	// is present (src/calendar.js). A start/end range leaves Year zero and is
	// left alone, matching the `typeof options.year === 'number'` guard.
	if o.CandleLighting && o.Year != 0 {
		if (o.IsHebrewYear && o.Year < 5661) || o.Year < 1900 {
			o.CandleLighting = false
		}
	}
	applyDailyLearning(msg, o)
	return p, nil
}

// defaultCandleMins is the default number of minutes before sunset that candles
// are lit, matching DEFAULT_CANDLE_MINS in hebcal-web's src/urlArgs.js.
const defaultCandleMins = 18

// geonameIDCandleOffset holds the Israeli cities whose customary candle-lighting
// offset is larger than the 20-minute default, matching geonameIdCandleOffset in
// hebcal-web's src/urlArgs.js.
var geonameIDCandleOffset = map[int]int{
	281184: 40, // Jerusalem
	294801: 30, // Haifa
	293067: 30, // Zikhron Yaakov
}

// locationDefaultCandleMins is the port of locationDefaultCandleMins() in
// src/urlArgs.js: an Israeli location lights candles earlier than the diaspora
// default, either by a city-specific amount or by the 20-minute fallback.
func locationDefaultCandleMins(loc *geodb.Location) int {
	if loc.IsIsrael() {
		if off, ok := geonameIDCandleOffset[loc.GeonameID]; ok {
			return off
		}
		return 20
	}
	return defaultCandleMins
}

// applyIsraelCandleMins mirrors the Israel branch of src/calendar.js: an Israeli
// location uses its own default candle-lighting offset unless the request set a
// non-default offset of its own. `given` is the requested offset (0 if unset),
// `offset` the location's default.
func applyIsraelCandleMins(o *hebcal.CalOptions, given, offset int) {
	if given == 0 || (offset != defaultCandleMins && given == defaultCandleMins) {
		o.CandleLightingMins = offset
	}
}

// setLocation copies a resolved geo location into the calendar options and
// records the display name used in the title and subtitle.
//
// A resolved location implies candle-lighting, regardless of whether the request
// asked for it -- this is the `if (location) options.candlelighting = true` rule
// in src/calendar.js, without which a calendar carrying a geonameid but no c=on
// renders with no times at all.
func setLocation(p *Params, loc *geodb.Location, msg *downloadpb.Download) error {
	zl := loc.ZmanimLocation()
	if !msg.GetUseElevation() {
		// hebcal-web only honours the stored elevation when the request asked
		// for elevation-aware zmanim; otherwise sea level is used.
		zl.Elevation = 0
	}
	p.Opts.Location = &zl
	p.LocationName = zl.Name
	if p.CityName == "" {
		p.CityName = loc.ShortName()
	}
	// Only the geonames rows carry an asciiname; a ZIP location has none, and
	// its short name is already ASCII ("Palo Alto", "Washington, DC").
	p.CityNameAscii = loc.Asciiname
	p.Opts.CandleLighting = true
	if loc.IsIsrael() {
		p.Opts.IL = true
		applyIsraelCandleMins(&p.Opts, int(msg.GetCandleLightingMins()), locationDefaultCandleMins(loc))
	}
	return nil
}

// applyDateRange resolves the year/month/start/end fields. The protobuf can
// express three different things here and only one of them may be set.
func applyDateRange(msg *downloadpb.Download, p *Params) error {
	o := &p.Opts

	startStr, endStr := msg.GetStartStr(), msg.GetEndStr()
	if startStr == "" && msg.GetStart() != 0 {
		startStr = time.Unix(msg.GetStart(), 0).UTC().Format("2006-01-02")
	}
	if endStr == "" && msg.GetEnd() != 0 {
		endStr = time.Unix(msg.GetEnd(), 0).UTC().Format("2006-01-02")
	}
	if startStr != "" && endStr != "" {
		start, err := parseISODate(startStr)
		if err != nil {
			return fmt.Errorf("start date: %w", err)
		}
		end, err := parseISODate(endStr)
		if err != nil {
			return fmt.Errorf("end date: %w", err)
		}
		if end.Abs() < start.Abs() {
			return errors.New("end date precedes start date")
		}
		o.Start, o.End = start, end
		return nil
	}

	if msg.GetYearNow() {
		now := time.Now()
		if o.IsHebrewYear {
			o.Year = hdate.FromTime(now).Year()
		} else {
			o.Year = now.Year()
		}
	} else if y := msg.GetYear(); y != 0 {
		o.Year = int(y)
	} else {
		o.Year = time.Now().Year()
	}

	if m := msg.GetMonth(); m >= 1 && m <= 12 {
		o.Month = time.Month(m)
	}
	if n := msg.GetNumYears(); n > 1 {
		if n > maxNumYears {
			n = maxNumYears
		}
		o.NumYears = int(n)
	}
	return nil
}

// parseISODate parses YYYY-MM-DD into an HDate, the way hebcal-web's
// makeDownloadProps does before handing dates to @hebcal/core.
func parseISODate(s string) (hdate.HDate, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return hdate.HDate{}, err
	}
	return hdate.FromTime(t), nil
}

// OutOfRangeError marks a year hebcal-web has no calendar for. hebcal-web's
// hebcal-download.js answers these with HTTP 410 Gone, which also keeps a
// far-future request from ever reaching the generator.
type OutOfRangeError struct{ Year int }

func (e *OutOfRangeError) Error() string {
	return fmt.Sprintf("No calendar for year %d", e.Year)
}

// YearIsSupported mirrors yearIsOutsideGregRange / yearIsOutsideHebRange in
// src/dateUtil.js: no calendar is served before year 100 or after 2999
// (Gregorian), or before 3860 or after 6759 (Hebrew). The /holidays/ calendars
// range-check their own year with it too.
func YearIsSupported(year int, hebrew bool) bool {
	if hebrew {
		return year >= 3860 && year <= 6759
	}
	return year >= 100 && year <= 2999
}

// NotFoundError marks a named location (geonameid, ZIP or legacy city) that
// could not be resolved. hebcal-web's getLocationFromQuery reports these with
// HTTP 404 ("Sorry, can't find …"), reserving 400 for malformed input, so the
// handler distinguishes the two.
type NotFoundError struct{ msg string }

func (e *NotFoundError) Error() string { return e.msg }

// NotFoundf builds a NotFoundError with a formatted message.
func NotFoundf(format string, a ...any) error {
	return &NotFoundError{msg: fmt.Sprintf(format, a...)}
}

// applyLocation resolves the candle-lighting location. A lat/long ("geoPos")
// calendar needs nothing else; geonameid, ZIP and legacy-city calendars are
// resolved against the SQLite geo databases, the same ones @hebcal/geo-sqlite
// reads in hebcal-web.
func applyLocation(msg *downloadpb.Download, p *Params, db *geodb.DB) error {
	// Resolve whenever a location was given, not only when candle-lighting was
	// asked for. The location names the calendar -- "Hebcal Prestea 2008"
	// rather than "Hebcal Diaspora 2008" -- and hebcal-web's footer reports its
	// candle-lighting offset even for a calendar that carries no times.
	if !msg.GetGeoPos() && msg.GetGeonameid() == 0 && msg.GetZip() == "" &&
		msg.GetCityName() == "" {
		// No location: candle-lighting is impossible, so hebcal-web deletes it
		// even when the request asked for it (the `else` in src/calendar.js).
		p.Opts.CandleLighting = false
		return nil
	}
	if msg.GetGeoPos() {
		lat := float64(msg.GetLatitude())
		if msg.GetOldLatitude() != 0 {
			lat = msg.GetOldLatitude()
		}
		long := float64(msg.GetLongitude())
		if msg.GetOldLongitude() != 0 {
			long = msg.GetOldLongitude()
		}
		tzid := msg.GetTzid()
		if tzid == "" {
			return errors.New("geoPos location without tzid")
		}
		// getLocationFromQuery treats a lat/long location as Israel when the
		// request said so or the timezone is Asia/Jerusalem.
		il := msg.GetIsrael() || tzid == "Asia/Jerusalem"
		name := msg.GetCityName()
		cc := ""
		if il {
			cc = "IL"
		}
		p.Opts.Location = &zmanim.Location{
			Name:        name,
			Latitude:    lat,
			Longitude:   long,
			TimeZoneId:  tzid,
			CountryCode: cc,
			Elevation:   int(msg.GetElev()),
		}
		p.Opts.CandleLighting = true
		if il {
			p.Opts.IL = true
			// A lat/long location has no geonameid, so its Israel default is the
			// 20-minute fallback.
			applyIsraelCandleMins(&p.Opts, int(msg.GetCandleLightingMins()), 20)
		}
		return nil
	}
	if db == nil {
		// A named location was requested but cannot be resolved without the
		// databases. Because a location now implies candle-lighting, neither the
		// calendar's name nor its times can be produced correctly, so this is
		// fatal rather than a silently unnamed calendar.
		return errors.New("named location requires the geo databases")
	}
	if id := msg.GetGeonameid(); id != 0 {
		loc := db.LookupGeoname(int(id))
		if loc == nil {
			return NotFoundf("unknown geonameid %d", id)
		}
		return setLocation(p, loc, msg)
	}
	if zip := msg.GetZip(); zip != "" {
		loc := db.LookupZip(zip)
		if loc == nil {
			return NotFoundf("unknown zip %s", zip)
		}
		return setLocation(p, loc, msg)
	}
	if city := msg.GetCityName(); city != "" {
		// A cityName with no geoPos is a legacy Hebcal city identifier, which
		// only a /v2/ URL carries (downloadHref2 sets cityName only alongside
		// geoPos, from the typeahead). It is a lookup key -- "GB-London" --
		// rather than a label, so clear it and let setLocation name the
		// calendar after the resolved location, as getCalendarTitle does.
		if loc := db.LookupLegacyCity(city); loc != nil {
			p.CityName = ""
			return setLocation(p, loc, msg)
		}
		return NotFoundf("unknown city %q", city)
	}
	return errors.New("location could not be resolved")
}

// learningSchedules maps each protobuf field to the name it is registered
// under in hebcal-go's dailylearning registry.
//
// The registry is populated by importing github.com/hebcal/learning for its
// side effects, which every schedule's init() uses to register itself. Four of
// these also have dedicated CalOptions booleans; going through the registry
// for all of them keeps one list rather than two mechanisms.
var learningSchedules = []struct {
	name string
	on   func(*downloadpb.Download) bool
}{
	{"dafYomi", func(m *downloadpb.Download) bool { return m.GetDafyomi() }},
	{"mishnaYomi", func(m *downloadpb.Download) bool { return m.GetMishnaYomi() }},
	{"nachYomi", func(m *downloadpb.Download) bool { return m.GetNachYomi() }},
	{"yerushalmi-vilna", func(m *downloadpb.Download) bool { return m.GetYerushalmiYomi() }},
	{"yerushalmi-schottenstein", func(m *downloadpb.Download) bool { return m.GetYySchottenstein() }},
	{"perekYomi", func(m *downloadpb.Download) bool { return m.GetPerekYomi() }},
	{"dafWeekly", func(m *downloadpb.Download) bool { return m.GetDafWeekly() }},
	{"929", func(m *downloadpb.Download) bool { return m.GetNine29() }},
	{"psalms", func(m *downloadpb.Download) bool { return m.GetPsalms() }},
	{"rambam1", func(m *downloadpb.Download) bool { return m.GetRambam1() }},
	{"rambam3", func(m *downloadpb.Download) bool { return m.GetRambam3() }},
	{"tanakhYomi", func(m *downloadpb.Download) bool { return m.GetTanakhYomi() }},
	{"pirkeiAvotSummer", func(m *downloadpb.Download) bool { return m.GetPirkeiAvotSummer() }},
}

// applyDailyLearning enables the requested schedules through CalOptions'
// generic DailyLearning list, which hebcal-go resolves against the registry.
func applyDailyLearning(msg *downloadpb.Download, o *hebcal.CalOptions) {
	for _, s := range learningSchedules {
		if s.on(msg) {
			o.DailyLearning = append(o.DailyLearning, s.name)
		}
	}
}

// unsupportedSeries reports the daily-learning series a request asked for that
// hebcal-go cannot generate. Rendering anyway would silently drop rows the user
// explicitly selected, so callers hand these requests back to the Node service
// instead.
//
// These six have no schedule in github.com/hebcal/learning. Keep this list and
// learningSchedules together: anything the learning package gains should move
// from here to there.
func unsupportedSeries(msg *downloadpb.Download) []string {
	var out []string
	for _, s := range []struct {
		name string
		on   bool
	}{
		{"chofetzChaim", msg.GetChofetzChaim()},
		{"shemiratHaLashon", msg.GetShemiratHaLashon()},
		{"arukhHaShulchanYomi", msg.GetArukhHaShulchanYomi()},
		{"seferHaMitzvot", msg.GetSeferHaMitzvot()},
		{"kitzurShulchanAruch", msg.GetKitzurShulchanAruch()},
		{"dirshuAmudYomi", msg.GetDirshuAmudYomi()},
	} {
		if s.on {
			out = append(out, s.name)
		}
	}
	return out
}
