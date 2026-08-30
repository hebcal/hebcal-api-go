package readings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// readingTimeout bounds the /shabbatTorahReading call, which serves one MCP
// torah-portion request and must not hold it open.
const readingTimeout = 3 * time.Second

// ShabbatReading is the part of readings-svc's /shabbatTorahReading response
// the MCP torah-portion tool uses: the reading's English name and its summary.
type ShabbatReading struct {
	// Name is name.en -- the parsha ("Shemot") or, on a chag, the holiday
	// reading label ("Pesach Shabbat Chol ha-Moed").
	Name string
	// Summary is the merged Torah-reading range, e.g. "Exodus 1:1-6:1" or the
	// special-Shabbat form "Leviticus 1:1-5:26; Deuteronomy 25:17-19".
	Summary string
}

// readingResponse decodes the two fields the tool needs from the full
// getLeyningForParshaHaShavua() (or, on a chag, getLeyningForHoliday()) object
// /shabbatTorahReading returns; the rest (fullkriyah, haftara, ...) is ignored.
type readingResponse struct {
	Name struct {
		En string `json:"en"`
	} `json:"name"`
	Summary string `json:"summary"`
}

// ShabbatTorahReading returns the reading for the Shabbat whose parsha is read
// on dateISO (a YYYY-MM-DD Gregorian date), or -- when a chag displaces the
// parsha -- the holiday's own reading. Both name and summary are @hebcal/leyning
// output that hebcal-go cannot produce in-process (the summary is
// makeSummaryFromParts(), deliberately absent from the classic-API leyning
// object /leyning and /shabbat return), which is why it is a sidecar call.
//
// It is its own request rather than the (date,il)-keyed Leyning() LRU, which
// must stay summary-free so /shabbat matches production.
func (c *Client) ShabbatTorahReading(ctx context.Context, dateISO string, il bool) (ShabbatReading, error) {
	q := url.Values{}
	q.Set("date", dateISO)
	if il {
		q.Set("i", "on")
	}
	ctx, cancel := context.WithTimeout(ctx, readingTimeout)
	defer cancel()
	data, err := c.fetch(ctx, "/shabbatTorahReading", q)
	if err != nil {
		return ShabbatReading{}, err
	}
	var body readingResponse
	if err := json.Unmarshal(data, &body); err != nil {
		return ShabbatReading{}, fmt.Errorf("readings: decoding /shabbatTorahReading: %w", err)
	}
	return ShabbatReading{Name: body.Name.En, Summary: body.Summary}, nil
}
