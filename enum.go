package specout

import (
	"slices"
	"strconv"

	"github.com/invopop/jsonschema"
)

// Schema is the JSON Schema type that specout reads. A type with a method
// JSONSchema() *specout.Schema gets the schema that the method returns, so its
// MarshalJSON shape, or a shared enum, needs no other import.
type Schema = jsonschema.Schema

// Enum returns a string schema that allows only values. Use it in the
// JSONSchema method of a named string type, so that each field, parameter and
// body of that type shares one list:
//
//	type Status string
//
//	func (Status) JSONSchema() *specout.Schema { return specout.Enum("todo", "doing", "done") }
func Enum[T ~string](values ...T) *Schema {
	if len(values) == 0 {
		panic("specout: Enum needs at least one value")
	}
	enum := make([]any, 0, len(values))
	for i, v := range values {
		if slices.Contains(values[:i], v) {
			panic("specout: Enum: value " + strconv.Quote(string(v)) + " is repeated; list each value once")
		}
		enum = append(enum, string(v))
	}
	return &Schema{Type: typeString, Enum: enum}
}
