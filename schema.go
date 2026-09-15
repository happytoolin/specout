package specout

import (
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
)

// schemaRegistry reflects Go types into JSON Schema, dedupes by Go type, and
// names components.
type schemaRegistry struct {
	byType    map[reflect.Type]*schemaEntry
	order     []reflect.Type
	overrides map[reflect.Type]string // SchemaName[T] component-name overrides
	variants  map[string]reflect.Type // Register[T] union variants, by name
	closed    bool
	dialect   Dialect
}

type schemaEntry struct {
	name string
	s    *jsonschema.Schema
}

func newSchemaRegistry(cfg Config) *schemaRegistry {
	return &schemaRegistry{
		byType:    make(map[reflect.Type]*schemaEntry),
		overrides: make(map[reflect.Type]string),
		variants:  make(map[string]reflect.Type),
		closed:    cfg.ClosedSchemas,
		dialect:   cfg.JSONDialect,
	}
}

// registerVariant records a union variant type under its discriminator name.
func (sr *schemaRegistry) registerVariant(t reflect.Type, name string) {
	sr.variants[name] = t
}

// overrideName forces a component name for t (SchemaName[T]).
func (sr *schemaRegistry) overrideName(t reflect.Type, name string) {
	sr.overrides[t] = name
}

// refFor reflects t, registers it as a component, returns its $ref path.
func (sr *schemaRegistry) refFor(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeFor[NoContent]() || t == reflect.TypeFor[Binary]() || t == reflect.TypeFor[File]() {
		return ""
	}
	if e, ok := sr.byType[t]; ok {
		return "#/components/schemas/" + e.name
	}

	r := &jsonschema.Reflector{
		Anonymous:                 true,
		DoNotReference:            true,
		AllowAdditionalProperties: !sr.closed,
	}
	s := r.Reflect(reflect.New(t).Interface())
	s.Version = ""

	// invopop's oneof_type splits on ";", our docs use "|" — normalize.
	normalizeOneOf(s, sr)
	splitEnums(s)
	applyNullable(t, s)
	applyFieldTags(t, s)
	// closed schemas: additionalProperties: false at every object node
	if sr.closed {
		closeSchema(s)
	}

	e := &schemaEntry{name: sr.nameFor(t), s: s}
	sr.byType[t] = e
	sr.order = append(sr.order, t)
	return "#/components/schemas/" + e.name
}

// nameFor picks a component name: override > generic-aware > simple.
func (sr *schemaRegistry) nameFor(t reflect.Type) string {
	if n, ok := sr.overrides[t]; ok {
		return n
	}
	return sanitizeName(t)
}

func sanitizeName(t reflect.Type) string {
	n := t.String()
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return sanitizeName(t.Elem()) + "List"
	}
	// pkg.Page[pkg.T] → PageT
	if idx := strings.Index(n, "["); idx >= 0 {
		base := lastSeg(n[:idx])
		arg := lastSeg(strings.TrimSuffix(n[idx+1:], "]"))
		return base + arg
	}
	return lastSeg(n)
}

