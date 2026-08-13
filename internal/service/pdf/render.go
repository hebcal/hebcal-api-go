package pdf

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/gematriya"
	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/locales"
	// Aliased because this package is itself named pdf; pdflib is seehuhn's
	// PDF writer, not anything in here.
	pdflib "seehuhn.de/go/pdf"
	"seehuhn.de/go/pdf/action"
	"seehuhn.de/go/pdf/annotation"
	"seehuhn.de/go/pdf/document"
	pdffont "seehuhn.de/go/pdf/font"
	"seehuhn.de/go/pdf/graphics/color"

	"github.com/hebcal/hebcal-api-go/internal/config"
	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
)

// Page geometry, copied from hebcal-web's src/pdf.js. US Letter, landscape.
//
// All layout in this file is expressed in pdfkit's coordinate system, which
// runs top-down from the top-left corner, so the constants and arithmetic can
// be read side by side with src/pdf.js. Conversion to PDF's bottom-up
// coordinates happens in exactly two places, baseline() and yLine().
const (
	pdfWidth      = 792.0
	pdfHeight     = 612.0
	pdfBMargin    = 72.0
	pdfTMargin    = 32.0
	pdfLMargin    = 24.0
	pdfRMargin    = 24.0
	pdfColumns    = 7
	pdfColWidth   = (pdfWidth - pdfLMargin - pdfRMargin) / pdfColumns
	pdfCellMargin = 2.0
	timeFontSize  = 8.5
)

// Colours from src/pdf.js.
var (
	colorLearning = rgb("#666666")
	colorRoshChod = rgb("#660099")
	colorMinorFst = rgb("#FF3300")
	colorParsha   = rgb("#009900")
	colorMinorHol = rgb("#006699")
	colorChag     = rgb("#990000")
	colorBlack    = rgb("#000000")
	colorGrid     = rgb("#cccccc")
	colorGray     = rgb("#999999")
	colorDow      = rgb("#0000cc")
)

// rgb parses a #rrggbb literal into a PDF colour.
func rgb(hex string) color.Color {
	var r, g, b int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return color.SRGB(float64(r)/255, float64(g)/255, float64(b)/255)
}

// eventColor is the port of eventColor() in src/pdf.js. The order of the tests
// matters: an event can carry several flags and the first match wins.
func eventColor(f event.HolidayFlags) color.Color {
	switch {
	case f&(event.DAF_YOMI|event.OMER_COUNT|event.HEBREW_DATE|
		event.MISHNA_YOMI|event.YERUSHALMI_YOMI|event.NACH_YOMI) != 0:
		return colorLearning
	case f&event.ROSH_CHODESH != 0:
		return colorRoshChod
	case f&event.MINOR_FAST != 0:
		return colorMinorFst
	case f&event.PARSHA_HASHAVUA != 0:
		return colorParsha
	case f&(event.SPECIAL_SHABBAT|event.MODERN_HOLIDAY|event.MINOR_HOLIDAY) != 0:
		return colorMinorHol
	case f&(event.CHAG|event.EREV|event.CHOL_HAMOED|event.MAJOR_FAST) != 0:
		return colorChag
	}
	return colorBlack
}

// Renderer draws calendars.
type Renderer struct {
	fonts  *FontSet
	shaper *Shaper
}

// NewRenderer returns a Renderer using the given fonts.
func NewRenderer(fonts *FontSet) *Renderer {
	return &Renderer{fonts: fonts, shaper: NewShaper(fonts.faces)}
}

// Render writes a complete PDF calendar to w.
func (r *Renderer) Render(w io.Writer, p *Params, events []Event, title string) error {
	if r == nil || r.fonts == nil {
		return fmt.Errorf("no fonts loaded")
	}
	inst, err := r.fonts.Embed()
	if err != nil {
		return err
	}
	doc, err := document.WriteMultiPage(w, &pdflib.Rectangle{URx: pdfWidth, URy: pdfHeight}, pdfVersion, nil)
	if err != nil {
		return err
	}
	doc.Out.GetMeta().Info = &pdflib.Info{
		Title: pdflib.TextString(title),
		// hebcal-web sets Subject to the title as well, and macOS shows it as
		// the file's Description.
		Subject:  pdflib.TextString(title),
		Keywords: pdflib.TextString(documentKeywords(p)),
		Author:   pdflib.TextString("Hebcal Jewish Calendar (hebcal.com)"),
		Producer: pdflib.TextString(config.PDFProducer),
	}

	if p.MonthMode != GregorianArabic {
		hpages := SplitByHebrewMonth(events)
		if len(hpages) == 0 {
			return fmt.Errorf("no events to render")
		}
		campaign := campaignFromTitle(title)
		for _, hp := range hpages {
			if err := r.renderHebMonth(doc, inst, p, hp, campaign); err != nil {
				return err
			}
		}
		return doc.Close()
	}

	pages := SplitByGregorianMonth(events)
	if len(pages) == 0 {
		return fmt.Errorf("no events to render")
	}
	campaign := campaignFromTitle(title)
	for _, mp := range pages {
		if err := r.renderMonth(doc, inst, p, mp, campaign); err != nil {
			return err
		}
	}
	return doc.Close()
}

