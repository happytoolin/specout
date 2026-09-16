package specout

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/invopop/jsonschema"
)

// pathParamRe matches one route param, with or without a regex constraint.
// A constraint may itself carry a {n} quantifier, so braces are excluded from
// the constraint body and a balanced pair is matched explicitly; otherwise
// {id:[0-9]{4}} would stop at the inner } and leave a stray brace behind.
// ponytail: one nesting level; a two-deep regex stays unnormalized rather
// than mangled. Swap in a brace scanner if such a pattern ever appears.
var pathParamRe = regexp.MustCompile(`\{([^}:]+)(:[^{}]*(?:\{[^{}]*\}[^{}]*)*)?\}`)

// pathParams extracts {name} placeholders from a route pattern.
func pathParams(pattern string) []string {
	var out []string
	for _, m := range pathParamRe.FindAllStringSubmatch(pattern, -1) {
		out = append(out, m[1])
	}
	return out
}

// taggedParams reflects Req fields carrying a query/header/cookie tag into
// OpenAPI parameters.
func taggedParams(req reflect.Type, sr *schemaRegistry) []any {
	if req == nil {
		return nil
	}
	for req.Kind() == reflect.Pointer {
		req = req.Elem()
	}
	if req.Kind() != reflect.Struct {
		return nil
	}
	var params []any
	for i := 0; i < req.NumField(); i++ {
		f := req.Field(i)
		if f.PkgPath != "" {
			continue
		}
		loc, qt := paramTag(f)
		// not a parameter: it is a body field (or a path: field, which only
		// types an already-declared path parameter and never becomes one here).
		if !isParamField(f) {
			continue
		}
		if loc == "path" {
			continue
		}
		name := parts0(qt)
		// reflect a one-field wrapper so the field's jsonschema tag applies
		fs := reflectParam(f)
		if f.Type.Kind() == reflect.Pointer {
			base := fs.Type
			fs.Type = ""
			fs.Extras = map[string]any{"type": []string{base, "null"}}
		}
		splitEnums(fs)
		p := newObj().
			set("name", name).
			set("in", loc).
			set("required", !strings.Contains(qt, ",omitempty") && !strings.Contains(f.Tag.Get("json"), "omitempty") && tagValue(f.Tag.Get("jsonschema"), "default") == "").
			set("schema", fs)
		if desc := tagValue(f.Tag.Get("jsonschema"), "description"); desc != "" {
			fs.Description = ""
			p.set("description", desc)
		}
		params = append(params, p)
	}
	return params
}

// paramTag returns the parameter location and tag value of a Req field:
// query, header, cookie or path.
func paramTag(f reflect.StructField) (loc, value string) {
	for _, l := range []string{"query", "header", "cookie", "path"} {
		if t := f.Tag.Get(l); t != "" {
			return l, t
		}
	}
	return "", ""
}

// isParamField reports whether an exported Req field is an OpenAPI parameter.
// A path: field is one: it types the {name} placeholder and must not appear in
// the body either.
func isParamField(f reflect.StructField) bool {
	if f.PkgPath != "" {
		return false
	}
	loc, value := paramTag(f)
	if loc == "" {
		return false
	}
	// query:"-" (or header:"-") opts out: the field is a plain body field.
	return loc == "path" || parts0(value) != "-"
}

// hasParamField reports whether t carries any parameter field.
func hasParamField(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		if isParamField(t.Field(i)) {
			return true
		}
	}
	return false
}

// bodyView is the request-body view of a Req type that mixes parameters and
// body fields: the synthetic body-only struct, and the component name it is
// registered under.
type bodyView struct {
	t    reflect.Type
	name string
}

