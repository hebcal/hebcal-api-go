package jsutil

import (
	"bytes"
	"encoding/json"
)

// KV is one member of an OrderedObj.
type KV struct {
	Key string
	Val interface{}
}

// OrderedObj marshals to a JSON object preserving insertion order. Several
// responses (the zmanim ALL_TIMES ordering, the classic-API item objects, the
// /geo location) reproduce JavaScript object key order, which a Go map cannot.
type OrderedObj []KV

// MarshalJSON implements json.Marshaler.
func (o OrderedObj) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(kv.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		v, err := json.Marshal(kv.Val)
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Marshal marshals without HTML escaping and without a trailing newline,
// matching JSON.stringify.
func Marshal(v interface{}) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	b := buf.Bytes()
	return bytes.TrimSuffix(b, []byte{'\n'})
}
