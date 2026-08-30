package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/sedra"
	"github.com/hebcal/hebcal-go/zmanim"
	"github.com/hebcal/learning/dafyomi"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
)

// reISODate matches a leading YYYY-MM-DD, the same prefix test as the Node
// isoDateStringToDate.
var reISODate = regexp.MustCompile(`^\d\d\d\d-\d\d-\d\d`)

// parseISODate parses a "YYYY-MM-DD" string into an HDate, mirroring the Node
// isoDateStringToDate (new Date(yy, mm-1, dd) then new HDate(date)). ok is
// false when the string does not match the format.
func parseISODate(s string) (hdate.HDate, bool) {
	if !reISODate.MatchString(s) {
		return hdate.HDate{}, false
	}
	y, _ := strconv.Atoi(s[0:4])
	m, _ := strconv.Atoi(s[5:7])
	d, _ := strconv.Atoi(s[8:10])
	return hdate.FromGregorian(y, time.Month(m), d), true
}

// isoGreg formats an HDate's Gregorian date as YYYY-MM-DD (dayjs'
// format('YYYY-MM-DD')).
func isoGreg(hd hdate.HDate) string {
	return hd.Gregorian().Format("2006-01-02")
}

// renderEn renders an event's English description the way hebcal-mcp gets it
// from @hebcal/core: with the smart apostrophe ("Ta’anit Esther", "CH’’M") and
// the one-m Tamuz spelling that hebcal-go does not apply on its own.
func renderEn(ev event.CalEvent) string {
	return jsutil.SmartApostrophe(model.FixMonthSpelling(ev.Render("en")))
}

// convert-gregorian-to-hebrew

type convertGregorianToHebrewArgs struct {
	Date string `json:"date" jsonschema:"Gregorian date (in yyyy-MM-dd format) to convert"`
}

func (t *tools) convertGregorianToHebrew(_ context.Context, _ *mcpsdk.CallToolRequest, in convertGregorianToHebrewArgs) (*mcpsdk.CallToolResult, any, error) {
	hd, ok := parseISODate(in.Date)
	if !ok {
		return errorCard("Error parsing date: " + in.Date), nil, nil
	}
	return lines(
		"Hebrew year: "+strconv.Itoa(hd.Year()),
		"Hebrew month: "+model.HDMonthNameEn(hd),
		"Day of Hebrew month: "+strconv.Itoa(hd.Day()),
		"Date in Hebrew letters: "+model.GematriyaDate(hd),
		"Is leap year: "+strconv.FormatBool(hd.IsLeapYear()),
	), nil, nil
}

// convert-hebrew-to-gregorian

type convertHebrewToGregorianArgs struct {
	Day   int    `json:"day" jsonschema:"Hebrew day of month"`
	Month string `json:"month" jsonschema:"Hebrew month name transliterated, like Elul or Tishrei"`
	Year  int    `json:"year" jsonschema:"Hebrew year"`
}

func (t *tools) convertHebrewToGregorian(_ context.Context, _ *mcpsdk.CallToolRequest, in convertHebrewToGregorianArgs) (*mcpsdk.CallToolResult, any, error) {
	mon, err := hdate.MonthFromName(in.Month)
	if err != nil {
		return errorCard(`Cannot interpret "` + in.Month + `" as a Hebrew month name`), nil, nil
	}
	hd := hdate.New(in.Year, mon, in.Day)
	return textResult(isoGreg(hd)), nil, nil
}

// yahrzeit

type yahrzeitArgs struct {
	Date        string `json:"date" jsonschema:"Gregorian date of death (in yyyy-MM-dd format)"`
	AfterSunset bool   `json:"afterSunset" jsonschema:"after sunset"`
}

func (t *tools) yahrzeit(_ context.Context, _ *mcpsdk.CallToolRequest, in yahrzeitArgs) (*mcpsdk.CallToolResult, any, error) {
	hd, ok := parseISODate(in.Date)
	if !ok {
		return errorCard("Error parsing date: " + in.Date), nil, nil
	}
	return textResult(strings.Join(doYahrzeit(hd, in.AfterSunset), "\n")), nil, nil
}

// doYahrzeit builds the yahrzeit markdown table for the anniversaries of
// origHd, a port of the Node doYahrzeit. It sweeps two years back through
// twenty years forward of the current Hebrew year, skipping years where the
// anniversary does not fall (getYahrzeitHD returning nothing).
func doYahrzeit(origHd hdate.HDate, afterSunset bool) []string {
	if afterSunset {
		origHd = origHd.Next()
	}
	origHyear := origHd.Year()
	nowYear := hdate.FromTime(time.Now()).Year()
	out := []string{
		"| Anniversary number | Gregorian date | Hebrew year | Hebrew day and month | Date in Hebrew letters |",
		"| ---- | ---- | ---- | ---- | ---- |",
	}
	for hyear := nowYear - 2; hyear <= nowYear+20; hyear++ {
		anniv, err := hdate.GetYahrzeit(hyear, origHd)
		if err != nil {
			continue
		}
		out = append(out, fmt.Sprintf("| %d | %s | %d | %s | %s |",
			anniv.Year()-origHyear, isoGreg(anniv), anniv.Year(),
			hebDayMonthEn(anniv), model.GematriyaDate(anniv)))
	}
	return out
}