// baseline converts a pdfkit top-down text position into the PDF baseline.
// pdfkit's doc.text(str, x, y) treats y as the top of the text box and puts
// the baseline one ascender below it.
func (r *Renderer) baseline(fontName string, size, yTopDown float64) float64 {
	return pdfHeight - yTopDown - r.fonts.Ascent(fontName, size)
}

// yLine converts a top-down y used for rules and boxes, where no font is
// involved.
func yLine(yTopDown float64) float64 { return pdfHeight - yTopDown }

// draw writes one shaped string. x and yTopDown are pdfkit coordinates.
func (r *Renderer) draw(page *document.Page, inst *Instances, fontName string, size float64, col color.Color, x, yTopDown float64, s string) {
	f := inst.Get(fontName)
	if f == nil || s == "" {
		return
	}
	page.TextBegin()
	page.TextSetFont(f, size)
	page.SetFillColor(col)
	page.TextFirstLine(x, r.baseline(fontName, size, yTopDown))
	for _, run := range r.shaper.Shape(fontName, size, s) {
		page.TextShowGlyphs(&pdffont.GlyphSeq{Skip: run.Skip, Seq: run.Glyphs})
	}
	page.TextEnd()
}

// width is the equivalent of pdfkit's doc.widthOfString().
func (r *Renderer) width(fontName string, size float64, s string) float64 {
	return r.shaper.Width(fontName, size, s)
}

// drawCentered centres s horizontally on the page.
func (r *Renderer) drawCentered(page *document.Page, inst *Instances, fontName string, size float64, col color.Color, yTopDown float64, s string) {
	w := r.width(fontName, size, s)
	r.draw(page, inst, fontName, size, col, (pdfWidth-w)/2, yTopDown, s)
}

// drawRightAligned draws s so that it ends at x.
func (r *Renderer) drawRightAligned(page *document.Page, inst *Instances, fontName string, size float64, col color.Color, x, yTopDown float64, s string) {
	r.draw(page, inst, fontName, size, col, x-r.width(fontName, size, s), yTopDown, s)
}

// rowsFor returns 5 or 6 week rows. This is the rule from src/pdf.js rather
// than a computed ceiling: the grid stays five rows unless the month genuinely
// cannot fit, which keeps cell heights consistent from page to page.
func rowsFor(daysInMonth, startDayOfWeek int) int {
	if (daysInMonth == 31 && startDayOfWeek >= 5) || (daysInMonth == 30 && startDayOfWeek == 6) {
		return 6
	}
	return 5
}

// renderMonth draws one page.
func (r *Renderer) renderMonth(doc *document.MultiPage, inst *Instances, p *Params, mp MonthPage, campaign string) error {
	page := doc.AddPage()

	first := time.Date(mp.Year, mp.Month, 1, 0, 0, 0, 0, time.UTC)
	daysInMonth := first.AddDate(0, 1, -1).Day()
	startDow := int(first.Weekday())
	rows := rowsFor(daysInMonth, startDow)
	rowHeight := (pdfHeight - pdfTMargin - pdfBMargin) / float64(rows)

	r.drawMonthTitle(page, inst, p, mp)
	r.drawGrid(page, inst, p, rows, rowHeight)
	r.drawDays(page, inst, p, mp, daysInMonth, startDow, rowHeight, campaign)
	r.drawFooter(page, inst, p)

	return page.Close()
}

