package specout

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
)

// normalizeUnions finishes reflection before generating union branches, so
// generated names cannot hide types declared later in the document.
func (sr *schemaRegistry) normalizeUnions() {
	// Reflect all reachable variants before choosing generated names. A Go
	// type used by a later route or variant must keep its component name.
	register := func(s *jsonschema.Schema) {
		walkSchema(s, func(x *jsonschema.Schema) {
			if findVariantProperty(x) == "" {
				return
			}
			_, data := unionDataProperty(x)
			if data == nil {
				return
			}
			for _, name := range unionVariantNames(data.OneOf) {
				t, ok := sr.variants[name]
				if !ok {
					panic("specout: union variant " + name + " is not registered")
				}
				sr.refFor(t)
			}
		})
	}
	for i, j, k := 0, 0, 0; i < len(sr.order) || j < len(sr.nameOrder) || k < len(sr.inline); {
		switch {
		case i < len(sr.order):
			register(sr.byType[sr.order[i]].s)
			i++
		case j < len(sr.nameOrder):
			register(sr.byName[sr.nameOrder[j]])
			j++
		default:
			register(sr.inline[k])
			k++
		}
	}
	for _, t := range sr.order {
		e := sr.byType[t]
		normalizeOneOf(e.s, sr, e.name)
	}
	for _, name := range sr.nameOrder {
		normalizeOneOf(sr.byName[name], sr, name)
	}
	for _, s := range sr.inline {
		normalizeOneOf(s, sr, "Inline")
	}
}

// normalizeOneOf turns discriminator and payload declarations into complete
// envelope objects, so validation rejects a discriminator/payload mismatch.
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
	dataName, data := unionDataProperty(s)
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
		sr.addUnionBranch(branchName, s, disc, name, dataName, variantRef)
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

func unionDataProperty(s *jsonschema.Schema) (string, *jsonschema.Schema) {
	for name := range s.Properties.KeysFromOldest() {
		p := s.Properties.Value(name)
		if p != nil && len(unionVariantNames(p.OneOf)) > 0 {
			return name, p
		}
	}
	return "", nil
}

func unionVariantNames(oneOf []*jsonschema.Schema) []string {
	for _, sub := range oneOf {
		if sub != nil && strings.Contains(sub.Type, "|") {
			var names []string
			seen := make(map[string]bool)
			for name := range strings.SplitSeq(sub.Type, "|") {
				if !seen[name] {
					names = append(names, name)
					seen[name] = true
				}
			}
			return names
		}
	}
	return nil
}

func (sr *schemaRegistry) addUnionBranch(
	name string, source *jsonschema.Schema, discriminator, value, dataName, dataRef string,
) {
	if sr.byName[name] != nil || sr.owners[name] != nil {
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
	properties.Set(dataName, &jsonschema.Schema{Ref: dataRef})
	branch.Type = "object"
	branch.Properties = properties
	branch.Required = slices.Clone(source.Required)
	if !slices.Contains(branch.Required, discriminator) {
		branch.Required = append(branch.Required, discriminator)
	}
	if !slices.Contains(branch.Required, dataName) {
		branch.Required = append(branch.Required, dataName)
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
	for suffix := 2; sr.byName[name] != nil || sr.owners[name] != nil; suffix++ {
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
