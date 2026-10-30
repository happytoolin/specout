package specout

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
)

// walkSchema visits s and every nested schema depth-first, parent before
// child, so fn can rewrite a node and its children still get visited. It is
// the one traversal definition; the fixups below are its callbacks.
func walkSchema(s *jsonschema.Schema, fn func(*jsonschema.Schema)) {
	walkSchemaSeen(s, fn, make(map[*jsonschema.Schema]bool))
}

func walkSchemaSeen(s *jsonschema.Schema, fn func(*jsonschema.Schema), seen map[*jsonschema.Schema]bool) {
	if s == nil || seen[s] {
		return
	}
	seen[s] = true
	fn(s)
	eachSubschema(s, func(child *jsonschema.Schema) { walkSchemaSeen(child, fn, seen) })
}

// eachSubschema calls visit on each schema that s holds directly, in a fixed
// order. It reads each field just before that visit, so a visit sees the
// changes that earlier visits made. A visited schema can be nil.
func eachSubschema(s *jsonschema.Schema, visit func(*jsonschema.Schema)) {
	eachSorted(s.Definitions, visit)
	visit(s.Items)
	visit(s.Not)
	visit(s.If)
	visit(s.Then)
	visit(s.Else)
	visit(s.Contains)
	visit(s.PropertyNames)
	visit(s.ContentSchema)
	eachSorted(s.DependentSchemas, visit)
	for _, group := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf, s.PrefixItems} {
		for _, sub := range group {
			visit(sub)
		}
	}
	if s.Properties != nil {
		for key := range s.Properties.KeysFromOldest() {
			visit(s.Properties.Value(key))
		}
	}
	visit(s.AdditionalProperties)
	eachSorted(s.PatternProperties, visit)
}

// eachSorted calls visit on the values of m, in key order.
func eachSorted(m map[string]*jsonschema.Schema, visit func(*jsonschema.Schema)) {
	for _, key := range slices.Sorted(maps.Keys(m)) {
		visit(m[key])
	}
}

// remapDefs rewrites #/$defs/ prefixes to #/components/schemas/ on every
// schema node, so the emitted tree is self-contained.
func remapDefs(s *jsonschema.Schema) {
	walkSchema(s, func(x *jsonschema.Schema) {
		if x.Ref != "" {
			x.Ref = strings.Replace(x.Ref, "#/$defs/", componentRef, 1)
		}
	})
}

// jsonSchemaTypes are the type names that JSON Schema defines.
var jsonSchemaTypes = map[string]bool{
	nullType: true, "boolean": true, typeObject: true, typeArray: true,
	typeNumber: true, typeString: true, typeInteger: true,
}

// checkTypes panics on a type name that JSON Schema does not define. A
// oneof_type tag outside a union envelope leaves its variant names there, and
// the document is then invalid.
func checkTypes(s *jsonschema.Schema) {
	walkSchema(s, func(x *jsonschema.Schema) {
		names := []string{x.Type}
		if types, ok := x.Extras["type"].([]string); ok {
			names = types
		}
		for _, name := range names {
			if name != "" && !jsonSchemaTypes[name] {
				panic("specout: schema type " + strconv.Quote(name) + " is not a JSON Schema type; " +
					"a oneof_type union separates its variants with | and needs a Discriminator field")
			}
		}
	})
}

// closeSchema sets additionalProperties: false on every object node.
func closeSchema(s *jsonschema.Schema) {
	walkSchema(s, func(x *jsonschema.Schema) {
		if hasSchemaType(x, typeObject) && x.AdditionalProperties == nil {
			x.AdditionalProperties = jsonschema.FalseSchema
		}
	})
}

// splitEnums expands pipe-separated values, including repeated enum tags.
func splitEnums(s *jsonschema.Schema) {
	walkSchema(s, func(x *jsonschema.Schema) {
		if !slices.ContainsFunc(x.Enum, func(value any) bool {
			str, ok := value.(string)
			return ok && strings.Contains(str, "|")
		}) {
			return
		}
		var values []any
		seen := make(map[string]bool)
		for _, value := range x.Enum {
			str, ok := value.(string)
			if !ok {
				values = append(values, value)
				continue
			}
			for part := range strings.SplitSeq(str, "|") {
				if !seen[part] {
					values = append(values, part)
					seen[part] = true
				}
			}
		}
		x.Enum = values
	})
}
