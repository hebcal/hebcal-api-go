// Package geoip is a client for the hebcal geoip2 lookup service, which
// answers HTTP requests over a unix domain socket and resolves a caller's IP
// address to approximate coordinates.
//
// The package is deliberately free of dependencies on the rest of this
// service so it can be reused (or split out) on its own.
package geoip

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"
)

// DefaultSocket is the conventional path of the geoip2 service socket.
const DefaultSocket = "/run/hebcal-geoip2/geoip2.sock"

// Point is a geographic coordinate returned by a lookup.
type Point struct {
	Latitude  float64
	Longitude float64
}

// maxAccuracyRadiusKm is the largest accuracy_radius (in kilometres) we treat
// as a usable answer. Beyond it the geoip2 result is too imprecise to be a
// meaningful location hint, so we discard it as if the service had no answer.
const maxAccuracyRadiusKm = 500

// lookupResponse is the subset of the geoip2 service's JSON we decode.
type lookupResponse struct {
	Location struct {
		Latitude       float64 `json:"latitude"`
		Longitude      float64 `json:"longitude"`
		AccuracyRadius int     `json:"accuracy_radius"`
	} `json:"location"`
}

// Client performs lookups against the geoip2 unix domain socket. The zero
// value is not usable; construct one with New.
type Client struct {
	socketPath string
	httpClient *http.Client
}

// New returns a Client that talks to the geoip2 service listening on
// socketPath. Idle connections are kept so repeated lookups reuse one socket.
func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{Transport: transport, Timeout: 200 * time.Millisecond},
	}
}

// DialSocket verifies connectivity to the GeoIP unix domain socket by opening
// and immediately closing a connection. It returns any dial error (whose string
// carries the underlying errno reason, e.g. "no such file or directory").
func (c *Client) DialSocket(ctx context.Context) error {
	if c == nil || c.socketPath == "" {
		return errors.New("missing geoip socket path")
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return err
	}
	return conn.Close()
}

// LookupPoint resolves an IP address to approximate coordinates. A nil Client,
// an unconfigured socket, or an empty ip all yield an error rather than a
// panic, so callers can treat the location hint as best-effort.
func (c *Client) LookupPoint(ctx context.Context, ip string) (*Point, error) {
	if c == nil || c.socketPath == "" || ip == "" {
		return nil, errors.New("missing geoip client or ip")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/lookup?ip="+url.QueryEscape(ip), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("geoip lookup did not return coordinates")
	}
	var out lookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Location.Latitude == 0 && out.Location.Longitude == 0 {
		return nil, errors.New("geoip lookup missing coordinates")
	}
	if out.Location.AccuracyRadius > maxAccuracyRadiusKm {
		return nil, errors.New("geoip lookup accuracy_radius too coarse")
	}
	return &Point{Latitude: out.Location.Latitude, Longitude: out.Location.Longitude}, nil
}
