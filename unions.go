package specout

import (
	"maps"
	"reflect"
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
	sr.reflectAllVariants()
	sr.eachSchema(func(owner string, s *jsonschema.Schema) { normalizeOneOf(s, sr, owner) })
	sr.eachSchema(func(_ string, s *jsonschema.Schema) { checkTypes(s) })
}

// eachSchema calls fn with each schema that the document can hold and the
// name of its owner: the typed components, then the hoisted definitions, then
// the inline schemas. It skips a definition with the name of a typed
// component, which replaces it, and the schemas that fn adds.
func (sr *schemaRegistry) eachSchema(fn func(owner string, s *jsonschema.Schema)) {
	for _, e := range sr.order {
		fn(e.name, e.s)
	}
	for _, name := range sr.nameOrder {
		if sr.byType[sr.owners[name]] == nil {
			fn(name, sr.byName[name])
		}
	}
	for _, s := range sr.inline {
		fn("Inline", s)
	}
}

// reflectAllVariants reflects the variants of every union in the registry. A
// reflected variant can add schemas, and the loop visits those too.
func (sr *schemaRegistry) reflectAllVariants() {
	for i, j, k := 0, 0, 0; i < len(sr.order) || j < len(sr.nameOrder) || k < len(sr.inline); {
		switch {
		case i < len(sr.order):
			sr.reflectVariants(sr.order[i].s)
			i++
		case j < len(sr.nameOrder):
			sr.reflectVariants(sr.byName[sr.nameOrder[j]])
			j++
		default:
			sr.reflectVariants(sr.inline[k])
			k++
		}
	}
}

// reflectVariants reflects the variants of each union in s.
func (sr *schemaRegistry) reflectVariants(s *jsonschema.Schema) {
	walkSchema(s, func(x *jsonschema.Schema) {
		if u, ok := declaredUnion(x); ok {
			for _, name := range u.names {
				sr.refFor(sr.variantType(name))
			}
		}
	})
}

// variantType is the Go type registered for a union variant name.
func (sr *schemaRegistry) variantType(name string) reflect.Type {
	t, ok := sr.variants[name]
	if !ok {
		panic("specout: union variant " + strconv.Quote(name) + ` is not registered; call doc.Register[T]("` + name + `")`)
	}
	return t
}

// unionDecl is a union that an envelope struct declares: the discriminator
// property, the payload property, and the variant names.
type unionDecl struct {
	disc, dataName string
	names          []string
}

// declaredUnion reads the union that s declares. It reports false when s
// declares none.
func declaredUnion(s *jsonschema.Schema) (unionDecl, bool) {
	disc := findVariantProperty(s)
	if disc == "" {
		return unionDecl{}, false
	}
	dataName, data := unionDataProperty(s)
	if data == nil {
		return unionDecl{}, false
	}
	names := unionVariantNames(data.OneOf)
	return unionDecl{disc: disc, dataName: dataName, names: names}, len(names) > 0
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
	u, ok := declaredUnion(s)
	if !ok {
		return
	}
	allowNull := hasSchemaType(s, nullType)
	// nullable stores a type array in Extras. Branches are object schemas;
	// leaving that array in their copies would emit two type members.
	delete(s.Extras, "type")
	refs, mapping := sr.addUnionBranches(s, owner, u)
	clearObjectKeywords(s)
	s.OneOf = refs
	if allowNull {
		nullable(s)
	}
	if s.Extras == nil {
		s.Extras = map[string]any{}
	}
	s.Extras["discriminator"] = map[string]any{"propertyName": u.disc, "mapping": mapping}
}

// addUnionBranches adds one branch component for each variant of u. It
// returns the oneOf refs to the branches and the discriminator mapping.
func (sr *schemaRegistry) addUnionBranches(
	s *jsonschema.Schema, owner string, u unionDecl,
) ([]*jsonschema.Schema, *obj) {
	mapping := newObj()
	refs := make([]*jsonschema.Schema, 0, len(u.names))
	for i, name := range u.names {
		// refFor can claim names, so it runs before the branch name is chosen
		dataRef := sr.refFor(sr.variantType(name))
		branch := unionBranch{name: sr.unionBranchName(owner, i+1), value: name, dataRef: dataRef}
		sr.addUnionBranch(s, u, branch)
		branchRef := componentRef + branch.name
		refs = append(refs, &jsonschema.Schema{Ref: branchRef})
		mapping.set(name, branchRef)
	}
	return refs, mapping
}

