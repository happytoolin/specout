package specout

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
)

const nullType = "null"

// nullable makes a property schema accept null. It is safe to call more than
// once because shared and recursive components can be reached from several
// reflection roots.
func nullable(p *jsonschema.Schema) {
	if p == nil || admitsNull(p) {
		return
	}
	if p.Const != nil {
		base := *p
		*p = jsonschema.Schema{AnyOf: []*jsonschema.Schema{&base, {Type: nullType}}}
		return
	}
	if len(p.Enum) > 0 {
		p.Enum = append(p.Enum, nil)
	}
	if p.Type != "" {
		if p.Extras == nil {
			p.Extras = map[string]any{}
		}
		p.Extras["type"] = []string{p.Type, nullType}
		p.Type = ""
		return
	}
	if p.Ref != "" {
		p.AnyOf = []*jsonschema.Schema{{Ref: p.Ref}, {Type: nullType}}
		p.Ref = ""
		return
	}
	if len(p.AnyOf) > 0 {
		p.AnyOf = append(p.AnyOf, &jsonschema.Schema{Type: nullType})
		return
	}
	if len(p.OneOf) > 0 {
		p.OneOf = append(p.OneOf, &jsonschema.Schema{Type: nullType})
		return
	}
	if len(p.AllOf) > 0 {
		allOf := p.AllOf
		p.AllOf = nil
		p.AnyOf = []*jsonschema.Schema{{AllOf: allOf}, {Type: nullType}}
		return
	}
	if len(p.Enum) > 0 {
		return
	}
	// An unconstrained schema already accepts null. This is the schema for
	// *any and must stay {}, not ["", "null"].
}

func admitsNull(s *jsonschema.Schema) bool {
	if s == nil {
		return false
	}
	if hasSchemaType(s, nullType) {
		return true
	}
	if slices.Contains(s.Enum, any(nil)) {
		return true
	}
	return slices.ContainsFunc(s.AnyOf, admitsNull) || slices.ContainsFunc(s.OneOf, admitsNull)
}

func hasSchemaType(s *jsonschema.Schema, name string) bool {
	if types, ok := s.Extras["type"].([]string); ok && slices.Contains(types, name) {
		return true
	}
	if types, ok := s.Extras["type"].([]any); ok && slices.Contains(types, any(name)) {
		return true
	}
	return s.Type == name
}

// applySchemaFixes runs the post-reflection fixups over the whole type graph:
// pointer fields become nullable, and the tag keywords invopop misses (bare
// readonly/writeonly/deprecated, form= renames) reach the property.
//
// invopop hoists every named struct into components.schemas before this runs,
// so the walk follows $refs into those components. Without it only the root
// type is fixed up: a nested struct keeps invopop's raw output, no
// nullability and no readonly/deprecated tags.
func (sr *schemaRegistry) applySchemaFixes(t reflect.Type, s *jsonschema.Schema) {
	if t == nil || s == nil {
		return
	}
	t = deref(t)
	if s.Ref != "" {
		if body := sr.bodyForRef(s.Ref); body != nil {
			s = body
		}
	}
	// Shared components need one pass, but repeated inline types have
	// separate schema nodes and each needs its own fixes.
	if sr.fixed[s] {
		return
	}
	sr.fixed[s] = true
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		sr.applySchemaFixes(t.Elem(), s.Items)
		if t.Elem().Kind() == reflect.Pointer {
			nullable(s.Items)
		}
		return
	case reflect.Map:
		sr.fixMapValues(t, s)
		return
	case reflect.Struct:
	default:
		return
	}
	if s.Properties == nil {
		return
	}
	sr.fixStructFields(t, s)
}

func (sr *schemaRegistry) fixMapValues(t reflect.Type, s *jsonschema.Schema) {
	// The reflector places signed integer keys in patternProperties and
	// closes additionalProperties. Go also serializes negative integer keys.
	if value, ok := s.PatternProperties["^[0-9]+$"]; ok {
		switch t.Key().Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			delete(s.PatternProperties, "^[0-9]+$")
			s.PatternProperties["^-?[0-9]+$"] = value
		default:
		}
	}
	fix := func(value *jsonschema.Schema) {
		sr.applySchemaFixes(t.Elem(), value)
		if t.Elem().Kind() == reflect.Pointer {
			nullable(value)
		}
	}
	if len(s.PatternProperties) > 0 {
		for _, value := range s.PatternProperties {
			fix(value)
		}
		return
	}
	if s.AdditionalProperties == nil {
		s.AdditionalProperties = jsonschema.TrueSchema
	}
	fix(s.AdditionalProperties)
}

