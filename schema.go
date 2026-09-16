package specout

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
)

// schemaRegistry reflects Go types into JSON Schema, dedupes by Go type, and
// names components.
type schemaRegistry struct {
	byType    map[reflect.Type]*schemaEntry
	order     []reflect.Type
	byName    map[string]*jsonschema.Schema // hoisted $defs with no Go type
	nameOrder []string
	overrides map[reflect.Type]string   // SchemaName[T] component-name overrides
	defOwners map[string]reflect.Type   // component name -> type that claimed it
	variants  map[string]reflect.Type   // Register[T] union variants, by name
	anon      int                       // anonymous struct component counter
	owned     map[string]reflect.Type   // component name -> owning Go type
	bodyViews map[reflect.Type]bodyView // Req type -> request-body-only view
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
		byName:    make(map[string]*jsonschema.Schema),
		overrides: make(map[reflect.Type]string),
		defOwners: make(map[string]reflect.Type),
		variants:  make(map[string]reflect.Type),
		owned:     make(map[string]reflect.Type),
		bodyViews: make(map[reflect.Type]bodyView),
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
	if e, ok := sr.byType[t]; ok {
		return "#/components/schemas/" + e.name
	}

	r := &jsonschema.Reflector{
		Anonymous:                 true,
		DoNotReference:            false,
		AllowAdditionalProperties: !sr.closed,
		// The File marker is a raw payload, not an object: map it to the
		// binary string schema before invopop turns it into a $ref and an
		// empty "File" component.
		Mapper: func(t reflect.Type) *jsonschema.Schema {
			if t == reflect.TypeFor[File]() {
				return &jsonschema.Schema{Type: "string", Format: "binary"}
			}
			return nil
		},
		// invopop names hoisted $defs after the bare Go type name, so a
		// SchemaName override never reached a nested-only type, and two
		// packages owning the same type name collapsed into one component
		// (the second shape silently lost, every $ref pointing at it).
		// Name defs the way the registry names components, and fail loud on
		// a real clash: a wrong schema is worse than a panic.
		Namer: func(t reflect.Type) string {
			name := t.Name()
			if n, ok := sr.overrides[t]; ok {
				name = n
			}
			if name == "" {
				return ""
			}
			if owner, ok := sr.defOwners[name]; ok && owner != t {
				panic("specout: duplicate component name " + name + " (" + owner.String() + " vs " + t.String() + "), call SchemaName to disambiguate")
			}
			sr.defOwners[name] = t
			return name
		},
	}
	s := r.Reflect(reflect.New(t).Interface())
	s.Version = ""
	s = sr.unwrapDefs(t, s)
	// invopop's oneof_type splits on ";", our docs use "|" — normalize.
	normalizeOneOf(s, sr)
	splitEnums(s)
	sr.applySchemaFixes(t, s, map[reflect.Type]bool{})
	// closed schemas: additionalProperties: false at every object node
	if sr.closed {
		closeSchema(s)
	}

	e := &schemaEntry{name: sr.nameFor(t), s: s}
	if owner, clash := sr.owned[e.name]; clash && owner != t {
		panic("specout: duplicate component name " + e.name + " (" + owner.String() + " vs " + t.String() + "), call SchemaName to disambiguate")
	}
	sr.byType[t] = e
	sr.order = append(sr.order, t)
	sr.owned[e.name] = t
	return "#/components/schemas/" + e.name
}

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
		return isBuiltin(t.Elem())
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		// PkgPath is empty for the predeclared types only: a named alias
		// (type Password string) keeps its own component.
		return t.PkgPath() == ""
	}
	return false
}

// schemaFor returns the inline schema for a builtin type and a component
// $ref for everything else.
func schemaFor(t reflect.Type, sr *schemaRegistry) any {
	if !isBuiltin(t) {
		return newObj().set("$ref", sr.refFor(t))
	}
	if s := propSchema(t, ""); s != nil {
		return s
	}
	return newObj().set("$ref", sr.refFor(t))
}

// nameFor picks a component name: override > generic-aware > simple.
func (sr *schemaRegistry) nameFor(t reflect.Type) string {
	if n, ok := sr.overrides[t]; ok {
		return n
	}
	if t.Kind() == reflect.Struct && t.Name() == "" {
		sr.anon++
		return "Anonymous" + strconv.Itoa(sr.anon)
	}
	return sanitizeName(t)
}

