package ical

import (
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"

	"github.com/hebcal/hebcal-api-go/internal/model"
)

// kiddushLevanaMemo lists the four times that bound Kiddush Levana -- the
// earliest, three and seven days after the molad, and the latest, midway
// between molads and fifteen days after it -- one to a line, followed for the
// latest by the time the event was moved to when that time falls in daylight
// or on Shabbat. These are the unadjusted times; the event itself starts at the
// adjusted one. Earliest times round up to the minute and latest times down.
func kiddushLevanaMemo(ev event.CalEvent, o *Options) string {
	kl, ok := ev.(hebcal.KiddushLevanaEvent)
	if !ok || o.Location == nil {
		return ""
	}
	tz := kl.EventTime.Location()
	cc := o.Location.CountryCode
	clock := func(t time.Time) string {
		return event.ReformatTimeStr(t.Format("15:04"), "pm", cc, o.Hour12)
	}
	rawMaharil := kl.Molad.SofZmanKidushLevanaBetweenMoldos()
	maharil := rawMaharil.Truncate(time.Minute)
	rows := []struct {
		label string
		t     time.Time
	}{
		{"Earliest Kiddush Levana (3 days)", ceilMinute(kl.Molad.TchilasZmanKidushLevana3Days())},
		{"Earliest Kiddush Levana (7 days)", ceilMinute(kl.Molad.TchilasZmanKidushLevana7Days())},
		{"Latest Kiddush Levana (Maharil)", maharil},
		{"Latest Kiddush Levana (15 days)", kl.Molad.SofZmanKidushLevana15Days().Truncate(time.Minute)},
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		t := r.t.In(tz)
		line := model.Gettext(r.label, o.Locale) + ": " + formatDate(t, o.Locale) + ", " + clock(t)
		if i == 2 {
			if zman := adjustedTo(kl, rawMaharil, tz, o); zman != "" {
				line += " (" + model.Gettext("adjusted to", o.Locale) + " " + clock(kl.EventTime) + " " +
					model.Gettext(zman, o.Locale) + ")"
			}
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// adjustedTo names what the event's time was moved to from the Maharil's, as
// "Alot haShachar" or "Candle lighting", or "" when it was not moved. A time
// in the day is moved back to alot hashachar that morning, and one on Shabbat
// or a festival to candle lighting before it; the event's time is what
// tells the two apart.
func adjustedTo(kl hebcal.KiddushLevanaEvent, raw time.Time, tz *time.Location, o *Options) string {
	local := raw.In(tz)
	y, m, d := local.Date()
	z := zmanim.New(o.Location, time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
	z.UseElevation = o.UseElevation
	alos, tzeit := z.AlotHaShachar(), z.Tzeit(zmanim.Tzeit3SmallStars)
	if !alos.IsZero() && !tzeit.IsZero() && raw.After(alos) && raw.Before(tzeit) {
		if kl.EventTime.Equal(alos.Truncate(time.Minute)) {
			return "Alot haShachar"
		}
		return "Candle lighting"
	}
	if kl.EventTime.Equal(raw.Truncate(time.Minute)) {
		return ""
	}
	return "Candle lighting"
}

// ceilMinute rounds up to the next whole minute.
func ceilMinute(t time.Time) time.Time {
	floor := t.Truncate(time.Minute)
	if floor.Equal(t) {
		return t
	}
	return floor.Add(time.Minute)
}

// formatDate writes a date as a short weekday, day, month and year in the
// calendar's language: "Mon, Dec 22, 2025" in English, "lun, 22 dic 2025" in
// Spanish. A language with no entry of its own is written in English, as the
// transliterated locales are.
func formatDate(t time.Time, locale string) string {
	key := strings.ToLower(locale)
	if key == "he-x-nonikud" {
		key = "he"
	}
	f, ok := klDateFormats[key]
	if !ok {
		return t.Format("Mon, Jan 2, 2006")
	}
	return strings.NewReplacer(
		"{w}", f.Weekdays[t.Weekday()],
		"{d}", strconv.Itoa(t.Day()),
		"{m}", f.Months[t.Month()-1],
		"{y}", strconv.Itoa(t.Year()),
	).Replace(f.Pattern)
}
