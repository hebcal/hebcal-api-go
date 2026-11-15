// Package webconfig holds the two calendar configuration files this service
// shares with hebcal-web, embedded in the binary: dailyLearningConfig.json,
// one entry per daily-learning series, and staticCalendars.json, one entry
// per static holiday, Hebrew-date, Torah-reading and Yizkor feed.
//
// The files are verbatim copies of hebcal-web's src/ originals, and the copies
// here are not edited by hand: change hebcal-web's, then run
// `make sync-webconfig` to copy them over. Everything that names a series --
// the download protobuf fields, the legacy query codes, the order of a day's
// learning rows, which series readings-svc supplies, the static /ical/ feeds --
// is derived from these entries by the packages that use them.
package webconfig

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

//go:embed dailyLearningConfig.json
var dailyLearningJSON []byte

//go:embed staticCalendars.json
var staticCalendarsJSON []byte

// LearningSeries is one entry of dailyLearningConfig.json. Only the fields
// this service reads are decoded; the rest (descLong, group, settingsName,
// feedLength, the /learning page's dailyLearningOptOverride) are the
// website's, and an entry may carry any others without affecting anything
// here.
type LearningSeries struct {
	// EventCategory names the series as its events' categories do: the first
	// category, or the first two joined with "-" for Yerushalmi
	// ("yerushalmi-vilna"). For a series readings-svc supplies it is the
	// Category of the rows it returns.
	EventCategory string `json:"eventCategory"`
	// QueryParam is the series' query-string flag ("F", "dr1", "dtn"), both
	// on the legacy download URLs and in readings-svc's /learning request.
	QueryParam string `json:"queryParam"`
	// DailyLearningOptName is the schedule's name in the dailylearning
	// registry ("dafYomi", "rambam1"), and empty for an entry that is not a
	// daily-learning series (the Torah readings).
	DailyLearningOptName string `json:"dailyLearningOptName"`
	// ProtocName is the series' bool field in the Download protobuf.
	ProtocName string `json:"protocName"`
	// DownloadSlug is the static feed's file name under /ical/, or empty for
	// a series with no feed of its own.
	DownloadSlug string `json:"downloadSlug"`
	// Color, RelCalID, ShortName and Years describe the static feed: its
	// X-APPLE-CALENDAR-COLOR, X-WR-RELCALID, X-WR-CALNAME and how many years
	// ahead it reaches. A RelCalID must never change once published.
	Color     string `json:"color"`
	RelCalID  string `json:"relcalid"`
	ShortName string `json:"shortName"`
	Years     int    `json:"years"`
	// DescMedium is the series' one-line description, and the feed's
	// X-WR-CALDESC unless CalDesc gives a longer one.
	DescMedium string `json:"descMedium"`
	CalDesc    string `json:"caldesc"`
}

// FeedDesc is the static feed's X-WR-CALDESC.
func (s *LearningSeries) FeedDesc() string {
	if s.CalDesc != "" {
		return s.CalDesc
	}
	return s.DescMedium
}

// StaticCalendar is one entry of staticCalendars.json. hebcal-web hands the
// entry to @hebcal/core's calendar() as its options, so every key either
// selects events (and has a field here) or is listed in staticWebsiteOnly;
// a key that is neither fails the decode rather than being silently dropped.
type StaticCalendar struct {
	DownloadSlug  string `json:"downloadSlug"`
	Title         string `json:"title"`
	CalDesc       string `json:"caldesc"`
	CalendarColor string `json:"calendarColor"`
	RelCalID      string `json:"relcalid"`
	Locale        string `json:"locale"`
	Years         int    `json:"years"`
	// Emoji puts each holiday's emoji in its title.
	Emoji bool `json:"emoji"`
	// NoMinorHolidays drops the minor holidays (Tu BiShvat, Lag BaOmer...),
	// keeping Purim and Chanukah.
	NoMinorHolidays bool `json:"noMinorHolidays"`

	// The calendar() options. Mask is an @hebcal/core flags bitmask, whose
	// bits hebcal-go's event.Flags shares.
	Mask             uint32 `json:"mask"`
	IL               bool   `json:"il"`
	NoHolidays       bool   `json:"noHolidays"`
	NoRoshChodesh    bool   `json:"noRoshChodesh"`
	NoModern         bool   `json:"noModern"`
	NoMinorFast      bool   `json:"noMinorFast"`
	NoSpecialShabbat bool   `json:"noSpecialShabbat"`
	AddHebrewDates   bool   `json:"addHebrewDates"`
	Omer             bool   `json:"omer"`
	Sedrot           bool   `json:"sedrot"`
	YomKippurKatan   bool   `json:"yomKippurKatan"`
	ShabbatMevarchim bool   `json:"shabbatMevarchim"`
	Yizkor           bool   `json:"yizkor"`
}

// staticWebsiteOnly are the staticCalendars.json keys that describe a feed
// to the website rather than choose its events -- hebcal-web's ignoreOpts
// in src/staticCalendars.js, less the keys StaticCalendar decodes. noMajor
// is among them: @hebcal/core has no such option.
var staticWebsiteOnly = []string{
	"noMajor", "emojiStr", "titleName", "shortName", "descLong", "feedLength",
	"noCSV", "hasNoEmojiVersion", "ordinal", "he", "name", "summary",
	"moreInfo", "color",
}

var (
	learning        = mustDecode[LearningSeries](dailyLearningJSON, nil)
	staticCalendars = mustDecode[StaticCalendar](staticCalendarsJSON, staticWebsiteOnly)
)

// Learning returns dailyLearningConfig.json's entries, in the file's order.
// The slice is shared; do not modify it.
func Learning() []LearningSeries { return learning }

// StaticCalendars returns staticCalendars.json's entries, in the file's
// order. The slice is shared; do not modify it.
func StaticCalendars() []StaticCalendar { return staticCalendars }

func mustDecode[T any](data []byte, websiteOnly []string) []T {
	out, err := decode[T](data, websiteOnly)
	if err != nil {
		panic(err)
	}
	return out
}

// decode parses a JSON array of T. With websiteOnly non-nil it is strict: a
// key that is neither a field of T nor listed is an error.
func decode[T any](data []byte, websiteOnly []string) ([]T, error) {
	var out []T
	if websiteOnly == nil {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("webconfig: %w", err)
		}
		return out, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("webconfig: %w", err)
	}
	for i, r := range raw {
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(r, &keys); err != nil {
			return nil, fmt.Errorf("webconfig: entry %d: %w", i, err)
		}
		var v T
		dec := json.NewDecoder(bytes.NewReader(stripKeys(keys, websiteOnly)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("webconfig: entry %d: %w (a calendar option this service does not "+
				"implement, or a website-only key missing from staticWebsiteOnly)", i, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// stripKeys re-encodes an object without the listed keys.
func stripKeys(obj map[string]json.RawMessage, drop []string) []byte {
	for k := range obj {
		if slices.Contains(drop, k) {
			delete(obj, k)
		}
	}
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err) // cannot happen: every value is already valid JSON
	}
	return b
}

// SeriesKey names the daily-learning series an event with these categories
// belongs to, in the form EventCategory uses: the first two categories
// joined with "-" when that names a series ("yerushalmi-vilna"), the first
// otherwise.
func SeriesKey(categories []string) string {
	if len(categories) == 0 {
		return ""
	}
	if len(categories) > 1 {
		joined := strings.Join(categories[:2], "-")
		for i := range learning {
			if learning[i].EventCategory == joined {
				return joined
			}
		}
	}
	return categories[0]
}
