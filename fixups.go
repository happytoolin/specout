package specout

import (
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
)

// nullable makes a property schema accept null: plain types widen to
// [T, "null"]; refs become anyOf: [{$ref}, {type: null}].
func nullable(p *jsonschema.Schema) {
	if p == nil {
		return
	}
	if p.Ref != "" {
		p.OneOf = []*jsonschema.Schema{{Ref: p.Ref}, {Type: "null"}}
		p.Ref = ""
		return
	}
	base := p.Type
	p.Type = ""
	if p.Extras == nil {
		p.Extras = map[string]any{}
	}
	p.Extras["type"] = []string{base, "null"}
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
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && parts0(f.Tag.Get("json")) == "" {
			// invopop flattens an untagged embedded struct into this schema:
			// its fields are properties of s itself, not of a nested object.
			if ft := deref(f.Type); ft.Kind() == reflect.Struct {
				// a by-value embedding cycle is impossible in Go, so this
				// descent terminates
				sr.fixStructFields(ft, s, seen)
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
				for i, r := range s.Required {
					if r == name {
						s.Required[i] = fn
					}
				}
			}
		}
		applyFormatTag(p, f.Tag)
		for _, part := range strings.Split(f.Tag.Get("jsonschema"), ",") {
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

// fieldName is a struct field's wire name: the json tag name when it has one,
// else the Go name.
func fieldName(f reflect.StructField) string {
	if jt := f.Tag.Get("json"); jt != "" {
		if parts := strings.Split(jt, ","); parts[0] != "" {
			return parts[0]
		}
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
