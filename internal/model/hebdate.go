package model

import (
	"fmt"
	"time"

	"github.com/hebcal/gematriya"
	"github.com/hebcal/greg"
	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
)

// IsoDateStringToDate parses a YYYY-MM-DD string like the JS
// isoDateStringToDate: only the prefix is validated, and out-of-range
// month/day values roll over the way a JavaScript Date does.
func IsoDateStringToDate(s string) (GregDate, error) {
	if !ReIsoDate.MatchString(s) {
		return GregDate{}, BadRequest("Date does not match format YYYY-MM-DD: %s", s)
	}
	yy, _ := jsutil.ParseInt(s)
	mm, _ := jsutil.ParseInt(s[5:7])
	dd, _ := jsutil.ParseInt(s[8:10])
	// normalize out-of-range month/day the same way new Date(y, m, d) does
	t := time.Date(yy, time.Month(mm), dd, 12, 0, 0, 0, time.UTC)
	y2, m2, d2 := t.Date()
	return GregDate{Year: y2, Month: m2, Day: d2}, nil
}

// MakeGregDate validates a Gregorian yy/mm/dd from query-string values and
// returns the date. Ported from hebcal-web src/dateUtil.js makeGregDate().
func MakeGregDate(gy, gm, gd string) (GregDate, error) {
	yy, okY := jsutil.ParseInt(gy)
	mm, okM := jsutil.ParseInt(gm)
	dd, okD := jsutil.ParseInt(gd)
	if !okD {
		return GregDate{}, BadRequest("Gregorian day must be numeric: %s", gd)
	} else if !okM {
		return GregDate{}, BadRequest("Gregorian month must be numeric: %s", gm)
	} else if !okY {
		return GregDate{}, BadRequest("Gregorian year must be numeric: %s", gy)
	} else if mm > 12 || mm < 1 {
		return GregDate{}, BadRequest("Gregorian month out of valid range 1-12: %s", gm)
	} else if yy > 9999 {
		return GregDate{}, BadRequest("Gregorian year cannot be greater than 9999: %s", gy)
	}
	maxDay := greg.DaysIn(time.Month(mm), yy)
	if dd < 1 || dd > maxDay {
		return GregDate{}, BadRequest("Gregorian day %d out of valid range for %d/%d", dd, mm, yy)
	}
	dt := GregDate{Year: yy, Month: time.Month(mm), Day: dd}
	// Hebrew date 1 Tishrei 1 == Gregorian -003760-09-07. The JS epoch
	// comparison rejects 1 Tishrei 1 itself, so <= rather than <.
	if dt.RD() <= rdEpochHebrew {
		return GregDate{}, BadRequest("Gregorian date before Hebrew year 1: %s", dt.String())
	}
	return dt, nil
}

// MakeHebDate validates a Hebrew yy/mm/dd from query-string values.
// Ported from hebcal-web src/dateUtil.js makeHebDate().
func MakeHebDate(hyStr, hmStr, hdStr string) (hdate.HDate, error) {
	hy, okY := jsutil.ParseInt(hyStr)
	hd, okD := jsutil.ParseInt(hdStr)
	if !okD {
		return hdate.HDate{}, BadRequest("Hebrew day must be numeric: %s", hdStr)
	} else if !okY {
		return hdate.HDate{}, BadRequest("Hebrew year must be numeric: %s", hyStr)
	} else if hy < 1 {
		return hdate.HDate{}, BadRequest("Hebrew year must be year 1 or later: %d", hy)
	} else if hy > 32000 {
		return hdate.HDate{}, BadRequest("Hebrew year is too large: %d", hy)
	}
	if hmStr == "" {
		return hdate.HDate{}, BadRequest("Hebrew month is required")
	}
	hm, err := hdate.MonthFromName(hmStr)
	if err != nil {
		return hdate.HDate{}, BadRequest("bad monthName: %s", hmStr)
	}
	if hm == hdate.Adar2 && !hdate.IsLeapYear(hy) {
		hm = hdate.Adar1
	}
	maxDay := hdate.DaysInMonth(hm, hy)
	if hd < 1 || hd > maxDay {
		monthName := MonthNameEn(hm, hy)
		return hdate.HDate{}, BadRequest("Hebrew day %d out of valid range 1-%d for %s %d", hd, maxDay, monthName, hy)
	}
	return hdate.New(hy, hm, hd), nil
}

