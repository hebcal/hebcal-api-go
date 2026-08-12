package converter

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/hebcal/hdate"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
)

type singleJSON struct {
	Gy          int               `json:"gy"`
	Gm          int               `json:"gm"`
	Gd          int               `json:"gd"`
	AfterSunset bool              `json:"afterSunset"`
	Hy          int               `json:"hy"`
	Hm          string            `json:"hm"`
	Hd          int               `json:"hd"`
	Hebrew      string            `json:"hebrew"`
	HeDateParts model.HeDateParts `json:"heDateParts"`
	Events      []string          `json:"events,omitempty"`
	Il          *bool             `json:"il,omitempty"`
}

// RenderSingleJSON builds the single-date JSON response body.
func RenderSingleJSON(p Props, q url.Values, lg string) []byte {
	hd := p.HD
	result := singleJSON{
		Gy:          p.DT.Year,
		Gm:          int(p.DT.Month),
		Gd:          p.DT.Day,
		AfterSunset: p.GS,
		Hy:          hd.Year(),
		Hm:          model.HDMonthNameEn(hd),
		Hd:          hd.Day(),
		Hebrew:      model.GematriyaDate(hd),
		HeDateParts: model.MakeHeDateParts(hd),
	}
	il := q.Get("i") == "on"
	events := model.GetEvents(hd, il)
	if len(events) != 0 {
		result.Events = renderEvents(events, lg)
		if q.Has("i") {
			result.Il = &il
		}
	}
	return jsutil.Marshal(result)
}

func renderEvents(events []model.CalEv, lg string) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = model.RenderEvent(ev, lg)
	}
	return out
}

type rangeItemJSON struct {
	Hy          int               `json:"hy"`
	Hm          string            `json:"hm"`
	Hd          int               `json:"hd"`
	Hebrew      string            `json:"hebrew"`
	HeDateParts model.HeDateParts `json:"heDateParts"`
	Events      []string          `json:"events,omitempty"`
	Il          *bool             `json:"il,omitempty"`
}

