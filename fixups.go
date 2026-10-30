package specout

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"iter"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/invopop/jsonschema"
)

// JSON Schema type and format names that specout writes.
const (
	nullType     = "null"
	typeString   = "string"
	typeObject   = "object"
	typeInteger  = "integer"
	typeNumber   = "number"
	typeArray    = "array"
	formatBinary = "binary"
)

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
	allowNull(p)
}

// allowNull adds null to the shape p already has: its type, its $ref, or its
// list of alternatives.
func allowNull(p *jsonschema.Schema) {
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
// readonly/writeonly/deprecated) reach the property.
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
	if body := sr.bodyForRef(s.Ref); body != nil {
		s = body
	}
	// Shared components need one pass, but repeated inline types have
	// separate schema nodes and each needs its own fixes.
	if sr.fixed[s] {
		return
	}
	sr.fixed[s] = true
	// The nullable tag wraps the reflected shape in oneOf. Its non-null arm
	// still needs the same field and container fixes as an unwrapped schema.
	for child := range nonNullArms(s.OneOf, s.AnyOf, s.AllOf) {
		sr.applySchemaFixes(t, child)
	}
	sr.fixContainer(t, s)
}

// nonNullArms yields the alternatives in groups, without the {"type": "null"}
// arm that nullable adds.
func nonNullArms(groups ...[]*jsonschema.Schema) iter.Seq[*jsonschema.Schema] {
	return func(yield func(*jsonschema.Schema) bool) {
		for _, group := range groups {
			for _, child := range group {
				if child != nil && child.Type != nullType && !yield(child) {
					return
				}
			}
		}
	}
}

// fixContainer fixes what a slice, array, map or struct schema holds. A text
// type is a string on the wire and holds nothing, and a struct written as its
// embedded field has that field's schema, not its own fields.
func (sr *schemaRegistry) fixContainer(t reflect.Type, s *jsonschema.Schema) {
	if _, promoted := promotedEncoder(t); promoted || writesText(t) {
		return
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		sr.applySchemaFixes(t.Elem(), s.Items)
		if t.Elem().Kind() == reflect.Pointer {
			nullable(s.Items)
		}
	case reflect.Map:
		sr.fixMapValues(t, s)
	case reflect.Struct:
		if s.Properties != nil {
			sr.fixStructFields(t, s)
		}
	default:
	}
}

func (sr *schemaRegistry) fixMapValues(t reflect.Type, s *jsonschema.Schema) {
	fixIntegerKeys(t.Key(), s)
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

// fixIntegerKeys corrects the key pattern of a map with signed integer keys.
// The reflector places such keys in patternProperties and closes
// additionalProperties. Go also serializes negative integer keys, and writes
// a key type with a MarshalText method through that method.
func fixIntegerKeys(key reflect.Type, s *jsonschema.Schema) {
	value, ok := s.PatternProperties["^[0-9]+$"]
	switch key.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
	default:
		ok = false
	}
	if !ok {
		return
	}
	delete(s.PatternProperties, "^[0-9]+$")
	if key.Implements(textMarshalerType) {
		s.AdditionalProperties = value
		return
	}
	s.PatternProperties["^-?[0-9]+$"] = value
}

// fixStructFields applies tags only from the winning JSON field. Hidden
// embedded fields must not change the schema of the field that shadows them.
func (sr *schemaRegistry) fixStructFields(t reflect.Type, s *jsonschema.Schema) {
	if sr.closed {
		checkHiddenFields(t, t, make(map[reflect.Type]bool))
	}
	fields := jsonFields(t, false)
	removeShadowedFields(t, s, fields)
	for _, f := range fields {
		if parts0(f.Tag.Get("jsonschema")) != "-" {
			sr.fixStructField(s, f)
		}
	}
}

// checkHiddenFields panics on a field of owner, or of a struct that owner
// embeds, that jsonschema:"-" hides from the schema. JSON still writes and
// reads that field, and a closed schema rejects every payload that holds it.
func checkHiddenFields(owner, t reflect.Type, seen map[reflect.Type]bool) {
	t = deref(t)
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	for f := range t.Fields() {
		switch {
		case f.Tag.Get("json") == "-" || (f.PkgPath != "" && !f.Anonymous):
		case parts0(f.Tag.Get("jsonschema")) == "-":
			panic("specout: " + owner.String() + " field " + f.Name + ` has jsonschema:"-", but JSON still ` +
				`writes it, and ClosedSchemas rejects it; tag it json:"-", or drop ClosedSchemas`)
		case promotes(f):
			checkHiddenFields(owner, f.Type, seen)
		}
	}
}

func (sr *schemaRegistry) fixStructField(s *jsonschema.Schema, f reflect.StructField) {
	name := fieldName(f)
	p, ok := s.Properties.Get(name)
	// encoding/json excludes only the exact tag "-". The reflector
	// also drops "-,omitempty", which names a real property.
	if !ok && name == "-" {
		p, ok = sr.addDashProperty(s, f), true
	}
	if !ok || p == nil {
		return
	}
	applyFormatTag(p, f.Tag)
	applyBoolEnumTag(p, f.Type, f.Tag)
	applyNumberEnumTag(p, f.Type, f.Tag)
	applyKeywordTags(p, f.Tag)
	// a union branch puts its variant where the payload field holds any
	if strings.Contains(f.Tag.Get("jsonschema"), "oneof_type=") {
		sr.payloads[p] = f.Type
	}
	// descend before nullable() clears a $ref: the recursion needs the link
	sr.applySchemaFixes(f.Type, p)
	if f.Type.Kind() == reflect.Pointer {
		nullable(p)
	}
}