// fixStructFields applies tags only from the winning JSON field. Hidden
// embedded fields must not change the schema of the field that shadows them.
func (sr *schemaRegistry) fixStructFields(t reflect.Type, s *jsonschema.Schema) {
	fields := jsonFields(t, false)
	removeShadowedFields(t, s, fields)
	for _, f := range fields {
		name := fieldName(f)
		p, ok := s.Properties.Get(name)
		if !ok || p == nil {
			continue
		}
		applyFormatTag(p, f.Tag)
		applyBoolEnumTag(p, f.Type, f.Tag)
		for part := range strings.SplitSeq(f.Tag.Get("jsonschema"), ",") {
			switch part {
			case "readonly", "readOnly=true":
				p.ReadOnly = true
			case "writeonly", "writeOnly=true":
				p.WriteOnly = true
			case "deprecated", "deprecated=true":
				p.Deprecated = true
			}
		}
		// descend before nullable() clears a $ref: the recursion needs the link
		sr.applySchemaFixes(f.Type, p)
		if f.Type.Kind() == reflect.Pointer {
			nullable(p)
		}
	}
	renameFormFields(s, fields)
}

// Rename together so one field's destination cannot replace another field
// before it is read. Required names must use the same mapping.
func renameFormFields(s *jsonschema.Schema, fields []reflect.StructField) {
	renames := make(map[string]string)
	for _, f := range fields {
		name, form := fieldName(f), parts0(f.Tag.Get("form"))
		if form != "" && form != "-" && form != name {
			renames[name] = form
		}
	}
	if len(renames) == 0 {
		return
	}
	properties := jsonschema.NewProperties()
	for name := range s.Properties.KeysFromOldest() {
		wireName := name
		if form, ok := renames[name]; ok {
			wireName = form
		}
		if _, duplicate := properties.Get(wireName); duplicate {
			panic("specout: duplicate form field " + wireName)
		}
		properties.Set(wireName, s.Properties.Value(name))
	}
	s.Properties = properties
	for i, name := range s.Required {
		if form, ok := renames[name]; ok {
			s.Required[i] = form
		}
	}
}

// The reflector accumulates required names even when a later field replaces
// an earlier one. Remove ambiguous properties and stale required entries.
func removeShadowedFields(t reflect.Type, s *jsonschema.Schema, fields []reflect.StructField) {
	selected := make(map[string]reflect.StructField, len(fields))
	for _, f := range fields {
		selected[fieldName(f)] = f
	}
	var candidates []bodyFieldCandidate
	collectBodyFields(t, 0, &candidates, make(map[reflect.Type]bool), false)
	counts := make(map[string]int)
	for _, candidate := range candidates {
		counts[candidate.name]++
	}
	for _, candidate := range candidates {
		if counts[candidate.name] == 1 {
			continue
		}
		f, ok := selected[candidate.name]
		if !ok || parts0(f.Tag.Get("jsonschema")) == "-" {
			s.Properties.Delete(candidate.name)
		} else if !optionalTag(f.Tag.Get("json")) ||
			slices.Contains(strings.Split(f.Tag.Get("jsonschema"), ","), "required") {
			continue
		}
		s.Required = slices.DeleteFunc(s.Required, func(name string) bool { return name == candidate.name })
	}
}

// applyBoolEnumTag preserves valid enum=true|false constraints. The reflector
// accepts enum tags on strings and numbers but currently drops them on bools.
func applyBoolEnumTag(p *jsonschema.Schema, t reflect.Type, tag reflect.StructTag) {
	if p == nil || deref(t).Kind() != reflect.Bool || len(p.Enum) > 0 {
		return
	}
	value := tagValue(tag.Get("jsonschema"), "enum")
	if value == "" {
		return
	}
	var values []any
	for part := range strings.SplitSeq(value, "|") {
		v, err := strconv.ParseBool(part)
		if err != nil {
			return
		}
		values = append(values, v)
	}
	p.Enum = values
}

// fieldName is a struct field's wire name: the json tag name when it has one,
// else the Go name.
func fieldName(f reflect.StructField) string {
	if name := parts0(f.Tag.Get("json")); name != "" {
		return name
	}
	return f.Name
}

// bodyForRef resolves a component $ref to the schema it names, so the fixup
// walk can follow a nested named struct into its hoisted component.
func (sr *schemaRegistry) bodyForRef(ref string) *jsonschema.Schema {
	name, ok := strings.CutPrefix(ref, "#/components/schemas/")
	if !ok {
		return nil
	}
	if s, ok := sr.byName[name]; ok {
		return s
	}
	for _, e := range sr.byType {
		if e.name == name {
			return e.s
		}
	}
	return nil
}
