package handler

import (
	"net"
	"net/http"
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/httpx"
)

// downloadHost is the vhost that serves calendar downloads and feeds. The
// JSON APIs belong to www.hebcal.com, but Varnish sends all of
// download.hebcal.com here, so crawlers and probes reach /shabbat and
// /converter on it too.
const downloadHost = "download.hebcal.com"

// isDownloadHost reports whether r was addressed to download.hebcal.com,
// ignoring case and any port.
func isDownloadHost(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.EqualFold(strings.TrimSuffix(host, "."), downloadHost)
}

// writeUnsupportedCfg answers a JSON API request whose cfg this service does
// not implement: 501 on www.hebcal.com, but 421 Misdirected Request on
// download.hebcal.com, where the API is not offered at all and the request
// was sent to the wrong host.
func writeUnsupportedCfg(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("Content-Type", httpx.ContentTypeJSON)
	if isDownloadHost(r) {
		w.WriteHeader(http.StatusMisdirectedRequest)
		w.Write(httpx.JSONErrorBody(r.URL.Path + " is not served from " + downloadHost))
		return
	}
	w.WriteHeader(http.StatusNotImplemented)
	w.Write(httpx.JSONErrorBody(message))
}
