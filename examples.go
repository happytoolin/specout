package specout

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"strconv"
)

// Example is one named example of a request or response body. Set Value or
// ExternalValue, not both.
type Example struct {
	Summary       string
	Description   string
	Value         any
	ExternalValue string
}

// checkExamples panics on examples that would break the document. where names
// the field in the message.
func checkExamples(where string, example any, examples map[string]Example) {
	if example != nil && len(examples) > 0 {
		panic("specout: " + where + ": sets both Example and Examples; set one")
	}
	if example != nil {
		exampleValue(where, example)
	}
	for _, name := range sortedKeys(examples) {
		ex := examples[name]
		if ex.Value != nil && ex.ExternalValue != "" {
			panic("specout: " + where + ": example " + strconv.Quote(name) + " sets both Value and ExternalValue; set one")
		}
		if ex.Value != nil {
			exampleValue(where, ex.Value)
		}
	}
}

// exampleValue encodes an example with encoding/json, the encoder that specout
// schemas describe.
func exampleValue(where string, v any) jsontext.Value {
	data, err := jsonv1.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("specout: %s: a value cannot be encoded as JSON: %v", where, err))
	}
	return jsontext.Value(data)
}

// setExamples writes one body's examples on its media type object.
func setExamples(media *obj, where string, example any, examples map[string]Example) {
	if example != nil {
		media.set("example", exampleValue(where, example))
	}
	if len(examples) == 0 {
		return
	}
	out := newObj()
	for _, name := range sortedKeys(examples) {
		ex := examples[name]
		o := newObj().setIf("summary", ex.Summary).setIf("description", ex.Description)
		if ex.Value != nil {
			o.set("value", exampleValue(where, ex.Value))
		}
		out.set(name, o.setIf("externalValue", ex.ExternalValue))
	}
	media.set("examples", out)
}