// drawMonthTitle writes the centred month title and the Hebrew-month subtitle,
// matching renderPdfMonthTitle(): 26pt at TMARGIN-24 and 14pt at TMARGIN+4.
func (r *Renderer) drawMonthTitle(page *document.Page, inst *Instances, p *Params, mp MonthPage) {
	titleFont, subFont := FontSemi, FontPlain
	if p.RTL {
		titleFont, subFont = FontHebrew, FontHebrew
	}
	names := model.NamesFor(p.Locale)
	title := names.Months[int(mp.Month)-1] + " " + strconv.Itoa(mp.Year)
	r.drawCentered(page, inst, titleFont, 26, colorBlack, pdfTMargin-24, title)
	r.drawCentered(page, inst, subFont, 14, colorBlack, pdfTMargin+4, hebMonthRange(mp, p))
}

// drawGrid draws the calendar rectangle, its rules and the weekday headings.
// Ported from renderPdfMonthGrid(): the rectangle runs from BMARGIN down to
// HEIGHT-TMARGIN in pdfkit's coordinates.
func (r *Renderer) drawGrid(page *document.Page, inst *Instances, p *Params, rows int, rowHeight float64) {
	gridW := pdfWidth - pdfLMargin - pdfRMargin
	top, bottom := pdfBMargin, pdfHeight-pdfTMargin

	page.SetLineWidth(1)
	page.SetStrokeColor(colorGrid)
	page.Rectangle(pdfLMargin, yLine(bottom), gridW, bottom-top)
	page.Stroke()

	for i := 1; i < pdfColumns; i++ {
		x := pdfLMargin + float64(i)*pdfColWidth
		page.MoveTo(x, yLine(bottom))
		page.LineTo(x, yLine(top))
		page.Stroke()
	}
	for i := 1; i < rows; i++ {
		y := yLine(bottom - float64(i)*rowHeight)
		page.MoveTo(pdfLMargin, y)
		page.LineTo(pdfWidth-pdfRMargin, y)
		page.Stroke()
	}

	dowFont := FontPlain
	if p.RTL {
		dowFont = FontHebrew
	}
	for i, name := range model.NamesFor(p.Locale).Weekdays {
		edge := float64(i)*pdfColWidth + pdfColWidth/2
		cx := pdfLMargin + edge
		if p.RTL {
			cx = pdfWidth - pdfRMargin - edge
		}
		w := r.width(dowFont, 10, name)
		r.draw(page, inst, dowFont, 10, colorDow, cx-w/2, pdfTMargin+24, name)
	}
}

// drawDays fills each cell with its right-aligned day number and its events.
func (r *Renderer) drawDays(page *document.Page, inst *Instances, p *Params, mp MonthPage, daysInMonth, startDow int, rowHeight float64, campaign string) {
	// xpos is the right edge of the current cell less a 4pt inset, which is
	// what the day number is right-aligned against; xposNewRow is that position
	// for the first column of a row. RTL walks the columns in reverse.
	xposNewRow := pdfLMargin + pdfColWidth - 4
	mult := 1.0
	if p.RTL {
		xposNewRow = pdfWidth - pdfRMargin - 4
		mult = -1
	}
	dow := startDow
	xpos := xposNewRow + float64(dow)*pdfColWidth*mult
	ypos := pdfTMargin + 40.0

	// eventX is the left edge of the cell in column dow, plus the cell margin.
	// It is derived from the column rather than from xpos, which carries a 4pt
	// inset that applies only to the right-aligned day number.
	eventX := func(dow int) float64 {
		return cellOrigin(p.RTL, dow)
	}

	// Day numbers stay in the Latin semibold face even in a Hebrew locale:
	// they are Arabic numerals, and the Hebrew face is only needed when mm=2
	// asks for gematriya.
	numFont := FontSemi

	for mday := 1; mday <= daysInMonth; mday++ {
		r.drawRightAligned(page, inst, numFont, 14, colorBlack, xpos, ypos, strconv.Itoa(mday))

		evs := mp.Days[mday]
		// The alternate (Hebrew) date shares the day-number line and is not
		// redrawn as an event row below.
		r.renderAltDateOnLine(page, inst, p, firstAltDate(evs), xpos, ypos, nil)

		y := ypos + 22
		for i := range evs {
			if evs[i].AltDate {
				continue
			}
			if y+10 > ypos+rowHeight {
				break // would overrun the cell
			}
			y = r.renderEvent(page, inst, p, &evs[i], eventX(dow), y, campaign)
		}

		if dow++; dow == 7 {
			dow = 0
			xpos = xposNewRow
			ypos += rowHeight
		} else {
			xpos += pdfColWidth * mult
		}
	}
}

