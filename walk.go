package specout

import (
	"maps"
	"slices"
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
	for _, key := range slices.Sorted(maps.Keys(s.Definitions)) {
		walkSchemaSeen(s.Definitions[key], fn, seen)
	}
	walkSchemaSeen(s.Items, fn, seen)
	walkSchemaSeen(s.Not, fn, seen)
	walkSchemaSeen(s.If, fn, seen)
	walkSchemaSeen(s.Then, fn, seen)
	walkSchemaSeen(s.Else, fn, seen)
	walkSchemaSeen(s.Contains, fn, seen)
	walkSchemaSeen(s.PropertyNames, fn, seen)
	walkSchemaSeen(s.ContentSchema, fn, seen)
	for _, key := range slices.Sorted(maps.Keys(s.DependentSchemas)) {
		walkSchemaSeen(s.DependentSchemas[key], fn, seen)
	}
	for _, group := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf, s.PrefixItems} {
		for _, sub := range group {
			walkSchemaSeen(sub, fn, seen)
		}
	}
	if s.Properties != nil {
		for key := range s.Properties.KeysFromOldest() {
			walkSchemaSeen(s.Properties.Value(key), fn, seen)
		}
	}
	walkSchemaSeen(s.AdditionalProperties, fn, seen)
	for _, key := range slices.Sorted(maps.Keys(s.PatternProperties)) {
		walkSchemaSeen(s.PatternProperties[key], fn, seen)
	}
}

// remapDefs rewrites #/$defs/ prefixes to #/components/schemas/ on every
// schema node, so the emitted tree is self-contained.
func remapDefs(s *jsonschema.Schema) {
	walkSchema(s, func(x *jsonschema.Schema) {
		if x.Ref != "" {
			x.Ref = strings.Replace(x.Ref, "#/$defs/", "#/components/schemas/", 1)
		}
	})
}

// closeSchema sets additionalProperties: false on every object node.
func closeSchema(s *jsonschema.Schema) {
	walkSchema(s, func(x *jsonschema.Schema) {
		if hasSchemaType(x, "object") && x.AdditionalProperties == nil {
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
