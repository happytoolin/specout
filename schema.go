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
		if sr.byName[defName] == nil {
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

func lastSeg(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}
