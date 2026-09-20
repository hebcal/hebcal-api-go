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

// Params is everything the renderer needs, decoded from the URL: the protobuf
// carries the user's calendar choices, and these fields are the resolved form
// of them.
type Params struct {
	Opts hebcal.CalOptions

	// MonthMode selects Gregorian- or Hebrew-month pagination.
	MonthMode MonthMode
	// Locale is the resolved locale name ("en", "he", "ashkenazi").
	Locale string
	// LG is the raw `lg` code from the request ("s", "h", "de"). The resolved
	// name drives rendering; this raw code is retained for building the legacy
	// /v2/ and CGI query strings.
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
	// "New York City"), which the link campaign uses in place of CityName. It is
	// empty for the locations that have none -- lat/long and ZIP -- and CityName
	// stands in. See CampaignName.
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
	// AppendHebrew appends each event's Hebrew name to its rendered subject.
	// Set by lg=ah and lg=sh.
	AppendHebrew bool
	// PerEventCampaign takes each link's uc= / utm_campaign value from that
	// event's own Hebrew year ("pdf-5787") instead of from the document title.
	// The /holidays/ calendars are rendered this way; the /v4/ downloads set the
	// campaign from the document title instead.
	PerEventCampaign bool
}

// hebrewLocales are the resolved locale names that render right-to-left.
var hebrewLocales = map[string]bool{
	"he": true, "he-x-nonikud": true,
}

// maxNumYears bounds a multi-year calendar. An explicitly requested span is
// capped at 10, which also bounds the work a scanner can ask for.
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
// without padding.
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

// ParamsFromMessage resolves a decoded Download message into Params, writing
// the user's calendar choices straight into hebcal.CalOptions.
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
	// lg=ah and lg=sh render the transliteration and then the Hebrew name of
	// each event.
	p.AppendHebrew = p.LG == "ah" || p.LG == "sh"

	o := &p.Opts
	// The protobuf carries positive "include this" booleans; CalOptions uses
	// negative "suppress this" flags. Invert as we copy.
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
	// the coming month. This belongs here rather than in hebcal-go: it is a
	// convention about what a user who ticked three boxes on a form probably
	// wants, not a rule about the calendar.
	if msg.GetRoshChodesh() && msg.GetSpecialShabbat() && msg.GetSedrot() {
		o.ShabbatMevarchim = true
	}
	// In Gregorian-month mode the alternate date is the Hebrew date, so hebcal-go
	// generates it. In Hebrew-month mode (mm=1/mm=2) the alternate date is the
	// Gregorian date, which hebcal-go does not generate; Generate() synthesizes
	// it from p.AddAltDates / p.AddAltDatesForEvents instead.
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
		// A download URL that did not ask for a specific Havdalah time means
		// "no Havdalah". hebcal-go reads a zero HavdalahMins as "use the
		// default tzeit" and would draw one anyway, so ask it to suppress. A
		// non-default offset (m>0) or tzeit (M=on, handled above) both keep
		// Havdalah.
		o.SuppressHavdalah = o.HavdalahMins == 0
	}
	o.CandleLightingMins = int(msg.GetCandleLightingMins())

	if err := applyDateRange(msg, p); err != nil {
		return nil, err
	}
	// A single-year request outside the supported range is 410. A start/end
	// range leaves Year zero and is not checked.
	if o.Year != 0 && !model.YearIsSupported(o.Year, o.IsHebrewYear) {
		return nil, &OutOfRangeError{Year: o.Year, IsHebrewYear: o.IsHebrewYear}
	}
	if err := applyLocation(msg, p, db); err != nil {
		return nil, err
	}
	// Candle-lighting is switched off for years before the modern zmanim tables
	// begin -- Gregorian before 1900, Hebrew before 5661 -- even when a location
	// is present. A start/end range leaves Year zero and is left alone.
	if o.CandleLighting && o.Year != 0 {
		if (o.IsHebrewYear && o.Year < 5661) || o.Year < 1900 {
			o.CandleLighting = false
		}
	}
	applyDailyLearning(msg, o)
	return p, nil
}

// defaultCandleMins is the default number of minutes before sunset that candles
// are lit.
const defaultCandleMins = 18

