package specout

import (
	"encoding"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"maps"
	"math/big"
	"net"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
)

// componentRef is the $ref prefix of a schema in components.schemas.
const componentRef = "#/components/schemas/"

// schemaRegistry reflects Go types into JSON Schema, dedupes by Go type, and
// names components.
type schemaRegistry struct {
	byType    map[reflect.Type]*schemaEntry
	order     []*schemaEntry                // byType values, in registration order
	byName    map[string]*jsonschema.Schema // hoisted $defs with no Go type
	nameOrder []string
	inline    []*jsonschema.Schema // parameter and media-specific schemas outside components
	names     map[reflect.Type]string
	overrides map[reflect.Type]string             // SchemaName[T] component-name overrides
	owners    map[string]reflect.Type             // component name -> type that claimed it
	variants  map[string]reflect.Type             // Register[T] union variants, by name
	anon      int                                 // anonymous struct component counter
	reserved  map[string]bool                     // AnonymousN names that a named type uses
	fixed     map[*jsonschema.Schema]bool         // schema nodes whose field fixes are complete
	payloads  map[*jsonschema.Schema]reflect.Type // union payload property -> its field type
	closed    bool
}

type schemaEntry struct {
	name string
	s    *jsonschema.Schema
}

// Build holds d.mu. Variants are read-only; body views add local name overrides.
func (d *Generator) newSchemaRegistry() *schemaRegistry {
	sr := &schemaRegistry{
		byType:    make(map[reflect.Type]*schemaEntry),
		byName:    make(map[string]*jsonschema.Schema),
		names:     make(map[reflect.Type]string),
		overrides: make(map[reflect.Type]string),
		owners:    make(map[string]reflect.Type),
		variants:  d.variants,
		fixed:     make(map[*jsonschema.Schema]bool),
		payloads:  make(map[*jsonschema.Schema]reflect.Type),
		closed:    d.cfg.ClosedSchemas,
	}
	maps.Copy(sr.overrides, d.nameOverrides)
	return sr
}

// refFor reflects t, registers it as a component, returns its $ref path.
func (sr *schemaRegistry) refFor(t reflect.Type) string {
	t = deref(t)
	if e, ok := sr.byType[t]; ok {
		return componentRef + e.name
	}
	validateAnonymousEmbeddings(t, make(map[reflect.Type]bool))

	e := &schemaEntry{name: sr.claimName(t)}
	sr.byType[t] = e
	sr.order = append(sr.order, e)

	r := &jsonschema.Reflector{
		Anonymous:                 true,
		DoNotReference:            false,
		AllowAdditionalProperties: true,
		// The reflector visits embedded fields in declaration order. Reapply
		// the JSON winners last so an embedding cannot overwrite a direct field.
		AdditionalFields: dominantJSONFields,
		// The File marker is a raw payload, not an object: map it to the
		// binary string schema before invopop turns it into a $ref and an
		// empty "File" component.
		Mapper: func(t reflect.Type) *jsonschema.Schema {
			if t == reflect.TypeFor[File]() {
				return binarySchema()
			}
			return sr.wireSchema(t)
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
			return sr.claimName(t)
		},
	}
	s := r.Reflect(reflect.New(t).Interface())
	s.Version = ""
	s = sr.unwrapDefs(t, s)
	splitEnums(s)
	e.s = s
	sr.applySchemaFixes(t, s)
	// closed schemas: additionalProperties: false at every object node
	if sr.closed {
		closeSchema(s)
	}

	return componentRef + e.name
}

var (
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	jsonMarshalerToType = reflect.TypeFor[json.MarshalerTo]()
	schemaMethodType    = reflect.TypeFor[interface{ JSONSchema() *jsonschema.Schema }]()
)

// writesText reports whether the JSON encoders write t through its
// MarshalText method, as a JSON string. The method can have a pointer
// receiver, as on regexp.Regexp: the encoders call it on a value behind a
// pointer or in a slice. A JSON marshaler takes precedence. A string kind, a
// JSONSchema method and net.IP keep the reflector's schema.
func writesText(t reflect.Type) bool {
	p := reflect.PointerTo(t)
	return t.Kind() != reflect.String && p.Implements(textMarshalerType) &&
		!p.Implements(jsonMarshalerType) && !p.Implements(jsonMarshalerToType) &&
		!t.Implements(schemaMethodType) && t != reflect.TypeFor[net.IP]()
}