// bodyType returns the type whose schema is Req's request body plus the
// component name to register it under, or (nil, "") when Req carries no body.
//
// Req is path+query+body together, so a $ref to the whole struct would list a
// path or query parameter as a body property: reify the struct without its
// parameter fields instead. Any other Req is itself the body — []User is a
// JSON array body, and File is an octet-stream payload.
//
// The synthetic type is memoized per Req: StructOf must yield exactly one type
// per Req, or two operations sharing a Req type would each register a
// component under the same name.
func (sr *schemaRegistry) bodyType(req reflect.Type) (reflect.Type, string) {
	if req == nil {
		return nil, ""
	}
	for req.Kind() == reflect.Pointer {
		req = req.Elem()
	}
	// an empty struct carries no body, but a bare File is an octet-stream
	// payload: it is a marker type, not a body schema.
	if req == reflect.TypeFor[File]() {
		return req, ""
	}
	if req.Kind() != reflect.Struct {
		return req, ""
	}
	if req.NumField() == 0 {
		return nil, ""
	}
	if !hasParamField(req) {
		return req, ""
	}
	if v, ok := sr.bodyViews[req]; ok {
		return v.t, v.name
	}
	var fields []reflect.StructField
	for i := 0; i < req.NumField(); i++ {
		if f := req.Field(i); !isParamField(f) {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		// every field is a parameter: the operation has no body at all
		return nil, ""
	}
	v := bodyView{t: reflect.StructOf(fields), name: sr.nameFor(req)}
	sr.bodyViews[req] = v
	return v.t, v.name
}

// reflectParam builds a synthetic struct with one field shaped like f, so
// invopop applies the field's jsonschema tag, then returns the property.
func propSchema(t reflect.Type, tag reflect.StructTag) *jsonschema.Schema {
	wrap := reflect.StructOf([]reflect.StructField{{Name: "Wrap", Type: t, Tag: tag}})
	r := &jsonschema.Reflector{Anonymous: true, DoNotReference: true}
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
	return p
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

// reflectParam is propSchema for the field's own type and tag.
func reflectParam(f reflect.StructField) *jsonschema.Schema {
	return propSchema(f.Type, f.Tag)
}

func parts0(s string) string {
	if p := strings.Split(s, ","); p[0] != "" {
		return p[0]
	}
	return s
}

// tagValue extracts key=value from a jsonschema tag string.
func tagValue(tag, key string) string {
	for _, part := range strings.Split(tag, ",") {
		if strings.HasPrefix(part, key+"=") {
			return strings.TrimPrefix(part, key+"=")
		}
	}
	return ""
}

// pathField finds the Req field tagged path:"name".
func pathField(req reflect.Type, name string) (reflect.StructField, bool) {
	for _, f := range pathFields(req) {
		if parts0(f.Tag.Get("path")) == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// pathFields returns the exported Req fields tagged path:"...".
func pathFields(req reflect.Type) []reflect.StructField {
	if req == nil {
		return nil
	}
	for req.Kind() == reflect.Pointer {
		req = req.Elem()
	}
	if req.Kind() != reflect.Struct {
		return nil
	}
	var out []reflect.StructField
	for i := 0; i < req.NumField(); i++ {
		f := req.Field(i)
		if f.PkgPath == "" && parts0(f.Tag.Get("path")) != "" {
			out = append(out, f)
		}
	}
	return out
}

// strayPathField returns a path:"name" tag that the pattern does not carry as
// a {name} placeholder. The field is dropped from the body and the
// placeholder falls back to a plain string parameter, so a typo here would
// silently document the wrong thing.
func strayPathField(req reflect.Type, pattern string) (string, bool) {
	have := make(map[string]bool)
	for _, n := range pathParams(pattern) {
		have[strings.TrimSuffix(n, "...")] = true
	}
	for _, f := range pathFields(req) {
		name := parts0(f.Tag.Get("path"))
		if name != "-" && !have[name] {
			return name, true
		}
	}
	return "", false
}

// pathParamObjs builds OpenAPI parameter objects for {name} placeholders.
// A Req field tagged path:"name" types the parameter (petId is integer, not
// string); without one the parameter is a plain string.
func pathParamObjs(pattern string, req reflect.Type) []any {
	var out []any
	seen := make(map[string]bool)
	for _, name := range pathParams(pattern) {
		// one object per name: /a/{id}/b/{id} is a legal route template but
		// OpenAPI forbids two parameters with one name in one operation.
		if seen[name] {
			continue
		}
		seen[name] = true
		var schema any = newObj().set("type", "string")
		param := newObj().
			set("name", name).
			set("in", "path").
			set("required", true)
		if f, ok := pathField(req, name); ok {
			if s := reflectParam(f); s != nil {
				// the description belongs on the parameter object, not inside
				// its schema: that is where every published spec puts it.
				if desc := tagValue(f.Tag.Get("jsonschema"), "description"); desc != "" {
					s.Description = ""
					param.set("description", desc)
				}
				schema = s
			}
		}
		out = append(out, param.set("schema", schema))
	}
	return out
}

// requestBodyFor picks the request content type for a body type: a bare File
// is an octet-stream payload, a struct with a binary field is
// multipart/form-data, anything else is JSON.
func requestBodyFor(body reflect.Type, sr *schemaRegistry) (string, *obj) {
	if body == reflect.TypeFor[File]() {
		return "application/octet-stream", newObj().set("schema", binarySchema())
	}
	schema := newObj().set("schema", newObj().set("$ref", sr.refFor(body)))
	if hasBinaryField(body) {
		return "multipart/form-data", schema
	}
	return "application/json", schema
}

// binarySchema is the schema of a raw byte payload.
func binarySchema() *obj {
	return newObj().set("type", "string").set("format", "binary")
}

// hasBinaryField reports whether any field is a File marker or tagged format=binary.
func hasBinaryField(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	fileT := reflect.TypeOf(File{})
	if t == fileT {
		return true
	}
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i).Type
		if ft == fileT || (ft.Kind() == reflect.Slice && ft.Elem() == fileT) {
			return true
		}
		if tagValue(t.Field(i).Tag.Get("jsonschema"), "format") == "binary" {
			return true
		}
	}
	return false
}
