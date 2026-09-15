package specout

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/invopop/jsonschema"
)

var pathParamRe = regexp.MustCompile(`\{([^}:]+)(:[^}]+)?\}`)

// pathParams extracts {name} placeholders from a route pattern.
func pathParams(pattern string) []string {
	var out []string
	for _, m := range pathParamRe.FindAllStringSubmatch(pattern, -1) {
		out = append(out, m[1])
	}
	return out
}

// taggedParams reflects Req fields carrying a query/header/cookie tag into
// OpenAPI parameters. Returns (params, hasBodyFields).
func taggedParams(req reflect.Type, sr *schemaRegistry) ([]any, bool) {
	if req == nil {
		return nil, false
	}
	for req.Kind() == reflect.Pointer {
		req = req.Elem()
	}
	if req.Kind() != reflect.Struct {
		return nil, false
	}
	var params []any
	hasBody := false
	for i := 0; i < req.NumField(); i++ {
		f := req.Field(i)
		if f.PkgPath != "" {
			continue
		}
		loc := ""
		var qt string
		for _, l := range []string{"query", "header", "cookie"} {
			if t := f.Tag.Get(l); t != "" {
				loc, qt = l, t
				break
			}
		}
		if loc == "" || parts0(qt) == "-" {
			hasBody = true
			continue
		}
		name := parts0(qt)
		r := &jsonschema.Reflector{Anonymous: true, DoNotReference: true}
		fs := r.Reflect(reflect.New(f.Type).Interface())
		fs.Version = ""
		if f.Type.Kind() == reflect.Pointer {
			base := fs.Type
			fs.Type = ""
			fs.Extras = map[string]any{"type": []string{base, "null"}}
		}
		p := newObj().
			set("name", name).
			set("in", loc).
			set("required", !strings.Contains(qt, ",omitempty") && !strings.Contains(f.Tag.Get("json"), "omitempty")).
			set("schema", fs)
		if desc := tagValue(f.Tag.Get("jsonschema"), "description"); desc != "" {
			p.set("description", desc)
		}
		params = append(params, p)
	}
	return params, hasBody
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

// pathParamObjs builds OpenAPI parameter objects for {name} placeholders.
func pathParamObjs(pattern string) []any {
	var out []any
	for _, name := range pathParams(pattern) {
		out = append(out, newObj().
			set("name", name).
			set("in", "path").
			set("required", true).
			set("schema", newObj().set("type", "string")))
	}
	return out
}

// requestBodyFor picks the request content type: multipart/form-data when a
// field carries jsonschema:"format=binary" (a file upload), JSON otherwise.
func requestBodyFor(req reflect.Type, sr *schemaRegistry) (string, *obj) {
	for req.Kind() == reflect.Pointer {
		req = req.Elem()
	}
	schema := newObj().set("$ref", sr.refFor(req))
	if req.Kind() == reflect.Struct && hasBinaryField(req) {
		return "multipart/form-data", schema
	}
	return "application/json", schema
}

// hasBinaryField reports whether any field is a File marker or tagged format=binary.
func hasBinaryField(t reflect.Type) bool {
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