// addDashProperty adds the property of a field whose JSON name is "-". The
// reflector drops it.
func (sr *schemaRegistry) addDashProperty(s *jsonschema.Schema, f reflect.StructField) *jsonschema.Schema {
	p := sr.propSchema(f.Type, bodyJSONTag(f.Tag, "Wrap"))
	s.Properties.Set("-", p)
	if requiredField(f.Tag) {
		s.Required = append(s.Required, "-")
	}
	return p
}

// applyKeywordTags applies the bare readonly, writeonly and deprecated
// keywords, which the reflector misses.
func applyKeywordTags(p *jsonschema.Schema, tag reflect.StructTag) {
	for part := range strings.SplitSeq(tag.Get("jsonschema"), ",") {
		switch part {
		case "readonly", "readOnly=true":
			p.ReadOnly = true
		case "writeonly", "writeOnly=true":
			p.WriteOnly = true
		case keyDeprecated, "deprecated=true":
			p.Deprecated = true
		}
	}
}

// requiredField reports whether a field's tags make its property required:
// the json tag has no omitempty or omitzero, or the jsonschema tag says
// required.
func requiredField(tag reflect.StructTag) bool {
	return !optionalTag(tag.Get("json")) || slices.Contains(strings.Split(tag.Get("jsonschema"), ","), "required")
}

// The reflector accumulates required names even when a later field replaces
// an earlier one. Remove ambiguous properties and stale required entries.
func removeShadowedFields(t reflect.Type, s *jsonschema.Schema, fields []reflect.StructField) {
	selected := make(map[string]reflect.StructField, len(fields))
	for _, f := range fields {
		selected[fieldName(f)] = f
	}
	candidates := collectBodyFields(t, false)
	counts := make(map[string]int)
	for _, candidate := range candidates {
		counts[candidate.name]++
	}
	for _, candidate := range candidates {
		if counts[candidate.name] == 1 && !candidate.optional {
			continue
		}
		f, ok := selected[candidate.name]
		if !ok || parts0(f.Tag.Get("jsonschema")) == "-" {
			s.Properties.Delete(candidate.name)
		} else if requiredField(f.Tag) {
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
	if len(p.OneOf) > 0 && admitsNull(p) {
		for child := range nonNullArms(p.OneOf) {
			applyBoolEnumTag(child, t, tag)
		}
		return
	}
	if values, ok := boolEnum(tag); ok {
		p.Enum = values
	}
}

// applyNumberEnumTag applies enum=1|2 values to a number or a list of numbers.
// The reflector reads each enum value as one number, so it drops a value that
// lists several. Tags without a pipe keep the reflector's values.
func applyNumberEnumTag(p *jsonschema.Schema, t reflect.Type, tag reflect.StructTag) {
	if p == nil {
		return
	}
	t = deref(t)
	switch {
	case len(p.OneOf) > 0 && admitsNull(p):
		for child := range nonNullArms(p.OneOf) {
			applyNumberEnumTag(child, t, tag)
		}
	case p.Items != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array):
		applyNumberEnumTag(p.Items, t.Elem(), tag)
	case p.Type == typeInteger || p.Type == typeNumber:
		p.Enum = numberEnum(tag, p.Enum)
	}
}

// numberEnum reads the number values of a jsonschema tag's enum items, without
// repeats. It returns reflected unchanged when no item lists several values,
// or when a value is not a JSON number.
func numberEnum(tag reflect.StructTag, reflected []any) []any {
	var values []any
	listed := false
	for item := range strings.SplitSeq(tag.Get("jsonschema"), ",") {
		value, isEnum := strings.CutPrefix(item, "enum=")
		if !isEnum {
			continue
		}
		listed = listed || strings.Contains(value, "|")
		for part := range strings.SplitSeq(value, "|") {
			n, ok := jsonNumber(part)
			if !ok {
				return reflected
			}
			if !slices.Contains(values, any(n)) {
				values = append(values, n)
			}
		}
	}
	if !listed {
		return reflected
	}
	return values
}

// jsonNumber reports whether s is exactly one JSON number, with no space.
func jsonNumber(s string) (jsonv1.Number, bool) {
	v := jsontext.Value(s)
	return jsonv1.Number(s), v.IsValid() && v.Kind() == '0' && strings.TrimSpace(s) == s
}

// boolEnum reads the enum=true|false values of a jsonschema tag, without
// repeats. It reports false when a value is not a bool.
func boolEnum(tag reflect.StructTag) ([]any, bool) {
	var values []any
	for item := range strings.SplitSeq(tag.Get("jsonschema"), ",") {
		value, ok := strings.CutPrefix(item, "enum=")
		if !ok {
			continue
		}
		for part := range strings.SplitSeq(value, "|") {
			v, err := strconv.ParseBool(part)
			if err != nil {
				return nil, false
			}
			if !slices.Contains(values, any(v)) {
				values = append(values, v)
			}
		}
	}
	return values, true
}

// fieldName is a struct field's wire name: the json tag name when it has one,
// else the Go name.
func fieldName(f reflect.StructField) string {
	if name := parts0(f.Tag.Get("json")); name != "" {
		if parts0(f.Tag.Get("jsonschema")) != "-" && (!utf8.ValidString(name) || strings.ContainsAny(name, "'\"`\\")) {
			panic("specout: invalid JSON field name " + strconv.Quote(name))
		}
		return name
	}
	return f.Name
}

// bodyForRef resolves a component $ref to the schema it names, so the fixup
// walk can follow a nested named struct into its hoisted component.
func (sr *schemaRegistry) bodyForRef(ref string) *jsonschema.Schema {
	name, isComponent := strings.CutPrefix(ref, componentRef)
	if !isComponent {
		return nil
	}
	if s, ok := sr.byName[name]; ok {
		return s
	}
	if e := sr.byType[sr.owners[name]]; e != nil {
		return e.s
	}
	return nil
}
