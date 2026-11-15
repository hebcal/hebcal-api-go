package fridge

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"html/template"
	"net/url"
	"strconv"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
)

//go:embed page.html.tmpl
var pageSource string

var pageTemplate = template.Must(template.New("fridge").Parse(pageSource))

// cell is one candle-lighting as the table draws it: five <td>s.
type cell struct {
	Month, Day, Reason string
	Time, Havdalah     string
	yomTov             bool
	// pad is the class that sets the right-hand column off from the rule
	// down the middle of the table, "" for the left-hand column.
	pad string
	// narrow is the class a long or right-to-left reason takes.
	narrow string
}

func classes(names ...string) string {
	out := names[:0]
	for _, n := range names {
		if n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, " ")
}

func (c *cell) bold() string {
	if c.yomTov {
		return "yomtov"
	}
	return ""
}

func (c *cell) MonthClass() string  { return classes(c.bold(), c.pad) }
func (c *cell) DayClass() string    { return classes(c.bold(), "text-right") }
func (c *cell) ReasonClass() string { return classes(c.bold(), c.narrow) }
func (c *cell) TimeClass() string   { return classes(c.bold(), "text-right") }

// row pairs the nth candle-lighting of the first half of the year with the
// nth of the second; Right is nil on the last row of an odd count.
type row struct {
	Left, Right *cell
}

type nav struct {
	PrevHeb, PrevGreg, NextHeb, NextGreg int
}

type page struct {
	Lang           string
	Hebrew         bool
	Location       string
	ShortLocation  string
	Year           int
	Heading        string
	CandleLighting string
	Rows           []row
	BaseURL        string
	Nav            nav
	CandleMins     int
	Havdalah       HavdalahNote
	Nonce          string
}

// NewNonce returns a fresh random value for a Content-Security-Policy nonce
// and the script tags that carry it.
func NewNonce() string {
	var buf [9]byte
	rand.Read(buf[:])
	return base64.RawURLEncoding.EncodeToString(buf[:])
}

// HTML renders the page. Its inline script carries nonce, which the
// response's Content-Security-Policy must name. The links to the previous
// and next year are offered only for years that would be served today.
func (c *Calendar) HTML(q url.Values, today model.GregDate, nonce string) ([]byte, error) {
	lang := c.Lang()
	hebrew := lang == "he"
	gy1, gy2 := c.Year.GregYears()
	p := page{
		Lang:           lang,
		Hebrew:         hebrew,
		Location:       c.Location.Name,
		ShortLocation:  c.Location.ShortName(),
		Year:           c.Year.Number,
		CandleLighting: model.Gettext("Candle lighting", c.Locale),
		Rows:           c.rows(lang),
		BaseURL:        c.BaseURL(q),
		CandleMins:     c.Options.CandleLightingMins,
		Havdalah:       c.Havdalah(),
		Nonce:          nonce,
	}
	prevHeb, nextHeb := gy1+3759, gy1+3761
	if c.Year.Hebrew {
		p.Heading = fmt.Sprintf("Hebrew Year %d (%d - %d)", c.Year.Number, gy1, gy2)
		prevHeb, nextHeb = c.Year.Number-1, c.Year.Number+1
	} else {
		p.Heading = fmt.Sprintf("%d (%d - %d)", gy1, gy1+3760, gy1+3761)
	}
	inWindow := func(year int, isHebrew bool) int {
		if InWindow(year, isHebrew, today) {
			return year
		}
		return 0
	}
	p.Nav = nav{
		PrevHeb:  inWindow(prevHeb, true),
		PrevGreg: inWindow(gy1-1, false),
		NextHeb:  inWindow(nextHeb, true),
		NextGreg: inWindow(gy1+1, false),
	}
	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, &p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rows lays the items out in two columns, the first half of the year on the
// left.
func (c *Calendar) rows(lang string) []row {
	months := model.NamesFor(lang).MonthsShort
	pad := "leftpad"
	if lang == "he" {
		pad = "rightpad"
	}
	cells := make([]cell, len(c.Items))
	for i, it := range c.Items {
		cells[i] = cell{
			Month:    months[it.Date.Month-1],
			Day:      strconv.Itoa(it.Date.Day),
			Reason:   it.Reason,
			Time:     it.Time,
			Havdalah: it.Havdalah,
			yomTov:   it.YomTov,
			narrow:   narrowClass(lang, it.Reason),
		}
	}
	half := (len(cells) + 1) / 2
	rows := make([]row, half)
	for i := range rows {
		rows[i].Left = &cells[i]
		if j := i + half; j < len(cells) {
			cells[j].pad = pad
			rows[i].Right = &cells[j]
		}
	}
	return rows
}

// narrowClass picks the class for a reason's cell: Hebrew is right-aligned,
// and a name too long for the column is set in the condensed face. Cyrillic
// runs wide, so Russian switches sooner.
func narrowClass(lang, reason string) string {
	n := utf16Len(reason)
	switch {
	case lang == "he":
		return "text-right"
	case lang == "ru" && n > 10, n > 14:
		return "narrow"
	}
	return ""
}

// byteOrderMark opens the CSV so that Excel reads it as UTF-8.
const byteOrderMark = "\xEF\xBB\xBF"

// CSV renders the export: a header and one row for each candle-lighting,
// with ISO dates.
func (c *Calendar) CSV() []byte {
	var buf bytes.Buffer
	buf.WriteString(byteOrderMark)
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	w.Write([]string{"Date", "Parsha", "Candles", "Havdalah"})
	for _, it := range c.Items {
		w.Write([]string{it.Date.String(), it.Reason, it.Time, it.Havdalah})
	}
	w.Flush()
	return buf.Bytes()
}

// CSVFilename names the export after the location and year,
// "candles-jerusalem-5787.csv".
func (c *Calendar) CSVFilename() string {
	loc := c.Location
	name := loc.Zip
	if name == "" {
		name = loc.Asciiname
	}
	if name == "" {
		name = loc.ShortName()
	}
	return fmt.Sprintf("candles-%s-%d.csv", jsutil.MakeAnchor(name), c.Year.Number)
}
