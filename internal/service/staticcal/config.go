package staticcal

import (
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
)

// Feed describes one of the static calendars: what it holds, how far ahead it
// reaches, and how it names itself to a calendar application.
type Feed struct {
	// File is the name of the .ics and .csv files, without extension, and
	// the campaign its links are tagged with ("ical-" + File).
	File string
	// Title, Desc, Color and RelCalID are the calendar's X-WR-CALNAME,
	// X-WR-CALDESC, X-APPLE-CALENDAR-COLOR and X-WR-RELCALID. A RelCalID
	// must never change once published: it identifies the subscribed
	// calendar.
	Title    string
	Desc     string
	Color    string
	RelCalID string
	// Locale renders the events; IL selects the Israel schedule.
	Locale string
	IL     bool
	// Emoji puts each holiday's emoji in its title.
	Emoji bool
	// Years is how far ahead the feed reaches (see Window).
	Years int

	// Opts chooses the events hebcal-go generates, for a feed that is not a
	// readings-svc series. Start, End, IL and Locale are filled in.
	Opts hebcal.Options
	// NoMinorHolidays drops the minor holidays (Tu BiShvat, Lag BaOmer...)
	// from a holiday feed, keeping Purim and Chanukah.
	NoMinorHolidays bool
	// Series names a daily-learning series hebcal-go has no schedule for,
	// whose readings come from readings-svc instead.
	Series string
	// ChofetzChaim makes the one feed whose every day pairs a Chofetz Chaim
	// reading with a Shemirat HaLashon one.
	ChofetzChaim bool
}

const holidayColor = "#800002"

// The minor holidays, fasts, modern holidays, special Shabbatot and Rosh
// Chodesh, which the "major holidays" feeds leave out.
var majorOnly = hebcal.Options{
	NoRoshChodesh:    true,
	NoModern:         true,
	NoMinorFast:      true,
	NoSpecialShabbat: true,
}

