package model

import "strings"

// Month and weekday names for every locale the PDF calendars can render.
//
// Generated from the dayjs locales that hebcal-web's src/dayjs-locales.js
// imports, so the strings match what the Node implementation prints. hebcal-web
// maps its short `lg` codes onto these through localeMap in src/lang.js;
// AliasLocale does the same, and CalendarNames below is keyed by the result.
//
// Regenerate with tools/dump-locales.mjs (see the repo README).

// CalendarNames holds the localized vocabulary for one language.
type CalendarNames struct {
	// Months are the full Gregorian month names used in a page title.
	Months [12]string
	// MonthsShort are the abbreviated names used in the Gregorian date range
	// beneath a Hebrew-month title.
	MonthsShort [12]string
	// Weekdays are the column headings, Sunday first.
	Weekdays [7]string
}

// namesByLocale is keyed by the resolved locale name, not by the short `lg`
// code. Locales absent here fall back to English, which is what hebcal-web
// does for its transliterated locales (`s`, `ashkenazi`): their event text is
// Latin script, so English month names are the right pairing.
var namesByLocale = map[string]CalendarNames{
	"en": {
		Months:      [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
		MonthsShort: [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		Weekdays:    [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	},
	"de": {
		Months:      [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
		MonthsShort: [12]string{"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez."},
		Weekdays:    [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
	},
	"es": {
		Months:      [12]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"},
		MonthsShort: [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
		Weekdays:    [7]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
	},
	"fi": {
		Months:      [12]string{"tammikuu", "helmikuu", "maaliskuu", "huhtikuu", "toukokuu", "kesäkuu", "heinäkuu", "elokuu", "syyskuu", "lokakuu", "marraskuu", "joulukuu"},
		MonthsShort: [12]string{"tammi", "helmi", "maalis", "huhti", "touko", "kesä", "heinä", "elo", "syys", "loka", "marras", "joulu"},
		Weekdays:    [7]string{"sunnuntai", "maanantai", "tiistai", "keskiviikko", "torstai", "perjantai", "lauantai"},
	},
	"fr": {
		Months:      [12]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"},
		MonthsShort: [12]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
		Weekdays:    [7]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
	},
	"he": {
		Months:      [12]string{"ינואר", "פברואר", "מרץ", "אפריל", "מאי", "יוני", "יולי", "אוגוסט", "ספטמבר", "אוקטובר", "נובמבר", "דצמבר"},
		MonthsShort: [12]string{"ינו", "פבר", "מרץ", "אפר", "מאי", "יונ", "יול", "אוג", "ספט", "אוק", "נוב", "דצמ"},
		Weekdays:    [7]string{"ראשון", "שני", "שלישי", "רביעי", "חמישי", "שישי", "שבת"},
	},
	"hu": {
		Months:      [12]string{"január", "február", "március", "április", "május", "június", "július", "augusztus", "szeptember", "október", "november", "december"},
		MonthsShort: [12]string{"jan", "feb", "márc", "ápr", "máj", "jún", "júl", "aug", "szept", "okt", "nov", "dec"},
		Weekdays:    [7]string{"vasárnap", "hétfő", "kedd", "szerda", "csütörtök", "péntek", "szombat"},
	},
	"nl": {
		Months:      [12]string{"januari", "februari", "maart", "april", "mei", "juni", "juli", "augustus", "september", "oktober", "november", "december"},
		MonthsShort: [12]string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"},
		Weekdays:    [7]string{"zondag", "maandag", "dinsdag", "woensdag", "donderdag", "vrijdag", "zaterdag"},
	},
	"pl": {
		Months:      [12]string{"styczeń", "luty", "marzec", "kwiecień", "maj", "czerwiec", "lipiec", "sierpień", "wrzesień", "październik", "listopad", "grudzień"},
		MonthsShort: [12]string{"sty", "lut", "mar", "kwi", "maj", "cze", "lip", "sie", "wrz", "paź", "lis", "gru"},
		Weekdays:    [7]string{"niedziela", "poniedziałek", "wtorek", "środa", "czwartek", "piątek", "sobota"},
	},
	"pt": {
		Months:      [12]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"},
		MonthsShort: [12]string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"},
		Weekdays:    [7]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"},
	},
	"ru": {
		Months:      [12]string{"январь", "февраль", "март", "апрель", "май", "июнь", "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"},
		MonthsShort: [12]string{"янв.", "февр.", "март", "апр.", "май", "июнь", "июль", "авг.", "сент.", "окт.", "нояб.", "дек."},
		Weekdays:    [7]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"},
	},
	"ro": {
		Months:      [12]string{"Ianuarie", "Februarie", "Martie", "Aprilie", "Mai", "Iunie", "Iulie", "August", "Septembrie", "Octombrie", "Noiembrie", "Decembrie"},
		MonthsShort: [12]string{"Ian.", "Febr.", "Mart.", "Apr.", "Mai", "Iun.", "Iul.", "Aug.", "Sept.", "Oct.", "Nov.", "Dec."},
		Weekdays:    [7]string{"Duminică", "Luni", "Marți", "Miercuri", "Joi", "Vineri", "Sâmbătă"},
	},
	"uk": {
		Months:      [12]string{"січень", "лютий", "березень", "квітень", "травень", "червень", "липень", "серпень", "вересень", "жовтень", "листопад", "грудень"},
		MonthsShort: [12]string{"січ", "лют", "бер", "квіт", "трав", "черв", "лип", "серп", "вер", "жовт", "лист", "груд"},
		Weekdays:    [7]string{"неділя", "понеділок", "вівторок", "середа", "четвер", "п’ятниця", "субота"},
	},
}

// NamesFor returns the calendar vocabulary for a resolved locale, falling back
// to English. The lookup is case-insensitive, and he-x-nonikud shares he's
// month and weekday names -- hebcal-web's localeMap resolves both to the same
// dayjs `he` locale, since the no-nikud variant only affects event subjects.
func NamesFor(locale string) CalendarNames {
	key := strings.ToLower(locale)
	if key == "he-x-nonikud" {
		key = "he"
	}
	if n, ok := namesByLocale[key]; ok {
		return n
	}
	return namesByLocale["en"]
}