// hebDayMonthEn renders a Hebrew date's day and month in English without the
// year, the Node hd.render('en', false): "24th of Tevet", "1st of Sh’vat". The
// apostrophe is smartened as @hebcal/hdate's render() does.
func hebDayMonthEn(hd hdate.HDate) string {
	return humanize.Ordinal(hd.Day()) + " of " + jsutil.SmartApostrophe(model.HDMonthNameEn(hd))
}

// torah-portion

type torahPortionArgs struct {
	Date string `json:"date" jsonschema:"Gregorian date in yyyy-MM-dd format"`
	IL   bool   `json:"il" jsonschema:"True if in Israel, false for Diaspora"`
}

func (t *tools) torahPortion(ctx context.Context, _ *mcpsdk.CallToolRequest, in torahPortionArgs) (*mcpsdk.CallToolResult, any, error) {
	hd, ok := parseISODate(in.Date)
	if !ok {
		return errorCard("Error parsing date: " + in.Date), nil, nil
	}
	il := in.IL
	s := sedra.New(hd.Year(), il)
	parsha := s.Lookup(hd)
	// The parsha is read on the Saturday on or after the date, which is what
	// getHolidaysOnDate, "Date read" and the readings-svc lookup all key on.
	shabbat := hd.OnOrAfter(time.Saturday)

	// The name, Hebrew name and reading summary are the parts hebcal-go cannot
	// produce, so they come from readings-svc's /shabbatTorahReading. On a chag
	// it returns the holiday's own reading (getLeyningForHoliday): the label
	// @hebcal/leyning gives ("Shmini Atzeret (on Shabbat)"), that label in
	// Hebrew, and the merged verse summary. A nil client or an error leaves the
	// reading empty and the tool falls back to what hebcal-go can render.
	var reading readings.ShabbatReading
	if t.rd != nil {
		reading, _ = t.rd.ShabbatTorahReading(ctx, isoGreg(shabbat), il)
	}

	var out []string
	if parsha.Chag {
		// hebcal-go has no ParshaEvent for a chag week, so unlike the original
		// hebcal-mcp -- which prints only the portion and the date on a chag --
		// the Hebrew name and reading come straight from the sidecar's holiday
		// reading when it is available.
		if reading.Name != "" {
			out = append(out, "Torah portion: "+reading.Name)
			if reading.NameHe != "" {
				out = append(out, "Name in Hebrew: "+reading.NameHe)
			}
			if reading.Summary != "" {
				out = append(out, "Reading: "+reading.Summary)
			}
		} else {
			// Sidecar unavailable: hebcal-go's sedra has no chag reading name,
			// so fall back to the holiday on that Saturday, name only.
			out = append(out, "Torah portion: "+chagReadingName(shabbat, il))
		}
	} else {
		out = append(out, "Torah portion: Parashat "+strings.Join(parsha.Name, "-"))
		pe := event.NewParshaEvent(shabbat, parsha, il)
		out = append(out, "Name in Hebrew: "+pe.Render("he"))
		if reading.Summary != "" {
			out = append(out, "Reading: "+reading.Summary)
		}
		for _, h := range hebcal.GetHolidaysOnDate(shabbat, il) {
			if h.GetFlags()&event.SPECIAL_SHABBAT != 0 {
				out = append(out, "Special Shabbat: "+renderEn(h))
				break
			}
		}
	}
	out = append(out, "Date read: "+isoGreg(shabbat))
	return textResult(strings.Join(out, "\n")), nil, nil
}

// chagReadingName is the fallback name for the holiday reading that displaces
// the parsha on a chag Shabbat, used only when readings-svc is unavailable.
// hebcal-go's sedra returns no name for a chag week (unlike @hebcal/core), so
// the name comes from the holiday on that Saturday -- a coarser label
// ("Pesach V (CH”M)") than the sidecar's ("Pesach Shabbat Chol ha-Moed").
func chagReadingName(shabbat hdate.HDate, il bool) string {
	if hols := hebcal.GetHolidaysOnDate(shabbat, il); len(hols) > 0 {
		return renderEn(hols[0])
	}
	return ""
}

// jewish-holidays-year

type jewishHolidaysYearArgs struct {
	Year int `json:"year" jsonschema:"Gregorian year"`
}

