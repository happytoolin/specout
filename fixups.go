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
	if types, ok := s.Extras["type"].([]string); ok && slices.Contains(types, nullType) {
		return true
	}
	if types, ok := s.Extras["type"].([]any); ok && slices.Contains(types, any(nullType)) {
		return true
	}
	if s.Type == nullType {
		return true
	}
	if slices.Contains(s.Enum, any(nil)) {
		return true
	}
	return slices.ContainsFunc(s.AnyOf, admitsNull) || slices.ContainsFunc(s.OneOf, admitsNull)
}

// applySchemaFixes runs the post-reflection fixups over the whole type graph:
// pointer fields become nullable, and the tag keywords invopop misses (bare
// readonly/writeonly/deprecated, form= renames) reach the property.
//
// invopop hoists every named struct into components.schemas before this runs,
// so the walk follows $refs into those components. Without it only the root
// type is fixed up: a nested struct keeps invopop's raw output, no
// nullability and no readonly/deprecated tags.
func (sr *schemaRegistry) applySchemaFixes(t reflect.Type, s *jsonschema.Schema, seen map[reflect.Type]bool) {
	if t == nil || s == nil {
		return
	}
	t = deref(t)
	if s.Ref != "" {
		if body := sr.bodyForRef(s.Ref); body != nil {
			s = body
		}
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		sr.applySchemaFixes(t.Elem(), s.Items, seen)
		if t.Elem().Kind() == reflect.Pointer {
			nullable(s.Items)
		}
		return
	case reflect.Map:
		if s.AdditionalProperties == nil {
			s.AdditionalProperties = jsonschema.TrueSchema
		}
		sr.applySchemaFixes(t.Elem(), s.AdditionalProperties, seen)
		if t.Elem().Kind() == reflect.Pointer {
			nullable(s.AdditionalProperties)
		}
		return
	case reflect.Struct:
	default:
		return
	}
	// a recursive type reaches itself: each component body needs one pass
	if seen[t] || s.Properties == nil {
		return
	}
	seen[t] = true
	sr.fixStructFields(t, s, seen)
}

// fixStructFields applies the per-field fixups to one struct's schema. It is
// separate from applySchemaFixes so the embedded-struct descent can run on the
// parent's schema without marking the embedded type as seen: the type still
// needs its own pass when it is also reached as a component body elsewhere.
func (sr *schemaRegistry) fixStructFields(t reflect.Type, s *jsonschema.Schema, seen map[reflect.Type]bool) {
	sr.fixStructFieldsSeen(t, s, seen, make(map[reflect.Type]bool))
}

func (sr *schemaRegistry) fixStructFieldsSeen(
	t reflect.Type,
	s *jsonschema.Schema,
	seen, embedded map[reflect.Type]bool,
) {
	t = deref(t)
	if embedded[t] {
		return
	}
	embedded[t] = true
	defer delete(embedded, t)
	for f := range t.Fields() {
		if f.Anonymous && parts0(f.Tag.Get("json")) == "" {
			// invopop flattens an untagged embedded struct into this schema:
			// its fields are properties of s itself, not of a nested object.
			if ft := deref(f.Type); ft.Kind() == reflect.Struct {
				sr.fixStructFieldsSeen(ft, s, seen, embedded)
				continue
			}
		}
		// an embedded field is promoted even when its Go name is unexported
		if f.PkgPath != "" && !f.Anonymous {
			continue
		}
		name := fieldName(f)
		p, ok := s.Properties.Get(name)
		if !ok || p == nil {
			continue
		}
		if form := f.Tag.Get("form"); form != "" {
			if fn := parts0(form); fn != "-" && fn != name {
				s.Properties.Set(fn, p)
				s.Properties.Delete(name)
				// required names the wire property too: leaving the Go field
				// name here would require a property that does not exist.
				if i := slices.Index(s.Required, name); i >= 0 {
					s.Required[i] = fn
				}
			}
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
		sr.applySchemaFixes(f.Type, p, seen)
		if f.Type.Kind() == reflect.Pointer {
			nullable(p)
		}
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
