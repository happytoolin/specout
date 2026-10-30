package specout

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Keywords of a jsonschema tag that specout also reads or writes by name.
const (
	keyDescription = "description"
	keyDeprecated  = "deprecated"
)

// schemaTagKeys are the key=value keywords of a jsonschema tag: the ones that
// invopop/jsonschema reads, then the parameter keywords that specout reads.
var schemaTagKeys = []string{
	"title", keyDescription, "type", "anchor", "oneof_required", "anyof_required",
	"oneof_ref", "oneof_type", "anyof_ref", "anyof_type",
	"minLength", "maxLength", "pattern", "format", "readOnly", "writeOnly",
	"default", "example", "enum", "multipleOf", "minimum", "maximum",
	"exclusiveMaximum", "exclusiveMinimum", "minItems", "maxItems", "uniqueItems",
	"style", "explode", "allowEmptyValue", "allowReserved", keyDeprecated,
}

// schemaTagFlags are the jsonschema tag keywords that take no value.
var schemaTagFlags = []string{"required", "nullable", keyDeprecated, "readonly", "writeonly", "-"}

// checkSchemaTag panics on a jsonschema tag keyword that nothing reads. The
// tag parser ignores such a keyword, so a typo would remove a rule from the
// document with no message.
func checkSchemaTag(where, tag string) {
	for _, part := range tagParts(tag) {
		part = strings.TrimSpace(part)
		key, _, hasValue := strings.Cut(part, "=")
		switch {
		case part == "":
		case hasValue && !slices.Contains(schemaTagKeys, key):
			panic("specout: " + where + ": jsonschema keyword " + strconv.Quote(key) + " is not known" +
				didYouMean(key, schemaTagKeys))
		case !hasValue && !slices.Contains(schemaTagFlags, part):
			panic("specout: " + where + ": jsonschema keyword " + strconv.Quote(part) +
				" is not known; it needs a value such as minLength=1, and a comma inside a value is written \\\\," +
				didYouMean(part, slices.Concat(schemaTagKeys, schemaTagFlags)))
		}
	}
}

// fieldTagOwner names a struct field in a message.
func fieldTagOwner(owner reflect.Type, f reflect.StructField) string {
	return owner.String() + " field " + f.Name
}