// HDateFromRD is a convenience wrapper.
func HDateFromRD(rd int64) hdate.HDate {
	return hdate.FromRD(rd)
}

// NewHDateLenient behaves like the JavaScript `new HDate(day, month, year)`,
// which rolls an out-of-range day over into the following month
// (e.g. 30 Cheshvan in a year when Cheshvan has 29 days becomes 1 Kislev).
func NewHDateLenient(year int, month hdate.HMonth, day int) hdate.HDate {
	if month == hdate.Adar2 && !hdate.IsLeapYear(year) {
		month = hdate.Adar1
	}
	dim := hdate.DaysInMonth(month, year)
	if day > dim {
		return hdate.FromRD(hdate.ToRD(year, month, dim) + int64(day-dim))
	}
	return hdate.New(year, month, day)
}

// SimchatTorahDate returns the date of Simchat Torah for the given Hebrew
// year (22 Tishrei in Israel, 23 Tishrei in the Diaspora).
func SimchatTorahDate(year int, il bool) hdate.HDate {
	mday := 23
	if il {
		mday = 22
	}
	return hdate.New(year, hdate.Tishrei, mday)
}

// IsoGreg formats a Hebrew date's Gregorian date as YYYY-MM-DD.
func IsoGreg(hd hdate.HDate) string {
	y, m, d := hd.Greg()
	return jsutil.IsoDateString(y, m, d)
}

// enMonthNames are the transliterated month names used by @hebcal/hdate
// (JavaScript). Note "Tamuz" with a single m; the Go hdate package spells it
// "Tammuz", but this service matches the JS API output ("hm":"Tamuz").
var enMonthNames = []string{
	"", "Nisan", "Iyyar", "Sivan", "Tamuz", "Av", "Elul",
	"Tishrei", "Cheshvan", "Kislev", "Tevet", "Sh'vat", "Adar", "Adar II",
}

// MonthNameEn returns the JS-compatible English month name.
func MonthNameEn(m hdate.HMonth, year int) string {
	if m == hdate.Adar1 && hdate.IsLeapYear(year) {
		return "Adar I"
	}
	return enMonthNames[m]
}

// HDMonthNameEn returns the JS-compatible English name of a date's month.
func HDMonthNameEn(hd hdate.HDate) string {
	return MonthNameEn(hd.Month(), hd.Year())
}

// HDateString formats like the JS HDate.toString(), e.g. "20 Tamuz 5786".
func HDateString(hd hdate.HDate) string {
	return fmt.Sprintf("%d %s %d", hd.Day(), HDMonthNameEn(hd), hd.Year())
}

// HeDateParts is the Hebrew-lettered breakdown of a Hebrew date.
type HeDateParts struct {
	Y string `json:"y"`
	M string `json:"m"`
	D string `json:"d"`
}

// MakeHeDateParts builds the heDateParts member of a JSON response.
func MakeHeDateParts(hd hdate.HDate) HeDateParts {
	return HeDateParts{
		Y: gematriya.Gematriya(hd.Year()),
		M: hd.MonthName("he-x-NoNikud"),
		D: gematriya.Gematriya(hd.Day()),
	}
}

// gematriyaMonthNames are Hebrew month names with the ב prefix, indexed by
// hdate.HMonth (Nisan=1 .. Adar2=13). Ported from hebcal-web
// src/gematriyaDate.js.
var gematriyaMonthNames = []string{
	"",
	"בְּנִיסָן",
	"בְּאִיָיר",
	"בְּסִיוָן",
	"בְּתַמּוּז",
	"בְּאָב",
	"בֶּאֱלוּל",
	"בְּתִשְׁרֵי",
	"בְּחֶשְׁוָן",
	"בְּכִסְלֵו",
	"בְּטֵבֵת",
	"בִּשְׁבָט",
	"בַּאֲדָר",
	"בַּאֲדָר ב׳",
}

const gematriyaAdarI = "בַּאֲדָר א׳"

// GematriyaDate renders a Hebrew date in Hebrew letters with nikud,
// e.g. "כ׳ בְּתַמּוּז תשפ״ו".
func GematriyaDate(hd hdate.HDate) string {
	mm := hd.Month()
	var monthName string
	if mm == hdate.Adar1 && hdate.IsLeapYear(hd.Year()) {
		monthName = gematriyaAdarI
	} else {
		monthName = gematriyaMonthNames[mm]
	}
	return gematriya.Gematriya(hd.Day()) + " " + monthName + " " + gematriya.Gematriya(hd.Year())
}
