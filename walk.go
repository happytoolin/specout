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
	if s == nil {
		return
	}
	if seen[s] {
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

// normalizeOneOf turns a {kind,data} declaration into a oneOf of complete
// envelope objects. Each arm constrains kind and data together, so ordinary
// JSON Schema validation rejects a discriminator/payload mismatch.
func normalizeOneOf(s *jsonschema.Schema, sr *schemaRegistry, owner string) {
	var nodes []*jsonschema.Schema
	walkSchema(s, func(x *jsonschema.Schema) { nodes = append(nodes, x) })
	for _, x := range nodes {
		makeUnionEnvelope(x, sr, owner)
	}
}

func makeUnionEnvelope(s *jsonschema.Schema, sr *schemaRegistry, owner string) {
	if s.Properties == nil || len(s.OneOf) != 0 {
		return
	}
	disc := findVariantProperty(s)
	if disc == "" {
		return
	}
	data := unionDataProperty(s)
	if data == nil {
		return
	}
	names := unionVariantNames(data.OneOf)
	if len(names) == 0 {
		return
	}
	allowNull := hasSchemaType(s, nullType)
	// nullable stores a type array in Extras. Branches are object schemas;
	// leaving that array in their copies would emit two type members.
	delete(s.Extras, "type")
	mapping := newObj()
	refs := make([]*jsonschema.Schema, 0, len(names))
	for i, name := range names {
		t, ok := sr.variants[name]
		if !ok {
			panic("specout: union variant " + name + " is not registered")
		}
		variantRef := sr.refFor(t)
		branchName := sr.unionBranchName(owner, i+1)
		branchRef := "#/components/schemas/" + branchName
		sr.addUnionBranch(branchName, s, disc, name, variantRef)
		refs = append(refs, &jsonschema.Schema{Ref: branchRef})
		mapping.set(name, branchRef)
	}
	s.Type = ""
	s.Properties = nil
	s.AdditionalProperties = nil
	s.PatternProperties = nil
	s.PropertyNames = nil
	s.MinProperties = nil
	s.MaxProperties = nil
	s.Required = nil
	s.OneOf = refs
	if allowNull {
		nullable(s)
	}
	if s.Extras == nil {
		s.Extras = map[string]any{}
	}
	s.Extras["discriminator"] = map[string]any{"propertyName": disc, "mapping": mapping}
}

func unionDataProperty(s *jsonschema.Schema) *jsonschema.Schema {
	p, _ := s.Properties.Get("data")
	if p != nil && len(unionVariantNames(p.OneOf)) > 0 {
		return p
	}
	return nil
}

func unionVariantNames(oneOf []*jsonschema.Schema) []string {
	for _, sub := range oneOf {
		if sub != nil && strings.Contains(sub.Type, "|") {
			return slices.Collect(strings.SplitSeq(sub.Type, "|"))
		}
	}
	return nil
}

func (sr *schemaRegistry) addUnionBranch(name string, source *jsonschema.Schema, discriminator, value, dataRef string) {
	if sr.byName[name] != nil || sr.owned[name] != nil {
		panic("specout: duplicate component name " + name)
	}
	branch := *source
	branch.OneOf = nil
	branch.Extras = maps.Clone(source.Extras)
	properties := jsonschema.NewProperties()
	if source.Properties != nil {
		for key := range source.Properties.KeysFromOldest() {
			properties.Set(key, source.Properties.Value(key))
		}
	}
	if disc, ok := properties.Get(discriminator); ok {
		discriminatorSchema := *disc
		discriminatorSchema.Enum = nil
		discriminatorSchema.Const = value
		properties.Set(discriminator, &discriminatorSchema)
	} else {
		properties.Set(discriminator, &jsonschema.Schema{Type: "string", Const: value})
	}
	properties.Set("data", &jsonschema.Schema{Ref: dataRef})
	branch.Type = "object"
	branch.Properties = properties
	branch.Required = slices.Clone(source.Required)
	if !slices.Contains(branch.Required, discriminator) {
		branch.Required = append(branch.Required, discriminator)
	}
	if !slices.Contains(branch.Required, "data") {
		branch.Required = append(branch.Required, "data")
	}
	if sr.closed {
		branch.AdditionalProperties = jsonschema.FalseSchema
	}
	sr.byName[name] = &branch
	sr.nameOrder = append(sr.nameOrder, name)
}

func (sr *schemaRegistry) unionBranchName(owner string, index int) string {
	base := owner + "Variant" + strconv.Itoa(index)
	name := base
	for suffix := 2; sr.byName[name] != nil || sr.owned[name] != nil; suffix++ {
		name = base + strconv.Itoa(suffix)
	}
	return name
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
