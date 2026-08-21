// Package pdf renders the Hebcal.com PDF calendars served from
// download.hebcal.com/v4/<data>/<name>.pdf, a port of the pdfkit implementation
// in hebcal-web's src/pdf.js.
//
// The bar is that a calendar rendered here is indistinguishable from the one
// production serves for the same URL, so most of this package is written to be
// read side by side with the JavaScript it came from. See CLAUDE.md for the
// things that cost real time to work out (pdfkit's top-down coordinates, hhea
// baselines, shaping at a reference size) before changing layout or shaping.
//
// A request arrives as a base64 protobuf in the URL path. DecodeParams turns it
// into Params, Generate produces the calendar's events, and Renderer.Render
// draws them; Service.Prepare and Service.Render bundle those steps in the
// order the handler needs them, which is the same order hebcal-web's
// src/hebcal-download.js runs in.
package pdf

import (
	"bytes"
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/reqlog"
	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// Service holds everything a PDF request needs beyond the request itself.
type Service struct {
	// Renderer draws the calendar. It is nil when the fonts could not be
	// loaded, in which case the PDF routes report 503 and the rest of the API
	// keeps working.
	Renderer *Renderer
	// Geo resolves geonameid/ZIP/legacy-city locations. It may be nil, as it
	// may be for the other location-dependent routes.
	Geo *geodb.DB
	// Learning fetches the daily-learning series no Go schedule generates. Nil
	// when no hebcal-web URL is configured, in which case those requests are
	// refused rather than rendered incomplete.
	Learning *LearningFetcher
}

// Available reports whether calendars can be rendered at all, i.e. whether the
// fonts were loaded at startup.
func (s *Service) Available() bool { return s != nil && s.Renderer != nil }

// Calendar is a decoded request together with the events it generated and the
// document title derived from both.
type Calendar struct {
	Params *Params
	Events []Event
	Title  string
}

// UnsupportedSeriesError reports daily-learning series this build cannot
// generate. The two cases are deliberately different: with no hebcal-web
// configured the calendar can never be rendered (501, retrying cannot help),
// while a configured hebcal-web that did not answer is transient (503).
type UnsupportedSeriesError struct {
	// Series names the schedules that could not be produced.
	Series []string
	// Retryable is true when a hebcal-web fetch failed, false when no
	// hebcal-web URL is configured at all.
	Retryable bool
	// Err is the underlying fetch failure, if there was one.
	Err error
}

func (e *UnsupportedSeriesError) Error() string {
	if e.Retryable {
		return "daily-learning fetch failed: " + e.Err.Error()
	}
	return "daily-learning series not supported by this service"
}

func (e *UnsupportedSeriesError) Unwrap() error { return e.Err }

// Header is the X-Unsupported-Series value naming what was missing.
func (e *UnsupportedSeriesError) Header() string { return strings.Join(e.Series, ", ") }

// Prepare decodes a /v4/<data>/<name>.pdf path and generates the calendar's
// events, in the order src/hebcal-download.js does: decode, generate, fill in
// the daily-learning series hebcal-go cannot produce, and only then refuse an
// empty calendar -- those fetched rows can be the whole calendar.
//
// The errors it returns carry their own status: *NotFoundError is 404,
// *OutOfRangeError 410, *UnsupportedSeriesError 501 or 503, a *model.HTTPError
// whatever it says, and anything else is a malformed request (400).
func (s *Service) Prepare(ctx context.Context, u *url.URL) (*Calendar, error) {
	msg, err := decodeRequest(u)
	if err != nil {
		return nil, err
	}
	// The /v4/ base64 protobuf and the /v2/h/ base64 query string are both opaque
	// in the access log; record the query string the message decodes to so the
	// request is readable and reproducible under the log's "qs" key. The classic
	// /hebcal/index.cgi/ form already carries its query in the logged URL.
	if isOpaquePath(u.Path) {
		reqlog.FromContext(ctx).SetQuery(MessageToQuery(msg))
	}
	params, err := ParamsFromMessage(msg, s.Geo)
	if err != nil {
		return nil, err
	}
	events, err := Generate(params)
	if err != nil {
		return nil, model.Internal("render: %s", err.Error())
	}

	if missing := unsupportedSeries(msg); len(missing) > 0 {
		if s.Learning == nil {
			return nil, &UnsupportedSeriesError{Series: missing}
		}
		start, end, ok := learningRange(params, events)
		if !ok {
			return nil, model.Internal("render: cannot determine the calendar's date range")
		}
		extra, err := s.Learning.Fetch(ctx, missing, params.LG, start, end)
		if err != nil {
			return nil, &UnsupportedSeriesError{Series: missing, Retryable: true, Err: err}
		}
		events = mergeLearning(events, extra)
	}

	// hebcal-web answers a PDF request that produced no events with 400 rather
	// than an empty document (src/hebcal-download.js). The check comes after
	// the fetch above, since those rows can be the whole calendar.
	if len(events) == 0 {
		return nil, model.BadRequest("Please select at least one event option")
	}
	return &Calendar{Params: params, Events: events, Title: CalendarTitle(params, events)}, nil
}

// Render draws a prepared calendar.
//
// The PDF is built into a buffer rather than streamed: the renderer can fail
// part-way through (a bad date range, an unrenderable option), and a partial
// PDF written straight to the socket would reach the client as a corrupt file
// under a 200.
func (s *Service) Render(cal *Calendar) ([]byte, error) {
	if !s.Available() {
		return nil, model.Unavailable("PDF rendering is not available")
	}
	var buf bytes.Buffer
	if err := s.Renderer.Render(&buf, cal.Params, cal.Events, cal.Title); err != nil {
		return nil, model.Internal("render: %s", err.Error())
	}
	return buf.Bytes(), nil
}

// decodeRequest turns any of the three download URL shapes into the Download
// message it carries: the current /v4/<base64-protobuf>/<name>.pdf, the legacy
// /v2/h/<base64-querystring>/<name>.pdf that hebcal-web answers with a 301 to
// the former (see v2.go), and the classic /hebcal/index.cgi/<name>.pdf?<query>
// older than both (see cgi.go). From here on all three are the same request.
//
// isOpaquePath reports whether a download URL hides its calendar options behind
// a base64 payload -- the /v4/ protobuf and the /v2/h/ query string both do, so
// the logged URL is unreadable for either. The classic /hebcal/index.cgi/ form
// carries its query in the URL already, so its options are visible in the log's
// "url" without a "qs" field.
func isOpaquePath(path string) bool {
	return !strings.HasPrefix(path, cgiPrefix)
}

// A URL that is none of these is 404, which is what hebcal-web's router answers.
func decodeRequest(u *url.URL) (*downloadpb.Download, error) {
	switch path := u.Path; {
	case strings.HasPrefix(path, cgiPrefix):
		q, err := ParseCGIPath(path, u.RawQuery)
		if err != nil {
			return nil, NotFoundf("%s", err.Error())
		}
		return DecodeV2(q)
	case strings.HasPrefix(path, "/v2/"):
		q, err := ParseV2Path(path)
		if err != nil {
			return nil, NotFoundf("%s", err.Error())
		}
		return DecodeV2(q)
	default:
		payload, err := ParsePath(path)
		if err != nil {
			return nil, NotFoundf("%s", err.Error())
		}
		return DecodeMessage(payload)
	}
}

// CalendarTitle builds the document title, e.g. "Hebcal Palo Alto 2028",
// "Hebcal Diaspora August 2026" or "Hebcal Palo Alto 2026-2027".
//
// Port of getCalendarTitle() in @hebcal/rest-api. CampaignName builds the link
// tracking campaign from the same function, so the date range the two show can
// never drift apart -- only the location name differs, deliberately.
func CalendarTitle(p *Params, events []Event) string {
	return calendarTitle(p, events, false)
}

// CampaignName is the uc= / utm_campaign value every link on the calendar
// carries. Port of campaignName() in src/hebcal-download.js, which is the
// document title again -- but built with `preferAsciiName: true`, and that is
// not a cosmetic difference:
//
//	geonameid=5128581  title "Hebcal New York 2026"  campaign pdf-new-york-city-2026
//	geonameid=2657896  title "Hebcal Zürich 2026"    campaign pdf-zuerich-2026
//
// shortLocationName() takes the location's raw geonames asciiname whenever it
// has one, and getShortName() only otherwise. So the campaign is not
// campaignFromTitle(document title) for a location whose asciiname differs from
// its short name -- which is every accented city, and a few whose geonames row
// is longer than their display name.
func CampaignName(p *Params, events []Event) string {
	return campaignFromTitle(calendarTitle(p, events, true))
}

// calendarTitle is getCalendarTitle(); preferAscii is its preferAsciiName
// option, which only the campaign sets.
func calendarTitle(p *Params, events []Event, preferAscii bool) string {
	title := "Hebcal"
	cityName := p.CityName
	if preferAscii && p.CityNameAscii != "" {
		cityName = p.CityNameAscii
	}
	switch {
	case cityName != "":
		title += " " + cityName
	case p.Opts.Location != nil && p.Opts.Location.Name != "":
		title += " " + p.Opts.Location.Name
	case p.Opts.IL:
		title += " Israel"
	default:
		title += " Diaspora"
	}
	if p.Subscribe {
		return title
	}
	if p.Opts.Year != 0 && (p.Opts.IsHebrewYear || len(events) == 0) {
		return title + " " + strconv.Itoa(p.Opts.Year)
	}
	if len(events) == 0 {
		return title
	}
	start := events[0].Greg
	end := events[len(events)-1].Greg
	switch {
	case start.Year() != end.Year():
		title += " " + strconv.Itoa(start.Year()) + "-" + strconv.Itoa(end.Year())
	case start.Month() == end.Month():
		title += " " + start.Format("January") + " " + strconv.Itoa(start.Year())
	default:
		title += " " + strconv.Itoa(start.Year())
	}
	return title
}

// campaignFromTitle is the second half of campaignName(): drop the leading
// "Hebcal" and makeAnchor the rest, giving "pdf-diaspora-august-2026". Its
// argument is the ascii-preferring title, so call it through CampaignName
// rather than passing the document's own title.
//
// jsutil.MakeAnchor is what keeps punctuation out of the campaign: a title like
// "Hebcal Washington, D.C 2026" or the "40°42′N 74°0′W America/New_York" name a
// degrees/minutes location gets (see applyV2Location) would otherwise leave
// punctuation in the uc= campaign, where it is then percent-encoded and no
// longer matches production's. It drops the straight apostrophe and hyphenates
// every other non-ASCII-word character, so "Yom HaAtzma'ut" becomes
// "yom-haatzmaut" but the typographic "Sh’vat" becomes "sh-vat".
func campaignFromTitle(title string) string {
	if i := strings.Index(title, " "); i >= 0 {
		return "pdf-" + jsutil.MakeAnchor(title[i+1:])
	}
	return "pdf"
}

// campaignFor returns the campaign one event's link is tagged with. It is
// renderPdfEvent's `options.utmCampaign || 'pdf-' + evt.getDate().getFullYear()`:
// a download names the whole document, while a /holidays/ calendar, which sets
// no campaign, names each event's own Hebrew year.
func (p *Params) campaignFor(document string, ev *Event) string {
	if p.PerEventCampaign {
		return "pdf-" + strconv.Itoa(ev.HD.Year())
	}
	return document
}