// renderEvent draws one event line and returns the next top-down y, matching
// renderPdfEvent()'s `y + numLines * fontSize * 1.4`.
func (r *Renderer) renderEvent(page *document.Page, inst *Instances, p *Params, ev *Event, x, y float64, campaign string) float64 {
	return r.renderEventColored(page, inst, p, ev, x, y, campaign, nil)
}

// renderEventColored draws one event line, optionally forcing a colour. The
// Elul days folded onto a Tishrei page are drawn entirely in grey, so they
// read as belonging to the previous month.
func (r *Renderer) renderEventColored(page *document.Page, inst *Instances, p *Params, ev *Event, x, y float64, campaign string, override color.Color) float64 {
	rtl := p.RTL
	col := eventColor(ev.Flags)
	if ev.Learning {
		col = colorLearning
	}
	if override != nil {
		col = override
	}
	isChag := ev.Flags&event.CHAG != 0 && !ev.Timed()

	var timedWidth float64
	if ev.Timed() {
		timedWidth = r.width(FontBold, timeFontSize, ev.TimeStr+" ")
	}

	fontName := FontPlain
	switch {
	case rtl && isChag:
		fontName = FontHebrewBold
	case rtl:
		fontName = FontHebrew
	case isChag:
		fontName = FontBold
	}
	fontSize := 10.0
	if rtl {
		fontSize = 12.0
	}

	subject := ev.Subject
	width := r.width(fontName, fontSize, subject)
	available := pdfColWidth - 2*pdfCellMargin
	for i := 0; i < 4 && timedWidth+width > available; i++ {
		fontSize -= 0.5
		width = r.width(fontName, fontSize, subject)
	}
	lines := []string{subject}
	numLines := 1
	if timedWidth+width > available {
		lines = splitInTwo(subject, rtl)
		// renderPdfEvent counts the wrap whether or not the split found a place
		// to break, so the row advances by two lines either way.
		numLines = 2
	}

	// Alignment within the cell, following renderPdfEvent(). Right-to-left
	// calendars right-align: a timed event places the time and its subject as
	// one unit against the cell's right edge, with the time on the left, which
	// is where it reads first.
	textX := x + 2*pdfCellMargin
	switch {
	case rtl && ev.Timed():
		startX := x + available - (timedWidth + width)
		r.draw(page, inst, FontBold, timeFontSize, col, startX, y+1, ev.TimeStr+" ")
		textX = startX + timedWidth
	case rtl:
		textX = x + available - width
	case ev.Timed():
		r.draw(page, inst, FontBold, timeFontSize, col, textX, y+1, ev.TimeStr+" ")
		textX += timedWidth
	}
	// Hebrew sits fractionally lower on the line than the Latin faces do.
	textY := y
	if rtl {
		textY += 0.65
	}
	for i, ln := range lines {
		r.draw(page, inst, fontName, fontSize, col, textX, textY+float64(i)*fontSize*1.4, ln)
	}
	// A link over the whole event line, matching the textOptions.link that
	// hebcal-web hands to pdfkit.
	if href := eventLink(ev.URL, ev.HD.Year(), p.campaignFor(campaign, ev), p.Opts.IL); href != "" {
		h := float64(numLines) * fontSize * 1.4
		r.addLink(page, href, textX-timedWidth, y-fontSize*0.25, timedWidth+width, h)
	}
	if p.AppendHebrew && ev.HebrewBrief != "" {
		return r.appendHebrew(page, inst, ev, col, textX, y, width, timedWidth, numLines, fontName, fontSize)
	}
	return y + float64(numLines)*fontSize*1.4
}

// appendHebrew draws the Hebrew name after a transliterated subject, the
// appendHebrewToSubject branch of renderPdfEvent(). If the transliteration, a
// " / " separator and the Hebrew fit on the line they are drawn as one unit,
// with the Hebrew sitting 1.35pt lower; otherwise the Hebrew drops to the next
// line at the subject's left edge with no separator. It returns the next
// top-down y.
func (r *Renderer) appendHebrew(page *document.Page, inst *Instances, ev *Event, col color.Color, textX, y, width, timedWidth float64, numLines int, subjFont string, subjSize float64) float64 {
	const (
		slash  = " / "
		heSize = 11.0
	)
	available := pdfColWidth - 2*pdfCellMargin
	widthSlash := r.width(subjFont, subjSize, slash)
	hebrewWidth := r.width(FontHebrew, heSize, ev.HebrewBrief)
	if timedWidth+width+widthSlash+hebrewWidth > available {
		yy := y + float64(numLines)*subjSize*1.35
		r.draw(page, inst, FontHebrew, heSize, col, textX, yy, ev.HebrewBrief)
		return yy + subjSize*1.4
	}
	slashX := textX + width
	r.draw(page, inst, subjFont, subjSize, col, slashX, y, slash)
	r.draw(page, inst, FontHebrew, heSize, col, slashX+widthSlash, y+1.35, ev.HebrewBrief)
	return y + float64(numLines)*subjSize*1.4 + 1.35
}