// scalarTypes maps library types that the encoders write as one JSON scalar
// to their JSON type, so isBuiltin inlines them. *big.Int writes a number
// through MarshalJSON, *big.Float and *big.Rat write text, and json.Number is
// a Go string that the encoders write as a number.
var scalarTypes = map[reflect.Type]string{
	reflect.TypeFor[big.Int]():       typeInteger,
	reflect.TypeFor[big.Float]():     typeString,
	reflect.TypeFor[big.Rat]():       typeString,
	reflect.TypeFor[jsonv1.Number](): typeNumber,
}

// wireSchema is the schema of a type whose JSON form the reflector gets wrong,
// or nil for any other type. The reflector reads json.Number as a string, and
// it reads the Go fields of a math/big type, of a type that writesText reports, such as netip.Addr or a UUID array,
// and of a struct whose JSON encoder method comes from an embedded field, such
// as time.Time. The encoders write none of these as an object. The reflector
// panics on a uintptr, which the encoders write as an integer.
func (sr *schemaRegistry) wireSchema(t reflect.Type) *jsonschema.Schema {
	var s *jsonschema.Schema
	switch {
	case scalarTypes[t] != "":
		s = &jsonschema.Schema{Type: scalarTypes[t]}
	case writesText(t):
		s = &jsonschema.Schema{Type: typeString}
	case t.Kind() == reflect.Uintptr && !customJSON(t):
		s = &jsonschema.Schema{Type: typeInteger}
	default:
		embedded, ok := promotedEncoder(t)
		if !ok {
			return nil
		}
		// the encoders write t as its embedded field
		s = sr.fieldSchema(embedded, "")
	}
	if extender, ok := reflect.TypeAssert[interface{ JSONSchemaExtend(s *jsonschema.Schema) }](reflect.New(t)); ok {
		extender.JSONSchemaExtend(s)
	}
	return s
}

// promotedEncoder returns the type of the embedded field that gives struct t
// its JSON encoder method, as an embedded time.Time does. The encoders then
// write t as that field. It reports false when t declares the method itself,
// has none, or has a JSONSchema method.
func promotedEncoder(t reflect.Type) (reflect.Type, bool) {
	if t.Kind() != reflect.Struct || t.Implements(schemaMethodType) {
		return nil, false
	}
	for _, name := range []string{"MarshalJSON", "MarshalJSONTo"} {
		if m, ok := t.MethodByName(name); ok {
			return methodProvider(t, m)
		}
	}
	return nil, false
}

// methodProvider returns the embedded field type of t that provides method m.
// As in Go, the shallowest embedded field that declares m provides it, and
// two such fields at one depth provide nothing.
func methodProvider(t reflect.Type, m reflect.Method) (reflect.Type, bool) {
	if !promotedMethod(m) {
		return nil, false
	}
	index := shallowestDeclarer(t, m.Name)
	if index == nil {
		return nil, false
	}
	return t.Field(index[0]).Type, true
}

// shallowestDeclarer returns the index of the shallowest embedded field of t
// that declares the method name. It returns nil when no field does, or when
// two do at one depth.
func shallowestDeclarer(t reflect.Type, name string) []int {
	var index []int
	tie := false
	for _, f := range reflect.VisibleFields(t) {
		if !f.Anonymous || !declaresMethod(f.Type, name) {
			continue
		}
		switch {
		case index == nil || len(f.Index) < len(index):
			index, tie = f.Index, false
		case len(f.Index) == len(index):
			tie = true
		default:
		}
	}
	if tie {
		return nil
	}
	return index
}

// declaresMethod reports whether t declares the method name, with a value
// receiver or, when t is a pointer, a pointer receiver.
func declaresMethod(t reflect.Type, name string) bool {
	m, ok := deref(t).MethodByName(name)
	if !ok && t.Kind() == reflect.Pointer {
		m, ok = t.MethodByName(name)
	}
	return ok && !promotedMethod(m)
}

// promotedMethod reports whether m comes from an embedded field. The compiler
// builds a wrapper for a promoted method, and the runtime gives it the source
// file "<autogenerated>"; a declared method has its real source file.
func promotedMethod(m reflect.Method) bool {
	fn := runtime.FuncForPC(m.Func.Pointer())
	if fn == nil {
		return false
	}
	file, _ := fn.FileLine(fn.Entry())
	return file == "<autogenerated>"
}