func sanitizeName(t reflect.Type) string {
	n := t.String()
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return sanitizeName(t.Elem()) + "List"
	case reflect.Map:
		// map[K]V carries no Go name; t.String() is "map[string]Foo", which is
		// not a legal component name. Name it after the value type.
		return sanitizeName(t.Elem()) + "Map"
	case reflect.Interface:
		// any / interface{}: "interface {}" is not a legal component name.
		if t.NumMethod() == 0 {
			return "Any"
		}
	}
	// pkg.Page[pkg.T] → PageT
	if idx := strings.Index(n, "["); idx >= 0 {
		base := lastSeg(n[:idx])
		arg := lastSeg(strings.TrimSuffix(n[idx+1:], "]"))
		return base + arg
	}
	return lastSeg(n)
}

// unwrapDefs pulls a reflected schema out of its $ref/$defs wrapper.
// The def named after t becomes the component body; sibling defs register
// as components too. All #/$defs/ refs become #/components/schemas/ so
// the emitted tree is self-contained.
func (sr *schemaRegistry) unwrapDefs(t reflect.Type, s *jsonschema.Schema) *jsonschema.Schema {
	if s == nil || len(s.Definitions) == 0 {
		return s
	}
	name := sanitizeName(t)
	if n, ok := sr.overrides[t]; ok {
		name = n
	}
	body := s
	if s.Ref != "" && strings.HasSuffix(s.Ref, "/"+name) {
		body = s.Definitions[name]
	}
	// $defs is a map; registration order feeds component order, so walk the
	// names sorted to keep the output byte-deterministic.
	defNames := make([]string, 0, len(s.Definitions))
	for defName := range s.Definitions {
		if defName != name {
			defNames = append(defNames, defName)
		}
	}
	slices.Sort(defNames)
	for _, defName := range defNames {
		if sr.defByName(defName) == nil {
			sr.addDef(defName, s.Definitions[defName])
		}
	}
	remapDefs(body)
	if body == s {
		// Every $def here was hoisted into components.schemas; a second copy
		// of the whole tree inside the body is dead weight.
		s.Definitions = nil
	}
	return body
}

// defByName finds a registered component emitted from a hoisted $def.
func (sr *schemaRegistry) defByName(name string) *jsonschema.Schema {
	return sr.byName[name]
}

// addDef registers a hoisted $def as a component directly by name; no
// Go type backs it, so it rides the same order/byType machinery keyed by
// a synthetic type.
func (sr *schemaRegistry) addDef(name string, def *jsonschema.Schema) {
	def.Version = ""
	splitEnums(def)
	normalizeOneOf(def, sr)
	if sr.closed {
		closeSchema(def)
	}
	remapDefs(def)
	if _, ok := sr.byName[name]; !ok {
		sr.byName[name] = def
		sr.nameOrder = append(sr.nameOrder, name)
	}
}

// remapDefs rewrites #/$defs/ prefixes to #/components/schemas/ on every
// schema node.
func remapDefs(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if s.Ref != "" {
		s.Ref = strings.Replace(s.Ref, "#/$defs/", "#/components/schemas/", 1)
	}
	for _, d := range s.Definitions {
		remapDefs(d)
	}
	remapDefs(s.Items)
	for _, x := range s.AllOf {
		remapDefs(x)
	}
	for _, x := range s.AnyOf {
		remapDefs(x)
	}
	for _, x := range s.OneOf {
		remapDefs(x)
	}
	for _, x := range s.PrefixItems {
		remapDefs(x)
	}
	if s.Properties != nil {
		for k := range s.Properties.KeysFromOldest() {
			v, _ := s.Properties.Get(k)
			remapDefs(v)
		}
	}
	remapDefs(s.AdditionalProperties)
	for _, x := range s.PatternProperties {
		remapDefs(x)
	}
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
	// discriminator: when an enum-tagged field sits next to a oneOf field,
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
							// variant name = registered key; match by type name.
							// sr.variants is a map, so collect the matches and
							// sort them: mapping key order must not vary
							// between processes.
							var vns []string
							for vn, vt := range sr.variants {
								if sanitizeName(vt) == name {
									vns = append(vns, vn)
								}
							}
							slices.Sort(vns)
							for _, vn := range vns {
								mapping.set(vn, name)
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

// findVariantProperty returns the property name of the discriminator field.
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
	for _, x := range s.OneOf {
		splitEnums(x)
	}
	for _, x := range s.AnyOf {
		splitEnums(x)
	}
}

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
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || s == nil {
		return
	}
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
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
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
