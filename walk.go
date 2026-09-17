package specout

import (
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
)

// walkSchema visits s and every nested schema depth-first, parent before
// child, so fn can rewrite a node and its children still get visited. It is
// the one traversal definition; the fixups below are its callbacks.
func walkSchema(s *jsonschema.Schema, fn func(*jsonschema.Schema)) {
	if s == nil {
		return
	}
	fn(s)
	for _, sub := range s.Definitions {
		walkSchema(sub, fn)
	}
	walkSchema(s.Items, fn)
	for _, group := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf, s.PrefixItems} {
		for _, sub := range group {
			walkSchema(sub, fn)
		}
	}
	if s.Properties != nil {
		for key := range s.Properties.KeysFromOldest() {
			walkSchema(s.Properties.Value(key), fn)
		}
	}
	walkSchema(s.AdditionalProperties, fn)
	for _, sub := range s.PatternProperties {
		walkSchema(sub, fn)
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
		if x.Type == "object" {
			x.AdditionalProperties = jsonschema.FalseSchema
		}
	})
}

// splitEnums rewrites invopop's single-string enum values (a|b|c) into
// proper arrays; invopop splits on commas only.
func splitEnums(s *jsonschema.Schema) {
	walkSchema(s, func(x *jsonschema.Schema) {
		if len(x.Enum) != 1 {
			return
		}
		str, ok := x.Enum[0].(string)
		if !ok || !strings.Contains(str, "|") {
			return
		}
		parts := strings.Split(str, "|")
		vals := make([]any, len(parts))
		for i, p := range parts {
			vals[i] = p
		}
		x.Enum = vals
	})
}

// normalizeOneOf rewrites oneof_type=a|b entries (our dialect) into $refs to
// registered variants, then attaches a discriminator where a Variant field
// sits next to the oneOf (api-reference §10).
func normalizeOneOf(s *jsonschema.Schema, sr *schemaRegistry) {
	// two passes: resolve first, because the discriminator reads the rewritten
	// oneOf of each property.
	walkSchema(s, func(x *jsonschema.Schema) { x.OneOf = variantRefs(x.OneOf, sr) })
	walkSchema(s, func(x *jsonschema.Schema) { attachDiscriminator(x, sr) })
}

// variantRefs replaces oneof_type=a|b placeholders with $refs to the
// registered variants; anything else is kept as-is.
func variantRefs(oneOf []*jsonschema.Schema, sr *schemaRegistry) []*jsonschema.Schema {
	var kept []*jsonschema.Schema
	for _, sub := range oneOf {
		// invopop parsed oneof_type=email|slack as one entry with the literal
		// Type "email|slack"; split on | and resolve each variant name.
		if sub == nil || !strings.Contains(sub.Type, "|") {
			kept = append(kept, sub)
			continue
		}
		for _, name := range strings.Split(sub.Type, "|") {
			if t, ok := sr.variants[name]; ok {
				kept = append(kept, &jsonschema.Schema{Ref: sr.refFor(t)})
			}
		}
	}
	return kept
}

// attachDiscriminator sets propertyName + mapping on each oneOf property of s
// when an enum-tagged Discriminator field sits next to it.
func attachDiscriminator(s *jsonschema.Schema, sr *schemaRegistry) {
	if s.Properties == nil || s.Extras != nil {
		return
	}
	disc := findVariantProperty(s)
	if disc == "" {
		return
	}
	for key := range s.Properties.KeysFromOldest() {
		p := s.Properties.Value(key)
		if p == nil || len(p.OneOf) == 0 {
			continue
		}
		mapping := newObj()
		for _, sub := range p.OneOf {
			if sub.Ref == "" {
				continue
			}
			name := strings.TrimPrefix(sub.Ref, "#/components/schemas/")
			// mapping key order must not vary between processes: sr.variants is
			// a map, so collect the matches and sort them.
			var names []string
			for vn, vt := range sr.variants {
				if sanitizeName(vt) == name {
					names = append(names, vn)
				}
			}
			slices.Sort(names)
			for _, vn := range names {
				mapping.set(vn, name)
			}
		}
		p.Extras = map[string]any{
			"discriminator": map[string]any{"propertyName": disc, "mapping": mapping},
		}
	}
}

// findVariantProperty returns the property name of the discriminator field.
func findVariantProperty(s *jsonschema.Schema) string {
	if s.Properties == nil {
		return ""
	}
	for key := range s.Properties.KeysFromOldest() {
		if p := s.Properties.Value(key); p != nil && p.Enum != nil && p.Description == "Discriminator" {
			return key
		}
	}
	return ""
}
