package specout

import (
	"reflect"
	"time"

	"github.com/invopop/jsonschema"
)

// isBuiltin reports whether t's schema is small enough to inline: predeclared
// scalars and containers of them. A component named after a Go type string
// ("int", "string") is junk in components.schemas, and real specs inline
// scalars (response headers, simple bodies) instead of ref-ing them.
func isBuiltin(t reflect.Type) bool {
	// time.Time reflects to {type: string, format: date-time} — a scalar in
	// every published spec. Without this it becomes a $ref to a component
	// named "Time" when it is a bare body or a header type, while the same
	// type used as a struct field inlines. One type, one shape.
	if t == reflect.TypeFor[time.Time]() {
		return true
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		// Named containers need components, including recursive types whose
		// elements can lead back to the container itself.
		return t.PkgPath() == "" && isBuiltin(t.Elem())
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		// PkgPath is empty for the predeclared types only: a named alias
		// (type Password string) keeps its own component.
		return t.PkgPath() == ""
	default:
		// structs, pointers, interfaces, channels and funcs are never inlined.
		return false
	}
}

// schemaFor returns the inline schema for a builtin type and a component
// $ref for everything else.
func schemaFor(t reflect.Type, sr *schemaRegistry) any {
	if t.Kind() == reflect.Pointer {
		return newObj().set("anyOf", []any{schemaFor(deref(t), sr), newObj().set("type", nullType)})
	}
	if !isBuiltin(t) {
		return newObj().set("$ref", sr.refFor(t))
	}
	if s := sr.propSchema(t, ""); s != nil {
		return s
	}
	return newObj().set("$ref", sr.refFor(t))
}

// propSchema builds a synthetic struct with one field shaped like f, so
// invopop applies the field's jsonschema tag, then returns the property.
func (sr *schemaRegistry) propSchema(t reflect.Type, tag reflect.StructTag) *jsonschema.Schema {
	validateAnonymousEmbeddings(t, make(map[reflect.Type]bool))
	wrap := reflect.StructOf([]reflect.StructField{{Name: "Wrap", Type: t, Tag: tag}})
	inlineRoot := true
	r := &jsonschema.Reflector{
		Anonymous: true, DoNotReference: true,
		AdditionalFields: dominantJSONFields,
		// Keep the property and ordinary arrays inline so their field tags
		// still apply. References stop recursive object and container expansion.
		Mapper: func(t reflect.Type) *jsonschema.Schema {
			if t == wrap {
				return nil
			}
			if inlineRoot {
				inlineRoot = false
				return nil
			}
			if isBuiltin(t) {
				return nil
			}
			switch t.Kind() {
			case reflect.Struct, reflect.Map:
				return &jsonschema.Schema{Ref: sr.refFor(t)}
			case reflect.Slice, reflect.Array:
				if recursiveContainer(t) {
					return &jsonschema.Schema{Ref: sr.refFor(t)}
				}
			default:
			}
			return nil
		},
	}
	s := r.Reflect(reflect.New(wrap).Interface())
	s.Version = ""
	if s.Properties == nil {
		return nil
	}
	var p *jsonschema.Schema
	for k := range s.Properties.KeysFromOldest() {
		p, _ = s.Properties.Get(k)
		break
	}
	applyFormatTag(p, tag)
	applyBoolEnumTag(p, t, tag)
	splitEnums(p)
	sr.applySchemaFixes(t, p)
	sr.inline = append(sr.inline, p)
	return p
}

// Recursive container chains have no struct boundary to stop inline expansion.
func recursiveContainer(t reflect.Type) bool {
	seen := make(map[reflect.Type]bool)
	for {
		t = deref(t)
		switch t.Kind() {
		case reflect.Array, reflect.Slice, reflect.Map:
			if seen[t] {
				return true
			}
			seen[t] = true
			t = t.Elem()
		default:
			return false
		}
	}
}

// applyFormatTag copies a format= tag onto the schema. invopop only reads
// format in its string-keyword branch, so format=int64 on an integer (every
// published spec names int32/int64) is otherwise dropped on the floor.
func applyFormatTag(p *jsonschema.Schema, tag reflect.StructTag) {
	if p == nil || p.Format != "" {
		return
	}
	if f := tagValue(tag.Get("jsonschema"), "format"); f != "" {
		p.Format = f
	}
}