// customJSON reports whether a method of t sets its JSON form or its schema.
func customJSON(t reflect.Type) bool {
	return t.Implements(textMarshalerType) || t.Implements(jsonMarshalerType) ||
		t.Implements(jsonMarshalerToType) || t.Implements(schemaMethodType)
}

// nameClash is the panic value of claimName when a named type claims a name
// that an anonymous struct holds. pathsAndSchemas recovers it and reflects
// the routes again with the name reserved.
type nameClash string

// claimName applies the same collision check to root types and nested definitions.
func (sr *schemaRegistry) claimName(t reflect.Type) string {
	name := sr.nameFor(t)
	if owner, ok := sr.owners[name]; ok && owner != t {
		// A reserved name that an anonymous struct still holds did not come
		// from nextAnonymousName, as a generated body name: another run
		// cannot free it.
		if isAnonymousStruct(owner) && !isAnonymousStruct(t) && !sr.reserved[name] {
			panic(nameClash(name))
		}
		panicDuplicateName(name, owner, t)
	}
	sr.owners[name] = t
	return name
}

func dominantJSONFields(t reflect.Type) []reflect.StructField {
	for f := range t.Fields() {
		if promotes(f) {
			return jsonFields(t, false)
		}
	}
	return nil
}

// ownSchema reports whether the schema of t comes from its JSONSchema method
// or from wireSchema. The reflector then never reads the fields of t.
func ownSchema(t reflect.Type) bool {
	_, promoted := promotedEncoder(t)
	return t.Implements(schemaMethodType) || scalarTypes[t] != "" || writesText(t) || promoted
}

func validateAnonymousEmbeddings(t reflect.Type, checked map[reflect.Type]bool) {
	t = deref(t)
	if t == nil || checked[t] {
		return
	}
	checked[t] = true
	if ownSchema(t) {
		return
	}
	switch t.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		validateAnonymousEmbeddings(t.Elem(), checked)
	case reflect.Struct:
		validateStructEmbeddings(t, checked)
	case reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		if !customJSON(t) {
			panic("specout: JSON cannot encode " + t.String() + `; tag its field json:"-"`)
		}
	default:
	}
}

func validateStructEmbeddings(t reflect.Type, checked map[reflect.Type]bool) {
	checkPromotionCycle(t, map[reflect.Type]bool{t: true})
	for f := range t.Fields() {
		if f.Tag.Get("json") == "-" || parts0(f.Tag.Get("jsonschema")) == "-" || (f.PkgPath != "" && !f.Anonymous) {
			continue
		}
		if !promotes(f) && embedOption(f.Tag) {
			panic("specout: explicit JSON embedding requires an anonymous struct field")
		}
		validateAnonymousEmbeddings(f.Type, checked)
	}
}

// embedOption reports whether a json tag has the inline or embed option.
func embedOption(tag reflect.StructTag) bool {
	options := strings.Split(tag.Get("json"), ",")[1:]
	return slices.Contains(options, "inline") || slices.Contains(options, "embed")
}

func checkPromotionCycle(t reflect.Type, stack map[reflect.Type]bool) {
	for f := range t.Fields() {
		if parts0(f.Tag.Get("jsonschema")) == "-" || !promotes(f) {
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
	if isAnonymousStruct(t) {
		n = sr.nextAnonymousName()
	}
	sr.names[t] = n
	return n
}

// isAnonymousStruct reports a struct type with no name, struct{...}.
func isAnonymousStruct(t reflect.Type) bool {
	t = deref(t)
	return t.Kind() == reflect.Struct && t.Name() == ""
}

// nextAnonymousName returns the next AnonymousN name that no type holds and
// no named type reserved.
func (sr *schemaRegistry) nextAnonymousName() string {
	for {
		sr.anon++
		n := "Anonymous" + strconv.Itoa(sr.anon)
		if _, taken := sr.owners[n]; !taken && !sr.reserved[n] {
			return n
		}
	}
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

// schemaObjs is the hoisted components.schemas object: the typed components
// in registration order, then the $defs with no Go type behind them. Empty
// when nothing was hoisted, so the caller can skip the key.
func (sr *schemaRegistry) schemaObjs() *obj {
	sr.normalizeUnions()
	if len(sr.order) == 0 {
		return newObj()
	}
	schemas := newObj()
	for _, e := range sr.order {
		schemas.set(e.name, e.s)
	}
	// hoisted $defs with no Go type: emitted after the typed components
	for _, n := range sr.nameOrder {
		if schemas.get(n) == nil {
			schemas.set(n, sr.byName[n])
		}
	}
	return schemas
}
