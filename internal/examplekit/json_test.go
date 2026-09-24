package examplekit_test

import (
	"encoding/json"
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/happytoolin/specout/internal/examplekit"
	"github.com/stretchr/testify/assert"
)

func FuzzLaunchDecodeSingleJSONValue(f *testing.F) {
	for _, input := range []string{"null", `{"a":1}`, `{"a":1}{}`, `{"a":1}junk`, "", "[1,2,", `"text"`, strings.Repeat("[", 64)} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 16*1024 {
			t.Skip("keep fuzz cases bounded")
		}
		// RawMessage checks syntax without imposing float64 limits on numbers.
		var value json.RawMessage
		err := examplekit.DecodeJSON(strings.NewReader(input), &value)
		valid := jsontext.Value(input).IsValid(jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
		assert.Equal(t, valid, err == nil, "decoding must accept exactly one complete JSON value")
	})
}