// splitInTwo breaks a subject across two lines, the two-line fallback in
// renderPdfEvent(). It returns one line when no break point is found, which the
// caller still spaces as two (see below).
//
// renderPdfEvent splits on `/(\s)/`, keeping the separators, and looks for the
// break at the element just past the midpoint of that array -- which lands on a
// space for some word counts and on a word for others, in which case it takes
// the space after it. For an even number of words that leaves one more word on
// the first line than halving would: production draws "Yom HaAliyah School" /
// "Observance", not "Yom HaAliyah" / "School Observance".
//
// A right-to-left subject arrives at that split already rejoined by
// reverseHebrewWords() with *two* spaces between words, so the array has an
// empty element between every pair, the midpoint lands half a word earlier, and
// the break is a plain halving.
//
// With fewer than three words there is no space past the midpoint of the
// left-to-right array, so no break is inserted at all -- and yet renderPdfEvent
// increments its line count either way, spacing the row and sizing its link box
// as if it had wrapped. That is why the caller counts two lines regardless of
// how many this returns.
func splitInTwo(s string, rtl bool) []string {
	words := strings.Split(s, " ")
	n := len(words)
	var mid int
	switch {
	case rtl && n >= 2:
		mid = (n + 1) / 2
	case !rtl && n >= 3:
		mid = n/2 + 1
	default:
		return []string{s}
	}
	return []string{
		strings.Join(words[:mid], " "),
		"  " + strings.Join(words[mid:], " "),
	}
}

// altDateBrief renders a HEBREW_DATE alternate date without its year, matching
// HebrewDateEvent.renderBrief() in @hebcal/core. hebcal-go's hebrewDateEvent
// only renders the full form (with year), so the brief is that form with the
// trailing year trimmed. Rosh Hashana (1 Tishrei) keeps its year, as it does in
// @hebcal/core.
//
// The apostrophe is smartened here as it is on an event subject: @hebcal/hdate
// ends HDate.render() with `monthName.replace(/'/g, '’')` and hebcal-go does
// not, so without this a day line reads "1st of Sh'vat" where hebcal-web writes
// "1st of Sh’vat". The ordinal differs the same way for the locales that are
// neither English nor Spanish ("12 Tewet" here, "12. Tewet" there); that one
// belongs in hebcal-go rather than in a second date formatter over here.
func altDateBrief(hd hdate.HDate, locale string) string {
	full := jsutil.SmartApostrophe(model.FixMonthSpelling(event.NewHebrewDateEvent(hd).Render(locale)))
	if hd.Month() == hdate.Tishrei && hd.Day() == 1 {
		return full
	}
	switch strings.ToLower(locale) {
	case "he", "he-x-nonikud":
		return strings.TrimSuffix(full, " "+gematriya.Gematriya(hd.Year()))
	case "", "en", "sephardic", "ashkenazi",
		"ashkenazi_litvish", "ashkenazi_poylish", "ashkenazi_standard":
		return strings.TrimSuffix(full, ", "+strconv.Itoa(hd.Year()))
	default:
		return strings.TrimSuffix(full, " "+strconv.Itoa(hd.Year()))
	}
}

// firstAltDate returns the day's alternate-date event, or nil. There is at most
// one per day.
func firstAltDate(evs []Event) *Event {
	for i := range evs {
		if evs[i].AltDate {
			return &evs[i]
		}
	}
	return nil
}

