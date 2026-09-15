package specout

import (
	"bytes"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// obj is an insertion-ordered JSON object. encoding/json/v2 does not sort
// map keys, so the spec tree must carry its own key order to stay
// byte-deterministic.
type obj struct {
	keys []string
	vals map[string]any
}

func newObj() *obj { return &obj{vals: make(map[string]any)} }

func (o *obj) set(k string, v any) *obj {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

func (o *obj) get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

// MarshalJSON writes the object with keys in insertion order.
func (o *obj) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf)
	if err := o.write(enc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (o *obj) write(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, k := range o.keys {
		if err := enc.WriteToken(jsontext.String(k)); err != nil {
			return err
		}
		if err := json.MarshalEncode(enc, o.vals[k]); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}
