package specout

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"

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

// setIf sets k to v unless v is empty: every optional spec field is a string
// that is omitted rather than emitted empty.
func (o *obj) setIf(k, v string) *obj {
	if v != "" {
		o.set(k, v)
	}
	return o
}

// get returns the value at k, or nil when the key is absent.
func (o *obj) get(k string) any { return o.vals[k] }

func (o *obj) has(k string) bool {
	_, ok := o.vals[k]
	return ok
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
		return fmt.Errorf("specout: encode object: %w", err)
	}
	for _, k := range o.keys {
		if err := enc.WriteToken(jsontext.String(k)); err != nil {
			return fmt.Errorf("specout: encode key %q: %w", k, err)
		}
		if err := json.MarshalEncode(enc, o.vals[k], json.Deterministic(true)); err != nil {
			return fmt.Errorf("specout: encode value of %q: %w", k, err)
		}
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return fmt.Errorf("specout: close object: %w", err)
	}
	return nil
}