// renderAltDateOnLine draws the brief alternate date on the day-number line,
// left-aligned near the cell's left edge. Port of renderAlternateDateOnLine():
// xpos is the day number's right edge, the date sits 3pt below the number's top,
// in the plain face (or the Hebrew face in an RTL calendar) at 10pt and in grey.
func (r *Renderer) renderAltDateOnLine(page *document.Page, inst *Instances, p *Params, alt *Event, xpos, ypos float64, override color.Color) {
	if alt == nil {
		return
	}
	font := FontPlain
	marginFactor := 3.0
	if p.RTL {
		font = FontHebrew
		marginFactor = 2.0
	}
	col := colorLearning // #666666
	if override != nil {
		col = override
	}
	// Gregorian-month calendars show the Hebrew date; Hebrew-month calendars show
	// the Gregorian date. Both go through the same left-aligned placement.
	text := altDateBrief(alt.HD, p.Locale)
	if p.MonthMode != GregorianArabic {
		text = gregorianAltText(alt.HD, p.Locale)
	}
	if p.RTL {
		// A Hebrew-month Gregorian alt date ("25 אוק") starts with a number, and
		// x/text/bidi resolves a number-first string to an LTR paragraph, which
		// would draw the month to the right of the digits. hebcal-web forces the
		// order with reverseHebrewWords; here a leading right-to-left mark makes
		// the paragraph RTL so the Unicode algorithm orders it the same way. The
		// mark is zero-width and default-ignorable, so it adds no visible glyph.
		text = "‏" + text
	}
	altX := xpos - pdfColWidth + pdfCellMargin*marginFactor
	r.draw(page, inst, font, 10, col, altX, ypos+3, text)
}

// ccLine is the attribution shown at the bottom right of every page.
const ccLine = "Provided by Hebcal.com with a Creative Commons Attribution 4.0 International License"

// drawFooter writes the location/settings line and the licence line.
func (r *Renderer) drawFooter(page *document.Page, inst *Instances, p *Params) {
	const y = pdfHeight - 28
	r.draw(page, inst, FontPlain, 8, colorBlack, pdfLMargin, y, leftFooterText(p))
	r.drawRightAligned(page, inst, FontPlain, 8, colorBlack, pdfWidth-pdfRMargin, y, ccLine)
}

// leftFooterText is the port of makeLeftText(): the location and its
// candle-lighting offset when there is one, otherwise the calendar subtitle.
func leftFooterText(p *Params) string {
	loc := p.Opts.Location
	if loc == nil || loc.Name == "" {
		if p.Opts.IL {
			return "Israel holiday schedule"
		}
		return "Diaspora holiday schedule"
	}
	str := loc.Name
	if p.Opts.UseElevation && loc.Elevation > 0 {
		str += fmt.Sprintf(" (elevation: %d m)", loc.Elevation)
	}
	mins := p.Opts.CandleLightingMins
	if mins == 0 {
		mins = 18
	}
	return fmt.Sprintf("%s · Candle-lighting times %d min before sunset", str, mins)
}

// documentKeywords is the port of the Keywords line in createPdfDoc(). The
// full location name is used here, not the short form the title carries, so a
// search for the ZIP code finds the file.
func documentKeywords(p *Params) string {
	kw := "Hebrew calendar, Jewish holidays"
	name := p.LocationName
	if name == "" && p.Opts.Location != nil {
		name = p.Opts.Location.Name
	}
	if name != "" {
		kw += ", " + name
	}
	return kw
}

// hebMonthName returns a Hebrew month name as hebcal-web spells it in the
// subtitle. hebcal-web builds that with Locale.gettext(getMonthName(), locale),
// translating the English name through the locale's .po file -- a Spanish
// calendar reads "Jeshvan" and a German one "Cheschwan", not "Cheshvan". The Go
// equivalent is locales.LookupTranslation, the same call hebcal-go's own event
// rendering uses. Hebrew locales keep the native (nikud or plain) name, and the
// English family keeps the English name with the Tamuz spelling fixed.
func hebMonthName(hd hdate.HDate, locale string) string {
	en := hd.MonthName("en") // raw hdate spelling, e.g. "Tammuz", "Cheshvan"
	switch strings.ToLower(locale) {
	case "", "en", "s", "sephardic",
		"a", "ashkenazi", "ashkenazi_litvish", "ashkenazi_poylish", "ashkenazi_standard":
		return model.FixMonthSpelling(en)
	case "he", "he-x-nonikud":
		return hd.MonthName(locale)
	}
	// Translate the raw English name through the locale's .po, the way
	// hebcal-go's own event rendering does: de/fr have a "Tammus"/"Tammouz" for
	// the month, while es has none and falls back to the fixed "Tamuz" spelling.
	if tr, ok := locales.LookupTranslation(en, locale); ok {
		return tr
	}
	return model.FixMonthSpelling(en)
}

