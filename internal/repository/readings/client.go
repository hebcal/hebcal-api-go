// Package readings is the client for readings-svc, the small Node.js sidecar
// that supplies the two things hebcal-go cannot produce in-process:
//
//   - /leyning — Torah readings for Shabbat and holidays, including the
//     triennial cycle, from @hebcal/leyning and @hebcal/triennial. These back
//     the /shabbat API.
//   - /learning — the daily-learning series with no Go schedule (Sefer
//     HaMitzvot, Kitzur Shulchan Arukh, Arukh HaShulchan, Amud HaYomi,
//     Chofetz Chaim and Shemirat HaLashon), which back the PDF calendars.
//
// The sidecar answers HTTP over a unix domain socket, so both endpoints are
// local calls with no Varnish, DNS or TCP in the way. It replaces the two
// separate hebcal-web dependencies this service used to carry: an HTTP call to
// /leyning?cfg=json for readings and one out through the www.hebcal.com front
// door to /hebcal?cfg=json for daily learning.
//
// Both endpoints answer in @hebcal/rest-api's "classic API" shape — the same
// objects hebcal-web's own /shabbat and /hebcal responses are built from — so
// an item's "leyning" needs no reformatting here: it is passed through
// verbatim, key order included. That is what formatLeyningResult() in
// @hebcal/rest-api produces, and what this package used to reimplement.
package readings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/hebcal/hebcal-api-go/internal/reqlog"
)

// DefaultSocket is the conventional path of the readings-svc socket.
const DefaultSocket = "/run/hebcal/readings-svc.sock"

// requestTimeout bounds a single request. The sidecar is local and answers in
// single-digit milliseconds; this is a backstop, and a generous one because a
// PDF calendar can ask for several years of daily learning at once.
const requestTimeout = 10 * time.Second

// leyningTimeout is the tighter bound for /leyning, which serves one week of a
// user-facing API request and must not hold it open.
const leyningTimeout = 3 * time.Second

// ErrNoSocket reports that no socket path was configured.
var ErrNoSocket = errors.New("readings: no service socket configured")

// Item is one entry of a classic-API response. Only the fields either caller
// reads are decoded; Leyning is kept as raw JSON so its key order survives
// into the /shabbat response unchanged.
type Item struct {
	Title string `json:"title"`
	// TitleOrig is present only when the rendered title differs from the
	// event's untranslated description, exactly as eventToClassicApiObject
	// sets it.
	TitleOrig string `json:"title_orig"`
	Date      string `json:"date"`
	Category  string `json:"category"`
	Hebrew    string `json:"hebrew"`
	Link      string `json:"link"`
	// Leyning is the formatted reading, or nil when the event has none.
	Leyning json.RawMessage `json:"leyning"`
}

// Desc returns the event's untranslated description, which is what both
// callers match events on: title_orig when the renderer changed the title,
// and the title itself otherwise.
func (it *Item) Desc() string {
	if it.TitleOrig != "" {
		return it.TitleOrig
	}
	return it.Title
}

// apiResponse is the classic-API envelope both endpoints return.
type apiResponse struct {
	Items []Item `json:"items"`
}

// Client talks to readings-svc over its unix domain socket.
type Client struct {
	socketPath string
	httpClient *http.Client
	cache      *leyningCache
}

// New returns a Client for the sidecar listening on socketPath. Idle
// connections are kept so repeated requests reuse one socket.
func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{Transport: transport, Timeout: requestTimeout},
		cache:      newLeyningCache(),
	}
}

// DialSocket verifies connectivity by opening and immediately closing a
// connection, so an operator sees at startup whether the sidecar is running.
func (c *Client) DialSocket(ctx context.Context) error {
	if c == nil || c.socketPath == "" {
		return ErrNoSocket
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return err
	}
	return conn.Close()
}

// fetch performs one GET against the sidecar, records the round trip for the
// access log, and returns the raw response body. The host in the URL is a
// placeholder: the transport dials the socket whatever it says.
func (c *Client) fetch(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if c == nil || c.socketPath == "" {
		return nil, ErrNoSocket
	}
	// The host is a placeholder; the transport dials the socket whatever it
	// says. reqURI is what gets logged, so it drops the fake host.
	reqURI := path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+reqURI, nil)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("readings: %s: %w", path, err)
	}
	defer resp.Body.Close()
	// Read the whole body so the recorded length is the real byte count; the
	// classic-API envelope is small enough that buffering it is free.
	data, err := io.ReadAll(resp.Body)
	// Record this round trip so the access-log middleware can fold it into the
	// request's log line as a nested "subreq" object. A no-op when the context
	// carries no collector (a background call, or a test).
	reqlog.FromContext(ctx).Add(reqlog.Call{
		Status:   resp.StatusCode,
		URL:      reqURI,
		Duration: time.Since(start),
		Length:   len(data),
	})
	if err != nil {
		return nil, fmt.Errorf("readings: reading %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("readings: %s returned %s", path, resp.Status)
	}
	return data, nil
}

// get performs one GET against the sidecar and decodes the classic-API
// envelope.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]Item, error) {
	data, err := c.fetch(ctx, path, q)
	if err != nil {
		return nil, err
	}
	var body apiResponse
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("readings: decoding %s: %w", path, err)
	}
	return body.Items, nil
}