func (t *tools) jewishHolidaysYear(_ context.Context, _ *mcpsdk.CallToolRequest, in jewishHolidaysYearArgs) (*mcpsdk.CallToolResult, any, error) {
	events, err := hebcal.HebrewCalendar(&hebcal.CalOptions{Year: in.Year, IsHebrewYear: false})
	if err != nil {
		return errorCard("Error: " + err.Error()), nil, nil
	}
	out := []string{
		"| Gregorian date | Hebrew date | Holiday | Categories |",
		"| ---- | ---- | ---- | ---- |",
	}
	for _, ev := range events {
		hd := ev.GetDate()
		var cats []string
		for _, c := range ev.GetCategories() {
			if c != "holiday" {
				cats = append(cats, c)
			}
		}
		out = append(out, fmt.Sprintf("| %s | %s | %s | %s |",
			isoGreg(hd), model.HDateString(hd), renderEn(ev), strings.Join(cats, ", ")))
	}
	return textResult(strings.Join(out, "\n")), nil, nil
}

// daf-yomi

type dafYomiArgs struct {
	Date string `json:"date" jsonschema:"Gregorian date in yyyy-MM-dd format"`
}

func (t *tools) dafYomi(_ context.Context, _ *mcpsdk.CallToolRequest, in dafYomiArgs) (*mcpsdk.CallToolResult, any, error) {
	hd, ok := parseISODate(in.Date)
	if !ok {
		return errorCard("Error parsing date: " + in.Date), nil, nil
	}
	daf, err := dafyomi.New(hd)
	if err != nil {
		return errorCard("Can't find Daf Yomi for date: " + in.Date), nil, nil
	}
	ev := dafyomi.NewDafYomiEvent(hd, daf)
	// hebcal-go's dafYomiEvent.Render is already the brief form ("Berakhot 2"),
	// with no "Daf Yomi:" prefix, so it matches the Node renderBrief.
	return lines(
		"Daf Yomi (English): "+ev.Render("en"),
		"Daf Yomi (Hebrew): "+ev.Render("he"),
		"Hebrew date: "+model.HDateString(hd),
		"Read the text of the Daf at: "+event.URL(ev),
	), nil, nil
}

// shabbat-times

type shabbatTimesArgs struct {
	Latitude  float64 `json:"latitude" jsonschema:"Latitude as decimal, valid range -90 to +90 (e.g. 41.85003)"`
	Longitude float64 `json:"longitude" jsonschema:"Longitude as decimal, valid range -180 to +180 (e.g. -87.65005)"`
	Tzid      string  `json:"tzid" jsonschema:"Olson timezone ID (e.g. \"America/Chicago\", \"Europe/Moscow\")"`
	StartDate string  `json:"startDate" jsonschema:"Start date in yyyy-MM-dd format"`
	EndDate   string  `json:"endDate" jsonschema:"End date in yyyy-MM-dd format"`
}

func (t *tools) shabbatTimes(_ context.Context, _ *mcpsdk.CallToolRequest, in shabbatTimesArgs) (*mcpsdk.CallToolResult, any, error) {
	// Validate before zmanim.NewLocation, which panics out of range.
	if in.Latitude < -90 || in.Latitude > 90 || in.Longitude < -180 || in.Longitude > 180 {
		return errorCard(fmt.Sprintf("Latitude or longitude out of range: %v, %v", in.Latitude, in.Longitude)), nil, nil
	}
	start, ok1 := parseISODate(in.StartDate)
	end, ok2 := parseISODate(in.EndDate)
	if !ok1 || !ok2 {
		return errorCard("Error parsing dates: " + in.StartDate + " or " + in.EndDate), nil, nil
	}
	il := in.Tzid == "Asia/Jerusalem"
	loc := zmanim.NewLocation("", "", in.Latitude, in.Longitude, 0, in.Tzid)
	events, err := hebcal.HebrewCalendar(&hebcal.CalOptions{
		CandleLighting: true,
		Location:       &loc,
		Start:          start,
		End:            end,
		Sedrot:         true,
		IL:             il,
	})
	if err != nil {
		return errorCard("Error: " + err.Error()), nil, nil
	}
	out := []string{
		"| Date | Time | Type | Associated Event |",
		"| ---- | ---- | ---- | ---- |",
	}
	for _, ev := range events {
		te, ok := ev.(hebcal.TimedEvent)
		if !ok || (te.Desc != "Candle lighting" && te.Desc != "Havdalah") {
			continue
		}
		associated := ""
		if te.LinkedEvent != nil {
			associated = te.LinkedEvent.Render("en")
		}
		out = append(out, fmt.Sprintf("| %s | %s | %s | %s |",
			isoGreg(te.GetDate()), te.EventTime.Format("15:04"), te.Desc, associated))
	}
	return textResult(strings.Join(out, "\n")), nil, nil
}
