package specout

import (
	"encoding"
	json "encoding/json/v2"
	"maps"
	"net"
	"reflect"
	"regexp"
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
	overrides map[reflect.Type]string     // SchemaName[T] component-name overrides
	owners    map[string]reflect.Type     // component name -> type that claimed it
	variants  map[string]reflect.Type     // Register[T] union variants, by name
	anon      int                         // anonymous struct component counter
	fixed     map[*jsonschema.Schema]bool // schema nodes whose field fixes are complete
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
			return textSchema(t)
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
// MarshalText method, as a JSON string. A JSON marshaler takes precedence. A
// JSONSchema method and net.IP keep the reflector's schema.
func writesText(t reflect.Type) bool {
	return t.Kind() != reflect.Interface && t.Implements(textMarshalerType) &&
		!t.Implements(jsonMarshalerType) && !t.Implements(jsonMarshalerToType) &&
		!t.Implements(schemaMethodType) && t != reflect.TypeFor[net.IP]()
}

// textSchema is the string schema of a type that writesText reports, or nil
// for any other type. The reflector reads the Go fields of such a type, such
// as netip.Addr or a UUID array, not the string on the wire.
func textSchema(t reflect.Type) *jsonschema.Schema {
	if !writesText(t) {
		return nil
	}
	s := &jsonschema.Schema{Type: typeString}
	if extender, ok := reflect.TypeAssert[interface{ JSONSchemaExtend(s *jsonschema.Schema) }](reflect.New(t)); ok {
		extender.JSONSchemaExtend(s)
	}
	return s
}

// claimName applies the same collision check to root types and nested definitions.
func (sr *schemaRegistry) claimName(t reflect.Type) string {
	name := sr.nameFor(t)
	if owner, ok := sr.owners[name]; ok && owner != t {
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

func validateAnonymousEmbeddings(t reflect.Type, checked map[reflect.Type]bool) {
	t = deref(t)
	if t == nil || checked[t] {
		return
	}
	checked[t] = true
	switch t.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		validateAnonymousEmbeddings(t.Elem(), checked)
	case reflect.Struct:
		validateStructEmbeddings(t, checked)
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