// hebMonthRange builds the Hebrew-month subtitle under a Gregorian month
// title, e.g. "Av – Elul 5786". Port of makeHebMonthStr(): the start year is
// only shown when the month spans a Hebrew year boundary, and the end month
// only when it differs from the start month.
func hebMonthRange(mp MonthPage, p *Params) string {
	firstG := time.Date(mp.Year, mp.Month, 1, 0, 0, 0, 0, time.UTC)
	lastG := firstG.AddDate(0, 1, -1)
	start := hdate.FromTime(firstG)
	end := hdate.FromTime(lastG)

	str := hebMonthName(start, p.Locale)
	if end.Year() != start.Year() {
		str += " " + strconv.Itoa(start.Year())
	}
	if end.Month() != start.Month() {
		str += " – " + hebMonthName(end, p.Locale)
	}
	str += " " + strconv.Itoa(end.Year())
	return strings.ReplaceAll(str, "'", "’")
}

// addLink attaches a URI link annotation over a rectangle given in pdfkit's
// top-down coordinates.
func (r *Renderer) addLink(page *document.Page, href string, x, yTopDown, w, h float64) {
	if w <= 0 || h <= 0 {
		return
	}
	y1 := yLine(yTopDown)
	page.Page.Annots = append(page.Page.Annots, &annotation.Link{
		Common: annotation.Common{
			Rect:  pdflib.Rectangle{LLx: x, LLy: y1 - h, URx: x + w, URy: y1},
			Flags: annotation.FlagPrint,
		},
		Action:    &action.URI{URI: href},
		Highlight: annotation.HighlightNone,
	})
}

// renderHebMonth draws one page in Hebrew-month mode (mm=1 or mm=2).
//
// The grid is the same as the Gregorian one; what changes is the pagination,
// the titles (the Hebrew month is primary and the Gregorian range becomes the
// subtitle) and, for mm=2, the day numbers, which are written in gematriya.
func (r *Renderer) renderHebMonth(doc *document.MultiPage, inst *Instances, p *Params, hp HebMonthPage, campaign string) error {
	page := doc.AddPage()

	first := hdate.New(hp.Year, hp.Month, 1)
	daysInMonth := first.DaysInMonth()
	startDow := int(first.Gregorian().Weekday())
	rows := rowsFor(daysInMonth, startDow)
	rowHeight := (pdfHeight - pdfTMargin - pdfBMargin) / float64(rows)

	r.drawHebMonthTitle(page, inst, p, hp, first, daysInMonth)
	r.drawGrid(page, inst, p, rows, rowHeight)
	r.drawHebDays(page, inst, p, hp, daysInMonth, startDow, rowHeight, campaign)
	r.drawFooter(page, inst, p)

	return page.Close()
}

// hebTitleYear formats the Hebrew year for a Hebrew-month title. pdfkit's
// pdfMonthTitleHebrew keys this on rtl, not on gematriyaNumerals:
// `const yearStr = rtl ? gematriya(year) : year`. So mm=2 with a non-Hebrew
// locale (e.g. lg=s) draws the year as a plain number, even though the day
// numbers below it are still in gematriya (which uses useGematriya). Keying it
// on useGematriya() instead put the Hebrew year letters into the Latin FontSemi
// used for a non-RTL title, rendering them as tofu boxes.
func hebTitleYear(p *Params, year int) string {
	if p.RTL {
		return gematriya.Gematriya(year)
	}
	return strconv.Itoa(year)
}

// drawHebMonthTitle writes the Hebrew month and year, with the Gregorian range
// beneath. Port of pdfMonthTitleHebrew().
func (r *Renderer) drawHebMonthTitle(page *document.Page, inst *Instances, p *Params, hp HebMonthPage, first hdate.HDate, daysInMonth int) {
	titleFont, subFont := FontSemi, FontPlain
	if p.RTL {
		titleFont, subFont = FontHebrew, FontHebrew
	}
	title := hebMonthName(first, p.Locale) + " " + hebTitleYear(p, hp.Year)
	r.drawCentered(page, inst, titleFont, 26, colorBlack, pdfTMargin-24, title)

	last := hdate.New(hp.Year, hp.Month, daysInMonth)
	r.drawCentered(page, inst, subFont, 14, colorBlack, pdfTMargin+4,
		gregRange(first.Gregorian(), last.Gregorian(), model.NamesFor(p.Locale)))
}

