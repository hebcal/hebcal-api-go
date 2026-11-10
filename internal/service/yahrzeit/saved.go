package yahrzeit

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/hebcal/hebcal-api-go/pkg/icalendar"
)

// SavedPrefix is the URL prefix of a saved calendar: /v3/<id>/<name>.ics.
const SavedPrefix = "/v3/"

// savedIDLen is the length of a saved calendar's id, a ULID.
const savedIDLen = 26

// maxExpandedID bounds the entry numbers expandRow fills in one by one. A
// saved calendar has a handful of entries; the bound only keeps a corrupt row
// with an entry numbered in the millions from costing a loop that long.
const maxExpandedID = 1000

// SavedID returns the calendar id a /v3/ path names: the 26 characters after
// the prefix. ok is false when they are not all letters and digits, so no
// such calendar can exist.
func SavedID(path string) (id string, ok bool) {
	if !strings.HasPrefix(path, SavedPrefix) || len(path) < len(SavedPrefix)+savedIDLen {
		return "", false
	}
	id = path[len(SavedPrefix) : len(SavedPrefix)+savedIDLen]
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return "", false
		}
	}
	return id, true
}

// NewSavedRequest builds the request for a saved calendar: the parameters
// stored in its row (contents, a JSON object), overridden by the URL's own
// query string -- so ?yrem=0 or ?dl=1 applies to a saved calendar as to any
// other. path is the request path and rawQuery its query string, undecoded.
// updated is when the row was last saved; together with the contents it
// versions the calendar for the cache validator.
func NewSavedRequest(path, rawQuery, id string, contents []byte, updated time.Time) (*Request, error) {
	q, err := queryFromJSON(contents)
	if err != nil {
		return nil, err
	}
	expandRow(q)
	q.Set("v", "yahrzeit")
	url := ParseQuery(rawQuery)
	for _, key := range url.Keys() {
		q.Set(key, url.Get(key))
	}
	// The id is the calendar's identity, whatever the URL says.
	q.Set("ulid", id)
	return &Request{
		Path:     path,
		RawQuery: rawQuery,
		Query:    q,
		ID:       id,
		Version:  updated.UTC().Format(time.RFC3339Nano) + "/" + icalendar.Murmur32Hex(string(contents)),
	}, nil
}

// errNotObject is returned for a row whose contents are not a JSON object.
var errNotObject = errors.New("saved calendar is not a JSON object")

// queryFromJSON reads a saved calendar's JSON object into a Query, keeping
// the order its keys come in, which names the calendar. A number becomes its
// decimal string and a boolean "true" or "false"; an array stands for its
// first element, as a repeated URL parameter does; null and nested objects
// are dropped. Numeric sunset flags are remembered so expandRow can spell
// them out.
func queryFromJSON(contents []byte) (*Query, error) {
	dec := json.NewDecoder(bytes.NewReader(contents))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errNotObject
	}
	q := &Query{vals: make(map[string]string)}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		val, numeric, ok := jsonScalar(raw)
		if !ok {
			continue
		}
		if numeric && isNumKey(key, 's') {
			// The compact form stores after-sunset as 0 or 1.
			if val == "0" || val == "false" {
				val = "off"
			} else {
				val = "on"
			}
		}
		q.Set(key, val)
	}
	return q, nil
}

// jsonScalar renders one JSON value as a query parameter value. numeric is
// true for a number or a boolean.
func jsonScalar(raw json.RawMessage) (val string, numeric, ok bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false, false
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false, false
		}
		return s, false, true
	case '[':
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil || len(arr) == 0 {
			return "", false, false
		}
		return jsonScalar(arr[0])
	case '{', 'n':
		return "", false, false
	case 't', 'f':
		return string(raw), true, true
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return "", false, false
	}
	if f, err := n.Float64(); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64), true, true
	}
	return n.String(), true, true
}

// expandRow turns a saved row's compact entries back into the form a URL
// carries: x<N> "1978-12-08" becomes y<N>/m<N>/d<N> without leading zeros,
// and t<N> is spelled out ("y" becomes "Yahrzeit") for every entry number up
// to the highest -- including numbers with no entry, which then count as
// Yahrzeits when the calendar's set of types is worked out.
func expandRow(q *Query) {
	ids := entryIDs(q)
	if len(ids) == 0 {
		return
	}
	maxID := ids[len(ids)-1]
	if maxID > maxExpandedID {
		maxID = maxExpandedID
		var kept []int
		for _, id := range ids {
			if id <= maxID {
				kept = append(kept, id)
			}
		}
		ids = kept
	}
	for id := 1; id <= maxID; id++ {
		n := strconv.Itoa(id)
		if x := q.Get("x" + n); len(x) == 10 {
			q.Set("y"+n, x[0:4])
			q.Set("m"+n, strings.TrimPrefix(x[5:7], "0"))
			q.Set("d"+n, strings.TrimPrefix(x[8:10], "0"))
			q.Delete("x" + n)
		}
		q.Set("t"+n, anniversaryType(q.Get("t"+n)))
	}
}