// Feeds lists every static calendar, in the order they are generated.
var Feeds = []Feed{
	{
		File: "jewish-holidays-v2", Title: "Jewish Holidays ✡️", Emoji: true,
		Desc:   "Major Jewish holidays for the Diaspora from Hebcal.com",
		Color:  holidayColor,
		Locale: "en", Years: 10, Opts: majorOnly, NoMinorHolidays: true,
	},
	{
		File: "jewish-holidays-all-v2", Title: "Jewish Holidays ✡️", Emoji: true,
		Desc:   "All Jewish holidays for the Diaspora from Hebcal.com",
		Color:  holidayColor,
		Locale: "en", Years: 8,
	},
	{
		File: "jewish-holidays", Title: "Jewish Holidays",
		Desc:   "Major Jewish holidays for the Diaspora from Hebcal.com",
		Color:  holidayColor,
		Locale: "en", Years: 10, Opts: majorOnly, NoMinorHolidays: true,
	},
	{
		File: "jewish-holidays-all", Title: "Jewish Holidays",
		Desc:   "All Jewish holidays for the Diaspora from Hebcal.com",
		Color:  holidayColor,
		Locale: "en", Years: 8,
	},
	{
		File: "hdate-en", Title: "Hebrew calendar dates (en)",
		Desc:   "Displays the Hebrew date every day of the week in English transliteration",
		Color:  "#AC8E68",
		Locale: "en", Years: 3,
		Opts: hebcal.Options{AddHebrewDates: true, NoHolidays: true},
	},
	{
		File: "hdate-he", Title: "תאריך עברי (עם ניקוד)",
		Desc:   "מציג את התאריך העברי בכל יום מימות השבוע, עם ניקוד",
		Color:  "#AC8E68",
		Locale: "he", Years: 3,
		Opts: hebcal.Options{AddHebrewDates: true, NoHolidays: true},
	},
	{
		File: "hdate-he-v2", Title: "תאריך עברי",
		Desc:   "מציג את התאריך העברי בכל יום מימות השבוע, ללא ניקוד",
		Color:  "#AC8E68",
		Locale: "he-x-NoNikud", Years: 3,
		Opts: hebcal.Options{AddHebrewDates: true, NoHolidays: true},
	},
	{
		File: "omer", Title: "Days of the Omer",
		Desc:   "7 weeks from the second night of Pesach to the day before Shavuot",
		Color:  "#FF9F0A",
		Locale: "en", Years: 3,
		Opts: hebcal.Options{Omer: true, NoHolidays: true},
	},
	{
		File: "torah-readings-diaspora", Title: "Torah Readings (Diaspora)",
		Desc:   "Parashat ha-Shavua - Weekly Torah Portion from Hebcal.com",
		Color:  "#257E4A",
		Locale: "en", Years: 5,
		Opts: hebcal.Options{Sedrot: true, NoHolidays: true},
	},
	{
		File: "torah-readings-israel", Title: "Torah Readings (Israel English)",
		Desc:   "Parashat ha-Shavua - Weekly Torah Portion from Hebcal.com",
		Color:  "#257E4A",
		Locale: "en", IL: true, Years: 5,
		Opts: hebcal.Options{Sedrot: true, NoHolidays: true},
	},
	{
		File: "torah-readings-israel-he", Title: "פרשת השבוע בישראל",
		Desc:   "פרשת השבוע - קריאת התורה השבועית מאתר Hebcal.com",
		Color:  "#257E4A",
		Locale: "he", IL: true, Years: 5,
		Opts: hebcal.Options{Sedrot: true, NoHolidays: true},
	},
	{
		File: "yom-kippur-katan", Title: "Yom Kippur Katan",
		Desc:     "יוֹם כִּפּוּר קָטָן, minor day of atonement on the day preceeding each Rosh Chodesh",
		Color:    holidayColor,
		RelCalID: "457ce561-311f-4eeb-9033-65561b7f7503",
		Locale:   "en", Years: 10,
		Opts: hebcal.Options{YomKippurKatan: true, NoHolidays: true},
	},
	{
		File: "rosh-chodesh", Title: "Rosh Chodesh",
		Desc:   "Rosh Chodesh is a minor holiday observed at the beginning of every month in the Hebrew calendar",
		Color:  "#6F42C1",
		Locale: "en", Years: 10,
		Opts: hebcal.Options{Mask: event.RoshChodesh},
	},
	{
		File: "mevarchim", Title: "Shabbat Mevarchim",
		Desc:   "Shabbat announcing new month,Prior to Rosh Chodesh",
		Color:  holidayColor,
		Locale: "en", Years: 10,
		Opts: hebcal.Options{ShabbatMevarchim: true, NoHolidays: true},
	},
	{
		File: "yizkor-diaspora", Title: "Yizkor (Diaspora)",
		Desc:     "Ashkenazi Jewish memorial prayer service for the dead recited in synagogue during four holidays yearly",
		Color:    holidayColor,
		RelCalID: "64acac8c-a02f-4433-b7ef-f3a5e02d26cc",
		Locale:   "en", Years: 10,
		Opts: hebcal.Options{Yizkor: true, NoHolidays: true},
	},
	{
		File: "yizkor-il", Title: "יזכור (ישראל)",
		Desc:     "תפילת אזכרה אשכנזית לזכר הנפטרים, הנאמרת בבית הכנסת בארבעה מועדים בשנה",
		Color:    holidayColor,
		RelCalID: "21eadc10-9fa6-402c-ac6f-c94d1ee7537e",
		Locale:   "he-x-NoNikud", IL: true, Years: 10,
		Opts: hebcal.Options{Yizkor: true, NoHolidays: true},
	},

	// Daily learning, one series per feed, in the download form's order.
	learning("daf-yomi", "dafYomi", "Daf Yomi (Babylonian Talmud)",
		"Daily regimen of learning the Babylonian Talmud", "#BF5AF2", "", 5),
	learning("mishna-yomi", "mishnaYomi", "Mishna Yomi",
		"Two Mishnayot each day", "#003399", "", 5),
	learning("perek-yomi", "perekYomi", "Perek Yomi",
		"One chapter (perek) of Mishnah each day", "#003399",
		"839ede7d-4e1e-4e1c-acb9-7646d9146768", 4),
	learning("nach-yomi", "nachYomi", "Nach Yomi",
		"Nevi’im (Prophets) and Ketuvim (Writings)", "#003399", "", 4),
	learning("tanakh-yomi", "tanakhYomi", "Tanakh Yomi",
		"Prophets and Writings on weekdays according to the ancient Masoretic division of sedarim",
		holidayColor, "2b9fa0d4-8d96-4645-b22d-a5ca232c2276", 3),
	learning("psalms", "psalms", "Daily Tehillim (Psalms)",
		"Daily study of few chapters from the book of Psalms", holidayColor,
		"b41ffb3e-0950-48ac-bb29-076c77405361", 2),
	learning("929", "929", "929: Tanakh B'yachad",
		"Chapter-a-day Tanakh reading, five days a week", holidayColor,
		"27719c07-a038-41b3-aba0-4ed8c95acc7a", 4),
	learning("rambam1", "rambam1", "Daily Rambam (Mishneh Torah)",
		"Maimonides’ Mishneh Torah legal code", holidayColor,
		"13cb480b-a4a0-4667-8ec5-25819a2e37a1", 4),
	learning("rambam3", "rambam3", "Daily Rambam (3 Chapters)",
		"Maimonides’ Mishneh Torah legal code", holidayColor,
		"404804af-b0ff-4873-8265-ce8694e46171", 3),
	sidecar("sefer-hamitzvot", "seferHaMitzvot", "Sefer HaMitzvot",
		"Maimonides’ list of the 613 commandments", "",
		"edf00df1-a68e-4389-be93-3bfff03d7bce", 3),
	learning("yerushalmi-vilna", "yerushalmi-vilna", "Yerushalmi Yomi (Vilna)",
		"Jerusalem Talmud (Vilna edition)", "#BF5AF2", "", 5),
	learning("yerushalmi-schottenstein", "yerushalmi-schottenstein", "Yerushalmi Yomi (Schottenstein)",
		"Jerusalem Talmud (Schottenstein edition)", "#BF5AF2", "", 5),
	{
		File: "chofetz-chaim", Title: "Daily Chofetz Chaim",
		Desc:     "Daily study of the Sefer Chofetz Chaim and Shemirat HaLashon, which deal with the Jewish ethics and laws of speech",
		RelCalID: "a2a058a4-d2eb-45d8-9142-5976d4c85ebc",
		Locale:   "en", Years: 2, ChofetzChaim: true,
	},
	sidecar("dirshu-amud-hayomi", "dirshuAmudYomi", "Amud HaYomi (Dirshu)",
		"One amud (page side) of Babylonian Talmud per day", "#BF5AF2", "", 5),
	learning("daf-weekly", "dafWeeklySunday", "Daf-a-Week",
		"One page of Babylonian Talmud per week", "#003399", "", 10),
	learning("pirkei-avot", "pirkeiAvotSummer", "Pirkei Avot",
		"Ethics of our Fathers, studied on Shabbat between Pesach and Rosh Hashana",
		"#003399", "", 10),
	sidecar("ahs-yomi", "arukhHaShulchanYomi", "Arukh HaShulchan Yomi",
		"Summary of the sources for each chapter of the Shulchan Arukh and its commentaries",
		holidayColor, "b87406d8-e243-49f5-a072-934b06b10e5f", 3),
	sidecar("ksa-yomi", "kitzurShulchanAruch", "Kitzur Shulchan Arukh Yomi",
		"Simplified summary of the Shulchan Arukh, focusing on Orach Chayim and Yoreh De'ah", "",
		"0c021b70-c96d-4dc4-9dde-fcfbbf84357b", 3),
	sidecar("dirshu-daf-halacha", "dirshuDafHalacha", "Daf HaYomi B’Halacha (Dirshu)",
		"Dirshu’s daily Mishnah Berurah program", "", "", 2),
	sidecar("tanya-yomi", "tanyaYomi", "Tanya Yomi",
		"Daily study of the Tanya by Rabbi Schneur Zalman of Liadi", "",
		"d17b8a21-dc0e-4222-9556-625f64c1464b", 3),
}

// learning is a feed of one series hebcal-go generates, named as its
// dailylearning registry has it.
func learning(file, schedule, title, desc, color, relcalid string, years int) Feed {
	return Feed{
		File: file, Title: title, Desc: desc, Color: color, RelCalID: relcalid,
		Locale: "en", Years: years,
		Opts: hebcal.Options{NoHolidays: true, DailyLearning: []string{schedule}},
	}
}

// sidecar is a feed of one series readings-svc supplies.
func sidecar(file, series, title, desc, color, relcalid string, years int) Feed {
	f := learning(file, "", title, desc, color, relcalid, years)
	f.Opts = hebcal.Options{}
	f.Series = series
	return f
}