// gregRange formats the Gregorian span of a Hebrew month, matching the three
// cases in pdfMonthTitleHebrew(): same month, same year, or spanning years.
func gregRange(start, end time.Time, names model.CalendarNames) string {
	mon := func(t time.Time) string {
		return names.MonthsShort[int(t.Month())-1]
	}
	d := func(t time.Time) string { return strconv.Itoa(t.Day()) }
	y := func(t time.Time) string { return strconv.Itoa(t.Year()) }
	switch {
	case start.Month() == end.Month() && start.Year() == end.Year():
		return mon(start) + " " + d(start) + " – " + d(end) + ", " + y(end)
	case start.Year() == end.Year():
		return mon(start) + " " + d(start) + " – " + mon(end) + " " + d(end) + ", " + y(end)
	default:
		return mon(start) + " " + d(start) + ", " + y(start) + " –" + mon(end) + " " + d(end) + ", " + y(end)
	}
}

// drawHebDays fills the cells for a Hebrew month, including the grey Elul days
// folded onto the Tishrei page.
func (r *Renderer) drawHebDays(page *document.Page, inst *Instances, p *Params, hp HebMonthPage, daysInMonth, startDow int, rowHeight float64, campaign string) {
	xposNewRow := pdfLMargin + pdfColWidth - 4
	mult := 1.0
	if p.RTL {
		xposNewRow = pdfWidth - pdfRMargin - 4
		mult = -1
	}
	eventX := func(dow int) float64 {
		return cellOrigin(p.RTL, dow)
	}
	numFont := FontSemi
	if p.useGematriya() {
		numFont = FontHebrew
	}
	ypos := pdfTMargin + 40.0

	// Leading Elul days, drawn grey in the cells before the 1st. The last day
	// of Elul is never a Saturday, so these never need an extra week.
	if len(hp.PrevDays) > 0 {
		const elulDays = 29
		for d := 0; d < startDow; d++ {
			elulDay := elulDays - (startDow - d - 1)
			evs := hp.PrevDays[elulDay]
			if len(evs) == 0 {
				continue
			}
			x := xposNewRow + float64(d)*pdfColWidth*mult
			r.drawRightAligned(page, inst, numFont, 14, colorGray, x, ypos, r.dayNumber(p, elulDay))
			r.renderAltDateOnLine(page, inst, p, firstAltDate(evs), x, ypos, colorGray)
			y := ypos + 22
			for i := range evs {
				if evs[i].AltDate {
					continue
				}
				if y+10 > ypos+rowHeight {
					break
				}
				y = r.renderEventColored(page, inst, p, &evs[i], eventX(d), y, campaign, colorGray)
			}
		}
	}

	dow := startDow
	xpos := xposNewRow + float64(dow)*pdfColWidth*mult
	for mday := 1; mday <= daysInMonth; mday++ {
		r.drawRightAligned(page, inst, numFont, 14, colorBlack, xpos, ypos, r.dayNumber(p, mday))

		evs := hp.Days[mday]
		r.renderAltDateOnLine(page, inst, p, firstAltDate(evs), xpos, ypos, nil)

		y := ypos + 22
		for i := range evs {
			if evs[i].AltDate {
				continue
			}
			if y+10 > ypos+rowHeight {
				break
			}
			y = r.renderEvent(page, inst, p, &evs[i], eventX(dow), y, campaign)
		}

		if dow++; dow == 7 {
			dow = 0
			xpos = xposNewRow
			ypos += rowHeight
		} else {
			xpos += pdfColWidth * mult
		}
	}
}

// dayNumber renders a day of the month, in gematriya when mm=2 asked for it.
func (r *Renderer) dayNumber(p *Params, day int) string {
	if p.useGematriya() {
		return gematriya.Gematriya(day)
	}
	return strconv.Itoa(day)
}

// cellOrigin returns the x that src/pdf.js passes to renderPdfEvent for a
// column: the cell's left edge less the cell margin. The alignment branches in
// renderEventColored add their own insets to it, so this is deliberately not
// the position anything is drawn at.
func cellOrigin(rtl bool, dow int) float64 {
	if rtl {
		return pdfWidth - pdfRMargin - float64(dow+1)*pdfColWidth - pdfCellMargin
	}
	return pdfLMargin + float64(dow)*pdfColWidth - pdfCellMargin
}
