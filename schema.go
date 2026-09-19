package specout

import (
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/invopop/jsonschema"
)

// schemaRegistry reflects Go types into JSON Schema, dedupes by Go type, and
// names components.
type schemaRegistry struct {
	byType      map[reflect.Type]*schemaEntry
	order       []reflect.Type
	byName      map[string]*jsonschema.Schema // hoisted $defs with no Go type
	nameOrder   []string
	names       map[reflect.Type]string
	overrides   map[reflect.Type]string   // SchemaName[T] component-name overrides
	defOwners   map[string]reflect.Type   // component name -> type that claimed it
	variants    map[string]reflect.Type   // Register[T] union variants, by name
	anon        int                       // anonymous struct component counter
	owned       map[string]reflect.Type   // component name -> owning Go type
	bodyViews   map[reflect.Type]bodyView // Req type -> request-body-only view
	normalizing map[string]bool           // union components currently being rewritten
	closed      bool
}

type schemaEntry struct {
	name string
	s    *jsonschema.Schema
}

func newSchemaRegistry(cfg Config) *schemaRegistry {
	return &schemaRegistry{
		byType:      make(map[reflect.Type]*schemaEntry),
		byName:      make(map[string]*jsonschema.Schema),
		names:       make(map[reflect.Type]string),
		overrides:   make(map[reflect.Type]string),
		defOwners:   make(map[string]reflect.Type),
		variants:    make(map[string]reflect.Type),
		owned:       make(map[string]reflect.Type),
		bodyViews:   make(map[reflect.Type]bodyView),
		normalizing: make(map[string]bool),
		closed:      cfg.ClosedSchemas,
	}
}

// registerVariant records a union variant type under its discriminator name.
func (sr *schemaRegistry) registerVariant(t reflect.Type, name string) {
	sr.variants[name] = deref(t)
}

// overrideName forces a component name for t (SchemaName[T]).
func (sr *schemaRegistry) overrideName(t reflect.Type, name string) {
	sr.overrides[deref(t)] = name
}

// refFor reflects t, registers it as a component, returns its $ref path.
func (sr *schemaRegistry) refFor(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if e, ok := sr.byType[t]; ok {
		return "#/components/schemas/" + e.name
	}
	validateAnonymousEmbeddings(t, make(map[reflect.Type]bool))

	e := &schemaEntry{name: sr.nameFor(t)}
	if owner, clash := sr.owned[e.name]; clash && owner != t {
		panicDuplicateName(e.name, owner, t)
	}
	sr.byType[t] = e
	sr.order = append(sr.order, t)
	sr.owned[e.name] = t

	r := &jsonschema.Reflector{
		Anonymous:                 true,
		DoNotReference:            false,
		AllowAdditionalProperties: true,
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
			if t.Name() == "" && sr.overrides[t] == "" {
				return ""
			}
			name := sr.nameFor(t)
			if owner, ok := sr.defOwners[name]; ok && owner != t {
				panicDuplicateName(name, owner, t)
			}
			sr.defOwners[name] = t
			return name
		},
	}
	s := r.Reflect(reflect.New(t).Interface())
	s.Version = ""
	s = sr.unwrapDefs(t, s)
	splitEnums(s)
	e.s = s
	sr.applySchemaFixes(t, s, map[reflect.Type]bool{})
	// Build union envelopes only after field fixups. Branches copy the parent
	// fields, so nullable and tag fixes must already be present.
	sr.normalizeUnions(e.name, s)
	i := 0
	for i < len(sr.nameOrder) {
		name := sr.nameOrder[i]
		sr.normalizeUnions(name, sr.byName[name])
		i++
	}
	// closed schemas: additionalProperties: false at every object node
	if sr.closed {
		closeSchema(s)
	}

	return "#/components/schemas/" + e.name
}

func (sr *schemaRegistry) normalizeUnions(name string, s *jsonschema.Schema) {
	if sr.normalizing[name] {
		return
	}
	sr.normalizing[name] = true
	defer delete(sr.normalizing, name)
	normalizeOneOf(s, sr, name)
}

func validateAnonymousEmbeddings(t reflect.Type, checked map[reflect.Type]bool) {
	t = deref(t)
	if t == nil || checked[t] {
		return
	}
	checked[t] = true
	if t.Kind() != reflect.Struct {
		switch t.Kind() {
		case reflect.Array, reflect.Slice, reflect.Map:
			validateAnonymousEmbeddings(t.Elem(), checked)
		default:
		}
		return
	}
	checkPromotionCycle(t, map[reflect.Type]bool{t: true})
	for f := range t.Fields() {
		validateAnonymousEmbeddings(f.Type, checked)
	}
}

func checkPromotionCycle(t reflect.Type, stack map[reflect.Type]bool) {
	for f := range t.Fields() {
		if !promotes(f) {
			continue
		}
		ft := deref(f.Type)
		if stack[ft] {
			panic("specout: recursive anonymous embedding in " + ft.String())
		}
		stack[ft] = true
		checkPromotionCycle(ft, stack)
		delete(stack, ft)
	}
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
	default:
		// structs, pointers, interfaces, channels and funcs are never inlined.
		return false
	}
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
	t = deref(t)
	if n, ok := sr.overrides[t]; ok {
		return n
	}
	if n, ok := sr.names[t]; ok {
		return n
	}
	n := sanitizeName(t)
	if t.Kind() == reflect.Struct && t.Name() == "" {
		sr.anon++
		n = "Anonymous" + strconv.Itoa(sr.anon)
	}
	sr.names[t] = n
	return n
}

var (
	packageQualifier = regexp.MustCompile(`[\pL\pN_./-]+\.`)
	componentChars   = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

func sanitizeName(t reflect.Type) string {
	t = deref(t)
	// Named maps and slices keep their Go name, just like named structs.
	if t.Name() == "" {
		switch t.Kind() {
		case reflect.Slice, reflect.Array:
			return sanitizeName(t.Elem()) + "List"
		case reflect.Map:
			return sanitizeName(t.Elem()) + "Map"
		case reflect.Interface:
			if t.NumMethod() == 0 {
				return "Any"
			}
		default:
		}
	}
	// Strip every package qualifier, including nested generic arguments,
	// then remove characters OpenAPI forbids in component names.
	n := componentChars.ReplaceAllString(packageQualifier.ReplaceAllString(t.String(), ""), "")
	if n == "" {
		n = "Anonymous"
	}
	return n
}

// panicDuplicateName fails loud when two Go types claim one component name.
func panicDuplicateName(name string, owner, t reflect.Type) {
	panic("specout: duplicate component name " + name +
		" (" + owner.String() + " vs " + t.String() + "), call SchemaName to disambiguate")
}