func lastSeg(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

// normalizeOneOf rewrites oneof_type=a|b entries (our dialect) into $refs to
// registered variants, and attaches a discriminator when a Variant field is
// present.
func normalizeOneOf(s *jsonschema.Schema, sr *schemaRegistry) {
	if s == nil {
		return
	}
	// invopop parsed oneof_type=email|slack as a single OneOf entry with the
	// literal Type "email|slack"; split on | and resolve each variant name.
	var kept []*jsonschema.Schema
	for _, sub := range s.OneOf {
		if sub == nil || sub.Type == "" || !strings.Contains(sub.Type, "|") {
			kept = append(kept, sub)
			continue
		}
		for _, name := range strings.Split(sub.Type, "|") {
			if t, ok := sr.variants[name]; ok {
				kept = append(kept, &jsonschema.Schema{Ref: sr.refFor(t)})
			}
		}
	}
	s.OneOf = kept
	// recurse into nested schemas so field-level oneof resolves too
	if s.Properties != nil {
		for key := range s.Properties.KeysFromOldest() {
			normalizeOneOf(s.Properties.Value(key), sr)
		}
	}
	if s.Items != nil {
		normalizeOneOf(s.Items, sr)
	}
	// discriminator: when a specout.Variant field sits next to a oneOf field,
	// attach propertyName + mapping per the api-reference §10.
	if s.Properties != nil && s.Extras == nil {
		for key := range s.Properties.KeysFromOldest() {
			p := s.Properties.Value(key)
			if p != nil && len(p.OneOf) > 0 {
				if disc := findVariantProperty(s); disc != "" {
					mapping := newObj()
					for _, sub := range p.OneOf {
						if sub.Ref != "" {
							name := strings.TrimPrefix(sub.Ref, "#/components/schemas/")
							// variant name = registered key; match by type name
							for vn, vt := range sr.variants {
								if sanitizeName(vt) == name {
									mapping.set(vn, name)
								}
							}
						}
					}
					p.Extras = map[string]any{
						"discriminator": map[string]any{
							"propertyName": disc,
							"mapping":      mapping,
						},
					}
				}
			}
		}
	}
}

// findVariantProperty returns the property name of the specout.Variant field.
func findVariantProperty(s *jsonschema.Schema) string {
	if s.Properties == nil {
		return ""
	}
	for key := range s.Properties.KeysFromOldest() {
		p := s.Properties.Value(key)
		if p != nil && p.Enum != nil && p.Description == "Discriminator" {
			return key
		}
	}
	return ""
}

// closeSchema sets additionalProperties: false recursively.
func closeSchema(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if s.Type == "object" {
		s.AdditionalProperties = jsonschema.FalseSchema
	}
	for _, d := range s.Definitions {
		closeSchema(d)
	}
	if s.Properties != nil {
		for key := range s.Properties.KeysFromOldest() {
			closeSchema(s.Properties.Value(key))
		}
	}
	if s.Items != nil {
		closeSchema(s.Items)
	}
	for _, sub := range s.OneOf {
		closeSchema(sub)
	}
	for _, sub := range s.AnyOf {
		closeSchema(sub)
	}
}

// splitEnums rewrites invopop's single-string enum values (a|b|c) into
// proper arrays; invopop splits on commas only.
func splitEnums(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Enum) == 1 {
		if str, ok := s.Enum[0].(string); ok && strings.Contains(str, "|") {
			parts := strings.Split(str, "|")
			vals := make([]any, len(parts))
			for i, p := range parts {
				vals[i] = p
			}
			s.Enum = vals
		}
	}
	if s.Properties != nil {
		for key := range s.Properties.KeysFromOldest() {
			splitEnums(s.Properties.Value(key))
		}
	}
	if s.Items != nil {
		splitEnums(s.Items)
	}
}

// applyNullable rewrites pointer fields to OpenAPI 3.1 type arrays
// ([T, "null"]). invopop only understands an explicit 'nullable' tag and
// emits oneOf; the api-reference table wants pointer ⇒ nullable, always.
func applyNullable(t reflect.Type, s *jsonschema.Schema) {
	if t.Kind() != reflect.Struct || s.Properties == nil {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type.Kind() != reflect.Pointer || f.PkgPath != "" {
			continue
		}
		name := f.Name
		if jt := f.Tag.Get("json"); jt != "" {
			if parts := strings.Split(jt, ","); parts[0] != "" {
				name = parts[0]
			}
		}
		if p, ok := s.Properties.Get(name); ok && p != nil {
			base := p.Type
			p.Type = ""
			if p.Extras == nil {
				p.Extras = map[string]any{}
			}
			p.Extras["type"] = []string{base, "null"}
		}
	}
}

// applyFieldTags handles keywords invopop misses: bare readonly/writeonly
// (it wants readOnly=true), often combined with example= in one tag.
func applyFieldTags(t reflect.Type, s *jsonschema.Schema) {
	if t.Kind() != reflect.Struct || s.Properties == nil {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name := f.Name
		if jt := f.Tag.Get("json"); jt != "" {
			if parts := strings.Split(jt, ","); parts[0] != "" {
				name = parts[0]
			}
		}
		p, ok := s.Properties.Get(name)
		if !ok || p == nil {
			continue
		}
		for _, part := range strings.Split(f.Tag.Get("jsonschema"), ",") {
			switch part {
			case "readonly", "readOnly=true":
				p.ReadOnly = true
			case "writeonly", "writeOnly=true":
				p.WriteOnly = true
			}
		}
	}
}
