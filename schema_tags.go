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

var schemaTagFlags = []string{"required", "nullable", keyDeprecated, "readonly", "writeonly", "-"}

// checkSchemaTag panics on a jsonschema tag keyword that nothing reads. The
// tag parser ignores such a keyword, so a typo would remove a rule from the
// document with no message. The parser does not trim spaces, so " minimum=1"
// is such a keyword too.
func checkSchemaTag(where, tag string) {
	for _, part := range tagParts(tag) {
		key, _, hasValue := strings.Cut(part, "=")
		switch {
		case part == "":
		case strings.TrimSpace(key) != key:
			panic("specout: " + where + ": jsonschema keyword " + strconv.Quote(key) +
				" starts or ends with a space; remove the space, and write a comma inside a value as \\\\,")
		case hasValue:
			if !slices.Contains(schemaTagKeys, key) {
				panic("specout: " + where + ": jsonschema keyword " + strconv.Quote(key) + " is not known" +
					didYouMean(key, schemaTagKeys))
			}
		default:
			checkTagFlag(where, part)
		}
	}
}

// checkTagFlag panics on a jsonschema keyword with no value that is not a flag.
func checkTagFlag(where, part string) {
	switch {
	case slices.Contains(schemaTagFlags, part):
	case slices.Contains(schemaTagKeys, part):
		panic("specout: " + where + ": jsonschema keyword " + strconv.Quote(part) + " has no value; write " +
			part + "=<value>")
	default:
		panic("specout: " + where + ": jsonschema keyword " + strconv.Quote(part) +
			" is not known; it needs a value such as minLength=1, and a comma inside a value is written \\\\," +
			didYouMean(part, slices.Concat(schemaTagKeys, schemaTagFlags)))
	}
}

func fieldTagOwner(owner reflect.Type, f reflect.StructField) string {
	return owner.String() + " field " + f.Name
}