// geonameIDCandleOffset holds the Israeli cities whose customary candle-lighting
// offset is larger than the 20-minute default.
var geonameIDCandleOffset = map[int]int{
	281184: 40, // Jerusalem
	294801: 30, // Haifa
	293067: 30, // Zikhron Yaakov
}

// locationDefaultCandleMins reports the default candle-lighting offset for a
// location: an Israeli location lights candles earlier than the diaspora
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

// applyIsraelCandleMins applies the Israel candle-lighting rule: an Israeli
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
// asked for it: without this rule a calendar carrying a geonameid but no c=on
// renders with no times at all.
func setLocation(p *Params, loc *geodb.Location, msg *downloadpb.Download) error {
	zl := loc.ZmanimLocation()
	if !msg.GetUseElevation() {
		// The stored elevation is only honoured when the request asked for
		// elevation-aware zmanim; otherwise sea level is used.
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

// parseISODate parses YYYY-MM-DD into an HDate.
func parseISODate(s string) (hdate.HDate, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return hdate.HDate{}, err
	}
	return hdate.FromTime(t), nil
}

// OutOfRangeError marks a year there is no calendar for. The handler answers
// these with HTTP 410 Gone, which also keeps a far-future request from ever
// reaching the generator.
type OutOfRangeError struct {
	Year         int
	IsHebrewYear bool
}

func (e *OutOfRangeError) Error() string {
	if e.IsHebrewYear {
		return fmt.Sprintf("No calendar for Hebrew year %d", e.Year)
	}
	return fmt.Sprintf("No calendar for Gregorian year %d", e.Year)
}

// NotFoundError marks a named location (geonameid, ZIP or legacy city) that
// could not be resolved. The handler answers these with HTTP 404, reserving
// 400 for malformed input.
type NotFoundError struct{ msg string }

func (e *NotFoundError) Error() string { return e.msg }

// NotFoundf builds a NotFoundError with a formatted message.
func NotFoundf(format string, a ...any) error {
	return &NotFoundError{msg: fmt.Sprintf(format, a...)}
}

// applyLocation resolves the candle-lighting location. A lat/long ("geoPos")
// calendar needs nothing else; geonameid, ZIP and legacy-city calendars are
// resolved against the SQLite geo databases.
func applyLocation(msg *downloadpb.Download, p *Params, db *geodb.DB) error {
	// Resolve whenever a location was given, not only when candle-lighting was
	// asked for. The location names the calendar -- "Hebcal Prestea 2008"
	// rather than "Hebcal Diaspora 2008" -- and the footer reports its
	// candle-lighting offset even for a calendar that carries no times.
	if !msg.GetGeoPos() && msg.GetGeonameid() == 0 && msg.GetZip() == "" &&
		msg.GetCityName() == "" {
		// No location: candle-lighting is impossible, so drop it even when the
		// request asked for it.
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
		// Validate the timezone before it reaches the generator: zmanim.New
		// panics if the tzid cannot be loaded, and IANA names are case-sensitive,
		// so a client-supplied "america/new_york" would otherwise crash the
		// request (recovered as a 500). Report it as a bad request instead.
		if _, err := zmanim.LoadLocation(tzid); err != nil {
			return fmt.Errorf("invalid time zone specified: %s", tzid)
		}
		// A lat/long location is treated as Israel when the request said so or
		// the timezone is Asia/Jerusalem.
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
		// only a /v2/ URL carries. It is a lookup key -- "GB-London" -- rather
		// than a label, so clear it and let setLocation name the calendar after
		// the resolved location.
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
	// The dw checkbox resolves to dafWeeklySunday (one row each Sunday), not
	// dafWeekly (the same daf drawn all seven days). The protobuf field keeps
	// its dw-derived name.
	{"dafWeeklySunday", func(m *downloadpb.Download) bool { return m.GetDafWeekly() }},
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
// explicitly selected, so their rows are fetched from the readings-svc sidecar
// and merged instead (see fallback.go).
//
// These seven have no schedule in github.com/hebcal/learning. Keep this list and
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
		{"dirshuDafHalacha", msg.GetDirshuDafHalacha()},
	} {
		if s.on {
			out = append(out, s.name)
		}
	}
	return out
}
