// Package complete renders the results of the geographic typeahead behind the
// /complete endpoint (also reachable as /complete.php). The search itself
// lives in pkg/geodb; this package turns its results into the JSON the
// endpoint returns, with an emoji country flag appended to each one.
package complete

import (
	"strings"

	"github.com/hebcal/hebcal-api-go/internal/jsutil"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
)

// ItemToObj serializes an autocomplete result to JSON in the field order and
// shape the /complete endpoint returns, appending a country flag. ZIP and
// geoname results carry different fields (a ZIP result has no "name", for
// instance) but share the same rule for the optional ones: when latlong is
// false, latitude/longitude/timezone/elevation/population are all dropped;
// when it is true, they're included, with elevation appearing only when it
// is positive and population only when it is non-zero (geoname results) or
// unconditionally (ZIP results).
func ItemToObj(it geodb.Item, latlong bool) jsutil.OrderedObj {
	o := jsutil.OrderedObj{
		{Key: "id", Val: it.ID},
		{Key: "value", Val: it.Value},
	}
	if it.IsZip {
		o = append(o,
			jsutil.KV{Key: "admin1", Val: it.Admin1},
			jsutil.KV{Key: "asciiname", Val: it.Asciiname},
			jsutil.KV{Key: "country", Val: it.Country},
			jsutil.KV{Key: "cc", Val: it.CC},
		)
		if latlong {
			o = append(o,
				jsutil.KV{Key: "latitude", Val: it.Latitude},
				jsutil.KV{Key: "longitude", Val: it.Longitude},
				jsutil.KV{Key: "timezone", Val: it.Timezone},
			)
			if it.Elevation > 0 {
				o = append(o, jsutil.KV{Key: "elevation", Val: it.Elevation})
			}
		}
		if latlong {
			o = append(o, jsutil.KV{Key: "population", Val: it.Population})
		}
		o = append(o, jsutil.KV{Key: "geo", Val: it.Geo})
	} else {
		o = append(o,
			jsutil.KV{Key: "admin1", Val: it.Admin1},
			jsutil.KV{Key: "country", Val: it.Country},
			jsutil.KV{Key: "cc", Val: it.CC},
		)
		if latlong {
			o = append(o,
				jsutil.KV{Key: "latitude", Val: it.Latitude},
				jsutil.KV{Key: "longitude", Val: it.Longitude},
				jsutil.KV{Key: "timezone", Val: it.Timezone},
			)
			if it.Elevation > 0 {
				o = append(o, jsutil.KV{Key: "elevation", Val: it.Elevation})
			}
		}
		o = append(o, jsutil.KV{Key: "geo", Val: it.Geo})
		if latlong && it.Population != 0 {
			o = append(o, jsutil.KV{Key: "population", Val: it.Population})
		}
		if it.Name != "" {
			o = append(o, jsutil.KV{Key: "name", Val: it.Name})
		}
		if it.Asciiname != "" {
			o = append(o, jsutil.KV{Key: "asciiname", Val: it.Asciiname})
		}
	}
	if len(it.CC) == 2 {
		o = append(o, jsutil.KV{Key: "flag", Val: FlagEmoji(it.CC)})
	}
	return o
}

// FlagEmoji converts a 2-letter ISO country code to its regional-indicator
// emoji flag (e.g. "IL" -> 🇮🇱), by offsetting each letter into the Unicode
// regional-indicator-symbol block.
func FlagEmoji(cc string) string {
	cc = strings.ToUpper(cc)
	var b strings.Builder
	for _, c := range cc {
		b.WriteRune(0x1F1E6 + (c - 'A'))
	}
	return b.String()
}