// RenderRangeJSON builds the batch (date range) JSON response body,
// preserving chronological key order in the hdates object.
func RenderRangeJSON(p Props, q url.Values, lg string) []byte {
	il := q.Get("i") == "on"
	hasI := q.Has("i")
	var buf bytes.Buffer
	buf.WriteString(`{"start":"`)
	buf.WriteString(model.GregFromRD(p.StartRD).String())
	buf.WriteString(`","end":"`)
	buf.WriteString(model.GregFromRD(p.EndRD).String())
	buf.WriteString(`","hdates":{`)
	first := true
	for rd := p.StartRD; rd <= p.EndRD; rd++ {
		hd := model.HDateFromRD(rd)
		item := rangeItemJSON{
			Hy:          hd.Year(),
			Hm:          model.HDMonthNameEn(hd),
			Hd:          hd.Day(),
			Hebrew:      model.GematriyaDate(hd),
			HeDateParts: model.MakeHeDateParts(hd),
		}
		events := model.GetEvents(hd, il)
		if len(events) != 0 {
			item.Events = renderEvents(events, lg)
			if hasI {
				item.Il = &il
			}
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteByte('"')
		buf.WriteString(model.GregFromRD(rd).String())
		buf.WriteString(`":`)
		buf.Write(jsutil.Marshal(item))
	}
	buf.WriteString("}}")
	return buf.Bytes()
}

// RenderXML builds the XML response body, matching views/converter-xml.ejs.
func RenderXML(p Props, q url.Values, lg string) []byte {
	hd := p.HD
	parts := model.MakeHeDateParts(hd)
	var buf bytes.Buffer
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<hebcal>\n")
	sunset := ""
	if p.GS {
		sunset = `sunset="1" `
	}
	fmt.Fprintf(&buf, "<gregorian year=\"%d\" month=\"%d\" day=\"%d\" %s/>\n",
		p.DT.Year, int(p.DT.Month), p.DT.Day, sunset)
	fmt.Fprintf(&buf, "<hebrew year=\"%d\" month=\"%s\" day=\"%d\" str=\"%s\" />\n",
		hd.Year(), jsutil.XMLEscape(model.HDMonthNameEn(hd)), hd.Day(), jsutil.XMLEscape(model.GematriyaDate(hd)))
	fmt.Fprintf(&buf, "<hebdate year=\"%s\" month=\"%s\" day=\"%s\" />\n",
		jsutil.XMLEscape(parts.Y), jsutil.XMLEscape(parts.M), jsutil.XMLEscape(parts.D))
	il := q.Get("i") == "on"
	events := model.GetEvents(hd, il)
	if len(events) != 0 {
		diaspora := model.GetEvents(hd, false)
		israel := model.GetEvents(hd, true)
		inIsrael := func(desc string) bool {
			for _, b := range israel {
				if b.Desc() == desc {
					return true
				}
			}
			return false
		}
		inDiaspora := func(desc string) bool {
			for _, b := range diaspora {
				if b.Desc() == desc {
					return true
				}
			}
			return false
		}
		buf.WriteString("<events>\n")
		for _, ev := range diaspora {
			if inIsrael(ev.Desc()) {
				// href attribute omitted entirely when the event has no URL
				href := ""
				if u := ev.URL(); u != "" {
					href = fmt.Sprintf("href=\"%s\"", jsutil.XMLEscape(u))
				}
				fmt.Fprintf(&buf, " <event name=\"%s\" diaspora=\"1\" israel=\"1\" %s />\n",
					jsutil.XMLEscape(model.RenderEvent(ev, lg)), href)
			}
		}
		for _, ev := range diaspora {
			if !inIsrael(ev.Desc()) {
				fmt.Fprintf(&buf, " <event name=\"%s\" diaspora=\"1\" israel=\"0\" href=\"%s\" />\n",
					jsutil.XMLEscape(model.RenderEvent(ev, lg)), jsutil.XMLEscape(ev.URL()))
			}
		}
		for _, ev := range israel {
			if !inDiaspora(ev.Desc()) {
				fmt.Fprintf(&buf, " <event name=\"%s\" diaspora=\"0\" israel=\"1\" href=\"%s\" />\n",
					jsutil.XMLEscape(model.RenderEvent(ev, lg)), jsutil.XMLEscape(ev.URL()))
			}
		}
		buf.WriteString("</events>\n")
	}
	buf.WriteString("</hebcal>\n")
	return buf.Bytes()
}

// RenderCSV builds the CSV body listing the Gregorian dates on which the
// Hebrew date falls over a range of years. Ported from converter.js
// dateConverterCsv() / makeFutureYearsHeb().
func RenderCSV(hd hdate.HDate) []byte {
	var buf bytes.Buffer
	buf.WriteString("Gregorian Date,Hebrew Date\r\n")
	for _, item := range FutureYearsHeb(hd, 75) {
		gy, gm, gd := item.ProlepticGreg()
		buf.WriteString(jsutil.IsoDateString(gy, gm, gd))
		buf.WriteByte(',')
		buf.WriteString(model.HDateString(item))
		buf.WriteString("\r\n")
	}
	return buf.Bytes()
}

// FutureYearsHeb returns the same Hebrew calendar date across a range of
// years, from 5 years before to numYears after the original date, applying
// the same Adar and end-of-month adjustments as the JS makeFutureYearsHeb().
func FutureYearsHeb(orig hdate.HDate, numYears int) []hdate.HDate {
	hy := orig.Year()
	month := orig.Month()
	day := orig.Day()
	isOrigAdar := month == hdate.Adar1
	isOrigAdarNonLeap := isOrigAdar && !hdate.IsLeapYear(hy)
	isAdar30 := isOrigAdar && day == 30
	var arr []hdate.HDate
	for i := -5; i <= numYears; i++ {
		hyear := hy + i
		if hyear < 1 {
			continue
		}
		isLeap := hdate.IsLeapYear(hyear)
		hm := month
		hd := day
		if isOrigAdarNonLeap && isLeap {
			hm = hdate.Adar2
		} else if isAdar30 && !isLeap {
			hm = hdate.Nisan
			hd = 1
		}
		arr = append(arr, model.NewHDateLenient(hyear, hm, hd))
	}
	return arr
}

// CSVFilename returns the attachment filename, e.g. "hdate-20-tamuz.csv".
func CSVFilename(hd hdate.HDate) string {
	return fmt.Sprintf("hdate-%d-%s.csv", hd.Day(), jsutil.MakeAnchor(model.HDMonthNameEn(hd)))
}

// StripCallback keeps only characters valid in a JSONP callback name.
func StripCallback(cb string) string {
	var b strings.Builder
	for _, r := range cb {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
