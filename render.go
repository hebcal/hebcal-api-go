package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/hebcal/gematriya"
	"github.com/hebcal/hdate"
)

type heDateParts struct {
	Y string `json:"y"`
	M string `json:"m"`
	D string `json:"d"`
}

func makeHeDateParts(hd hdate.HDate) heDateParts {
	return heDateParts{
		Y: gematriya.Gematriya(hd.Year()),
		M: hd.MonthName("he-x-NoNikud"),
		D: gematriya.Gematriya(hd.Day()),
	}
}

// jsonMarshal marshals without HTML escaping and without a trailing newline,
// matching JSON.stringify.
func jsonMarshal(v interface{}) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	b := buf.Bytes()
	return bytes.TrimSuffix(b, []byte{'\n'})
}

type singleJSON struct {
	Gy          int         `json:"gy"`
	Gm          int         `json:"gm"`
	Gd          int         `json:"gd"`
	AfterSunset bool        `json:"afterSunset"`
	Hy          int         `json:"hy"`
	Hm          string      `json:"hm"`
	Hd          int         `json:"hd"`
	Hebrew      string      `json:"hebrew"`
	HeDateParts heDateParts `json:"heDateParts"`
	Events      []string    `json:"events,omitempty"`
	Il          *bool       `json:"il,omitempty"`
}

// renderSingleJSON builds the single-date JSON response body.
func renderSingleJSON(p convProps, q url.Values, lg string) []byte {
	hd := p.hd
	result := singleJSON{
		Gy:          p.dt.Year,
		Gm:          int(p.dt.Month),
		Gd:          p.dt.Day,
		AfterSunset: p.gs,
		Hy:          hd.Year(),
		Hm:          hdMonthNameEn(hd),
		Hd:          hd.Day(),
		Hebrew:      gematriyaDate(hd),
		HeDateParts: makeHeDateParts(hd),
	}
	il := q.Get("i") == "on"
	events := getEvents(hd, il)
	if len(events) != 0 {
		result.Events = renderEvents(events, lg)
		if q.Has("i") {
			result.Il = &il
		}
	}
	return jsonMarshal(result)
}

func renderEvents(events []calEv, lg string) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = renderEvent(ev, lg)
	}
	return out
}

type rangeItemJSON struct {
	Hy          int         `json:"hy"`
	Hm          string      `json:"hm"`
	Hd          int         `json:"hd"`
	Hebrew      string      `json:"hebrew"`
	HeDateParts heDateParts `json:"heDateParts"`
	Events      []string    `json:"events,omitempty"`
	Il          *bool       `json:"il,omitempty"`
}

// renderRangeJSON builds the batch (date range) JSON response body,
// preserving chronological key order in the hdates object.
func renderRangeJSON(p convProps, q url.Values, lg string) []byte {
	il := q.Get("i") == "on"
	hasI := q.Has("i")
	var buf bytes.Buffer
	buf.WriteString(`{"start":"`)
	buf.WriteString(gregFromRD(p.startRD).String())
	buf.WriteString(`","end":"`)
	buf.WriteString(gregFromRD(p.endRD).String())
	buf.WriteString(`","hdates":{`)
	first := true
	for rd := p.startRD; rd <= p.endRD; rd++ {
		hd := hdateFromRD(rd)
		item := rangeItemJSON{
			Hy:          hd.Year(),
			Hm:          hdMonthNameEn(hd),
			Hd:          hd.Day(),
			Hebrew:      gematriyaDate(hd),
			HeDateParts: makeHeDateParts(hd),
		}
		events := getEvents(hd, il)
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
		buf.WriteString(gregFromRD(rd).String())
		buf.WriteString(`":`)
		buf.Write(jsonMarshal(item))
	}
	buf.WriteString("}}")
	return buf.Bytes()
}

