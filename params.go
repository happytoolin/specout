package specout

import (
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
)

const pathLocation = "path"

// pathParamRe matches one route param, with or without a regex constraint.
// A constraint may itself carry a {n} quantifier, so braces are excluded from
// the constraint body and a balanced pair is matched explicitly; otherwise
// {id:[0-9]{4}} would stop at the inner } and leave a stray brace behind.
// ponytail: one nesting level; a two-deep regex stays unnormalized rather
// than mangled. Swap in a brace scanner if such a pattern ever appears.
var pathParamRe = regexp.MustCompile(`{([^}:]+)(:[^{}]*(?:{[^{}]*}[^{}]*)*)?}`)

// pathParams extracts {name} placeholders from a route pattern.
func pathParams(pattern string) []string {
	out := make([]string, 0, strings.Count(pattern, "{"))
	for _, m := range pathParamRe.FindAllStringSubmatch(pattern, -1) {
		out = append(out, m[1])
	}
	return out
}

// paramTag returns the parameter location and tag value of a Req field:
// query, header, cookie or path.
func paramTag(f reflect.StructField) (string, string) {
	for _, l := range []string{"query", "header", "cookie", pathLocation} {
		if t := f.Tag.Get(l); t != "" {
			return l, t
		}
	}
	return "", ""
}

// requiredParam reports whether a parameter field is required: it is optional
// when the query tag ends in ",omitempty", the json tag says omitempty, or the
// jsonschema tag supplies a default.
func requiredParam(f reflect.StructField) bool {
	_, tag := paramTag(f)
	return !optionalTag(tag) && !optionalTag(f.Tag.Get("json")) &&
		tagValue(f.Tag.Get("jsonschema"), "default") == ""
}

func optionalTag(tag string) bool {
	_, options, _ := strings.Cut(tag, ",")
	for option := range strings.SplitSeq(options, ",") {
		if option == "omitempty" || option == "omitzero" {
			return true
		}
	}
	return false
}

// isParamField reports whether an exported Req field is an OpenAPI parameter.
// A path: field is one: it types the {name} placeholder and must not appear in
// the body either.
func isParamField(f reflect.StructField) bool {
	loc, value := paramTag(f)
	// query:"-" (or header:"-") opts out: the field is a plain body field.
	return f.PkgPath == "" && loc != "" && parts0(value) != "-"
}

// hasParamField reports whether t carries any parameter field.
func hasParamField(t reflect.Type) bool { return slices.ContainsFunc(exportedFields(t), isParamField) }

// taggedParams reflects Req fields carrying a query/header/cookie tag into
// OpenAPI parameters.
func (sr *schemaRegistry) taggedParams(req reflect.Type) []any {
	var params []any
	seen := make(map[string]bool)
	for _, f := range exportedFields(req) {
		loc, qt := paramTag(f)
		// a non-parameter is a body field, and a path: field only types an
		// already-declared placeholder: neither becomes a query parameter here.
		if !isParamField(f) || loc == pathLocation {
			continue
		}
		name := parts0(qt)
		if name == "" || seen[loc+"\x00"+name] {
			panic("specout: empty or duplicate " + loc + " parameter " + name)
		}
		seen[loc+"\x00"+name] = true
		params = append(params, sr.parameter(name, loc, f))
	}
	return params
}

// parameter builds every location from the same field metadata. An untyped
// path placeholder has no field and uses the default required string schema.
func (sr *schemaRegistry) parameter(name, loc string, f reflect.StructField) *obj {
	schema := &jsonschema.Schema{Type: "string"}
	if f.Type != nil {
		schema = sr.reflectParam(f)
		if loc != pathLocation && f.Type.Kind() == reflect.Pointer {
			nullable(schema)
		}
	}
	p := newObj().set("name", name).set("in", loc).
		set("required", loc == pathLocation || requiredParam(f)).set("schema", schema)
	applyParamStyle(p, f, loc)
	if desc := tagValue(f.Tag.Get("jsonschema"), "description"); desc != "" {
		schema.Description = ""
		p.set("description", desc)
	}
	return p
}

var parameterStyles = map[string][]string{
	pathLocation: {"matrix", "label", "simple"},
	"header":     {"simple"},
	"cookie":     {"form"},
	"query":      {"form", "spaceDelimited", "pipeDelimited", "deepObject"},
}

// applyParamStyle copies style= and explode= onto the parameter object. They
// are parameter serialization keywords, not schema keywords, and the
// jsonschema tag is the one tag specout already reads on a parameter field.
func applyParamStyle(p *obj, f reflect.StructField, loc string) {
	tag := f.Tag.Get("jsonschema")
	if st := tagValue(tag, "style"); st != "" {
		if !slices.Contains(parameterStyles[loc], st) {
			panic("specout: field " + f.Name + " has invalid " + loc + " parameter style=" + st)
		}
		p.set("style", st)
	}
	if ex := tagValue(tag, "explode"); ex != "" {
		b, err := strconv.ParseBool(ex)
		if err != nil {
			panic("specout: field " + f.Name + " has explode=" + ex + ", must be true or false")
		}
		p.set("explode", b)
	}
}

// pathField finds the Req field tagged path:"name".
func pathField(req reflect.Type, name string) (reflect.StructField, bool) {
	for _, f := range exportedFields(req) {
		if parts0(f.Tag.Get("path")) == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// strayPathField returns a path:"name" tag that the pattern does not carry as
// a {name} placeholder. The field is dropped from the body and the
// placeholder falls back to a plain string parameter, so a typo here would
// silently document the wrong thing.
func strayPathField(req reflect.Type, pattern string) (string, bool) {
	have := map[string]bool{}
	for _, n := range pathParams(pattern) {
		have[strings.TrimSuffix(n, "...")] = true
	}
	for _, f := range exportedFields(req) {
		name := parts0(f.Tag.Get("path"))
		if name != "" && name != "-" && !have[name] {
			return name, true
		}
	}
	return "", false
}

// pathParamObjs builds OpenAPI parameter objects for {name} placeholders.
// A Req field tagged path:"name" types the parameter (petId is integer, not
// string); without one the parameter is a plain string.
func (sr *schemaRegistry) pathParamObjs(pattern string, req reflect.Type) []any {
	var out []any
	seen := make(map[string]bool)
	for _, name := range pathParams(pattern) {
		// one object per name: /a/{id}/b/{id} is a legal route template but
		// OpenAPI forbids two parameters with one name in one operation.
		if seen[name] {
			continue
		}
		seen[name] = true
		f, _ := pathField(req, name)
		out = append(out, sr.parameter(name, pathLocation, f))
	}
	return out
}

// Parameters have their own wire names; json:"-" only hides a body property.
func (sr *schemaRegistry) reflectParam(f reflect.StructField) *jsonschema.Schema {
	var tag strings.Builder
	for _, key := range []string{"jsonschema", "jsonschema_description", "jsonschema_extras"} {
		tag.WriteString(key + ":" + strconv.Quote(f.Tag.Get(key)) + " ")
	}
	s := sr.propSchema(f.Type, reflect.StructTag(tag.String()))
	if s == nil {
		panic("specout: field " + f.Name + " has no parameter schema")
	}
	return s
}

func parts0(s string) string {
	name, _, _ := strings.Cut(s, ",")
	return name
}

// tagValue extracts key=value from a jsonschema tag string.
func tagValue(tag, key string) string {
	for part := range strings.SplitSeq(tag, ",") {
		if after, ok := strings.CutPrefix(part, key+"="); ok {
			return after
		}
	}
	return ""
}
