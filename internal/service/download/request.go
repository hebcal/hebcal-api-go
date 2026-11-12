package download

import (
	"context"
	"net/url"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/reqlog"
	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// UnsupportedSeriesError reports daily-learning series this build cannot
// generate. The two cases are deliberately different: with no readings-svc
// configured the calendar can never be rendered (501, retrying cannot help),
// while a configured readings-svc that did not answer is transient (503).
type UnsupportedSeriesError struct {
	// Series names the schedules that could not be produced.
	Series []string
	// Retryable is true when a readings-svc fetch failed, false when no
	// readings-svc is configured at all.
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

// Decode turns a download URL into the Download message it carries. Three
// URL shapes name the same request:
//
//	/v4/<base64-protobuf>/<name><ext>           the current form
//	/v2/h/<base64-querystring>/<name><ext>      the legacy form (see v2.go)
//	/hebcal/index.cgi/<name><ext>?<query>       the classic form (see cgi.go)
//	/export/[<dir>/...]<name><ext>?<query>      its older /export/ spelling
//
// ext is the file extension the caller serves (".pdf" or ".csv"); a URL with
// another extension, or of any other shape, is a *NotFoundError. From here on
// the three are the same request.
//
// The /v4/ protobuf and the /v2/h/ query string both hide the calendar's
// options behind base64, so Decode records the query string the message
// stands for in the request log, under its "qs" key. The classic form carries
// its query in the logged URL already.
func Decode(ctx context.Context, u *url.URL, ext string) (*downloadpb.Download, error) {
	msg, _, err := decodeWithQuery(ctx, u, ext)
	return msg, err
}

// DecodeICS is Decode for an .ics feed, which also reads the options a query
// string can carry that the message cannot: the calendar's title, colour and
// tracking parameters.
func DecodeICS(ctx context.Context, u *url.URL) (*downloadpb.Download, *ICSQuery, error) {
	msg, q, err := decodeWithQuery(ctx, u, ".ics")
	if err != nil {
		return nil, nil, err
	}
	// A feed counts minor holidays in whenever min is anything but off, where
	// the PDF and CSV need it to say on: the major holidays already bring
	// them, and only an explicit refusal takes them away. Subscriptions made
	// from the form's own placeholder ("min=...") rely on it.
	if q.on("maj") && !q.on("min") && !q.off("min") {
		msg.Minor = true
	}
	return msg, newICSQuery(q), nil
}

func decodeWithQuery(ctx context.Context, u *url.URL, ext string) (*downloadpb.Download, v2Query, error) {
	msg, q, err := decode(u, ext)
	if err != nil {
		return nil, nil, err
	}
	if isOpaquePath(u.Path) {
		reqlog.FromContext(ctx).SetQuery(MessageToQuery(msg))
	}
	return msg, q, nil
}

// isOpaquePath reports whether a download URL hides its calendar options behind
// a base64 payload, as the /v4/ protobuf and the /v2/h/ query string both do.
func isOpaquePath(path string) bool {
	return !isCGIPath(path)
}

// decode returns the message and the query string the request stands for, as
// hebcal.com reads it: the legacy forms carry theirs, and a /v4/ URL gets the
// one its message renders to with the URL's own query laid over it.
func decode(u *url.URL, ext string) (*downloadpb.Download, v2Query, error) {
	switch path := u.Path; {
	case isCGIPath(path):
		q, err := ParseCGIPath(path, u.RawQuery, ext)
		if err != nil {
			return nil, nil, NotFoundf("%s", err.Error())
		}
		msg, err := DecodeV2(q)
		return msg, q, err
	case strings.HasPrefix(path, "/v2/"):
		q, err := ParseV2Path(path, ext)
		if err != nil {
			return nil, nil, NotFoundf("%s", err.Error())
		}
		msg, err := DecodeV2(q)
		return msg, q, err
	default:
		payload, err := ParsePath(path, ext)
		if err != nil {
			return nil, nil, NotFoundf("%s", err.Error())
		}
		msg, err := DecodeMessage(payload)
		if err != nil {
			// Several crawlers (bingbot among them) fetch /v4/ URLs with the
			// path lowercased, which breaks the mixed-case base64 and the
			// protobuf it decodes to. An undecodable payload names no
			// calendar, so it is 404, the same as an unparseable path.
			return nil, nil, NotFoundf("%s", err.Error())
		}
		q := make(v2Query)
		vals, _ := url.ParseQuery(MessageToQuery(msg))
		for k, v := range vals {
			q[k] = v[len(v)-1]
		}
		for k, v := range u.Query() {
			q[k] = v[len(v)-1]
		}
		return msg, q, nil
	}
}