// renderXML builds the XML response body, matching views/converter-xml.ejs.
func renderXML(p convProps, q url.Values, lg string) []byte {
	hd := p.hd
	parts := makeHeDateParts(hd)
	var buf bytes.Buffer
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<hebcal>\n")
	sunset := ""
	if p.gs {
		sunset = `sunset="1" `
	}
	fmt.Fprintf(&buf, "<gregorian year=\"%d\" month=\"%d\" day=\"%d\" %s/>\n",
		p.dt.Year, int(p.dt.Month), p.dt.Day, sunset)
	fmt.Fprintf(&buf, "<hebrew year=\"%d\" month=\"%s\" day=\"%d\" str=\"%s\" />\n",
		hd.Year(), xmlEscape(hdMonthNameEn(hd)), hd.Day(), xmlEscape(gematriyaDate(hd)))
	fmt.Fprintf(&buf, "<hebdate year=\"%s\" month=\"%s\" day=\"%s\" />\n",
		xmlEscape(parts.Y), xmlEscape(parts.M), xmlEscape(parts.D))
	il := q.Get("i") == "on"
	events := getEvents(hd, il)
	if len(events) != 0 {
		diaspora := getEvents(hd, false)
		israel := getEvents(hd, true)
		inIsrael := func(desc string) bool {
			for _, b := range israel {
				if b.desc() == desc {
					return true
				}
			}
			return false
		}
		inDiaspora := func(desc string) bool {
			for _, b := range diaspora {
				if b.desc() == desc {
					return true
				}
			}
			return false
		}
		buf.WriteString("<events>\n")
		for _, ev := range diaspora {
			if inIsrael(ev.desc()) {
				// href attribute omitted entirely when the event has no URL
				href := ""
				if u := ev.url(); u != "" {
					href = fmt.Sprintf("href=\"%s\"", xmlEscape(u))
				}
				fmt.Fprintf(&buf, " <event name=\"%s\" diaspora=\"1\" israel=\"1\" %s />\n",
					xmlEscape(renderEvent(ev, lg)), href)
			}
		}
		for _, ev := range diaspora {
			if !inIsrael(ev.desc()) {
				fmt.Fprintf(&buf, " <event name=\"%s\" diaspora=\"1\" israel=\"0\" href=\"%s\" />\n",
					xmlEscape(renderEvent(ev, lg)), xmlEscape(ev.url()))
			}
		}
		for _, ev := range israel {
			if !inDiaspora(ev.desc()) {
				fmt.Fprintf(&buf, " <event name=\"%s\" diaspora=\"0\" israel=\"1\" href=\"%s\" />\n",
					xmlEscape(renderEvent(ev, lg)), xmlEscape(ev.url()))
			}
		}
		buf.WriteString("</events>\n")
	}
	buf.WriteString("</hebcal>\n")
	return buf.Bytes()
}

// renderCSV builds the CSV body listing the Gregorian dates on which the
// Hebrew date falls over a range of years. Ported from converter.js
// dateConverterCsv() / makeFutureYearsHeb().
func renderCSV(hd hdate.HDate) []byte {
	var buf bytes.Buffer
	buf.WriteString("Gregorian Date,Hebrew Date\r\n")
	for _, item := range futureYearsHeb(hd, 75) {
		gy, gm, gd := item.ProlepticGreg()
		buf.WriteString(isoDateString(gy, gm, gd))
		buf.WriteByte(',')
		buf.WriteString(hdateString(item))
		buf.WriteString("\r\n")
	}
	return buf.Bytes()
}

// futureYearsHeb returns the same Hebrew calendar date across a range of
// years, from 5 years before to numYears after the original date, applying
// the same Adar and end-of-month adjustments as the JS makeFutureYearsHeb().
func futureYearsHeb(orig hdate.HDate, numYears int) []hdate.HDate {
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
		arr = append(arr, newHDateLenient(hyear, hm, hd))
	}
	return arr
}

// csvFilename returns the attachment filename, e.g. "hdate-20-tamuz.csv".
func csvFilename(hd hdate.HDate) string {
	return fmt.Sprintf("hdate-%d-%s.csv", hd.Day(), makeAnchor(hdMonthNameEn(hd)))
}

// stripCallback keeps only characters valid in a JSONP callback name.
func stripCallback(cb string) string {
	var b strings.Builder
	for _, r := range cb {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