// clearObjectKeywords removes the object keywords of a union envelope. Its
// branches carry them now.
func clearObjectKeywords(s *jsonschema.Schema) {
	s.Type = ""
	s.Properties = nil
	s.AdditionalProperties = nil
	s.PatternProperties = nil
	s.PropertyNames = nil
	s.MinProperties = nil
	s.MaxProperties = nil
	s.Required = nil
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

// unionVariantNames reads the variant names of a oneof_type tag. The names
// are in the type of one oneOf arm, separated by pipes. A union with one
// variant has one arm, and its type is no JSON Schema type.
func unionVariantNames(oneOf []*jsonschema.Schema) []string {
	arms := slices.Collect(nonNullArms(oneOf))
	for _, sub := range arms {
		if strings.Contains(sub.Type, "|") || len(arms) == 1 && sub.Type != "" && !jsonSchemaTypes[sub.Type] {
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

// unionBranch is one generated branch of a union envelope: its component
// name, its discriminator value, and the $ref of its payload.
type unionBranch struct {
	name, value, dataRef string
}

func (sr *schemaRegistry) addUnionBranch(source *jsonschema.Schema, u unionDecl, b unionBranch) {
	if sr.byName[b.name] != nil || sr.owners[b.name] != nil {
		panic("specout: duplicate component name " + strconv.Quote(b.name) + "; call doc.SchemaName[T] with another name")
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
	if disc, ok := properties.Get(u.disc); ok {
		discriminatorSchema := *disc
		discriminatorSchema.Enum = nil
		discriminatorSchema.Const = b.value
		properties.Set(u.disc, &discriminatorSchema)
	} else {
		properties.Set(u.disc, &jsonschema.Schema{Type: typeString, Const: b.value})
	}
	// The payload keeps the optionality and nullability of its field: Go
	// omits it with omitempty and writes a nil pointer as null.
	payload, _ := properties.Get(u.dataName)
	data := sr.payloadSchema(payload, b.dataRef)
	if admitsNull(payload) {
		nullable(data)
	}
	properties.Set(u.dataName, data)
	branch.Type = typeObject
	branch.Properties = properties
	branch.Required = slices.Clone(source.Required)
	if !slices.Contains(branch.Required, u.disc) {
		branch.Required = append(branch.Required, u.disc)
	}
	if sr.closed {
		branch.AdditionalProperties = jsonschema.FalseSchema
	}
	sr.byName[b.name] = &branch
	sr.nameOrder = append(sr.nameOrder, b.name)
}

// payloadSchema is the payload of one union branch: the variant ref where the
// payload field holds any, inside the field's lists and maps. A []any payload
// is a list of variants.
func (sr *schemaRegistry) payloadSchema(payload *jsonschema.Schema, ref string) *jsonschema.Schema {
	t, ok := sr.payloads[payload]
	if !ok || payload == nil {
		return &jsonschema.Schema{Ref: ref}
	}
	s := variantShape(deref(t), ref)
	if s.Type == typeArray {
		// the reflector bounds a fixed-size array
		s.MinItems, s.MaxItems = payload.MinItems, payload.MaxItems
	}
	return s
}

// variantShape is the schema of t with the variant ref in place of its any.
// A payload that holds no any, such as json.RawMessage, is the variant.
func variantShape(t reflect.Type, ref string) *jsonschema.Schema {
	if !holdsVariants(t) {
		return &jsonschema.Schema{Ref: ref}
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return &jsonschema.Schema{Type: typeArray, Items: variantElem(t.Elem(), ref)}
	case reflect.Map:
		return &jsonschema.Schema{Type: typeObject, AdditionalProperties: variantElem(t.Elem(), ref)}
	default:
		return &jsonschema.Schema{Ref: ref}
	}
}

// holdsVariants reports whether t is a list or map whose elements are an
// interface, directly or in nested lists and maps.
func holdsVariants(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		elem := deref(t.Elem())
		return elem.Kind() == reflect.Interface || holdsVariants(elem)
	default:
		return false
	}
}

// variantElem is the schema of a list or map element of type t. Go writes a
// nil pointer element as null.
func variantElem(t reflect.Type, ref string) *jsonschema.Schema {
	s := variantShape(deref(t), ref)
	if t.Kind() == reflect.Pointer {
		nullable(s)
	}
	return s
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
