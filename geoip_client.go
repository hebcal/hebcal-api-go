package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"
)

const defaultGeoIPSocket = "/run/hebcal-geoip2/geoip2.sock"

type geoIPPoint struct {
	Latitude  float64
	Longitude float64
}

type geoIPLookupResponse struct {
	Location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"location"`
}

type geoIPClient struct {
	socketPath string
	httpClient *http.Client
}

func newGeoIPClient(socketPath string) *geoIPClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
	}
	return &geoIPClient{
		socketPath: socketPath,
		httpClient: &http.Client{Transport: transport, Timeout: 200 * time.Millisecond},
	}
}

func (c *geoIPClient) lookupPoint(ctx context.Context, ip string) (*geoIPPoint, error) {
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
	var out geoIPLookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Location.Latitude == 0 && out.Location.Longitude == 0 {
		return nil, errors.New("geoip lookup missing coordinates")
	}
	return &geoIPPoint{Latitude: out.Location.Latitude, Longitude: out.Location.Longitude}, nil
}
