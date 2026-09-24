package specout

import (
	"maps"
	"reflect"
	"slices"

	"github.com/invopop/jsonschema"
)

// requestBodyObj builds one operation's requestBody: always required, with the
// media types the route declared or the single type the body schema prefers.
func requestBodyObj(rec *routeRecord, body reflect.Type, sr *schemaRegistry) *obj {
	ct, base := requestBodyFor(body, sr)
	content := newObj()
	types := rec.reqContentTypes
	if len(types) == 0 {
		types = []string{ct}
	}
	for _, c := range types {
		schema := *base
		if c == "multipart/form-data" || c == "application/x-www-form-urlencoded" {
			schema = sr.formSchema(body, base)
		}
		if rec.req.Kind() == reflect.Pointer {
			nullable(&schema)
		}
		sr.inline = append(sr.inline, &schema)
		content.set(c, newObj().set("schema", &schema))
	}
	return newObj().set("required", true).set("content", content)
}

// formSchema copies the JSON component only when form tags change its names.
// Rename together so swapped names cannot overwrite fields before they are read.
func (sr *schemaRegistry) formSchema(body reflect.Type, base *jsonschema.Schema) jsonschema.Schema {
	source := sr.bodyForRef(base.Ref)
	if source == nil || source.Properties == nil {
		return *base
	}
	renames := make(map[string]string)
	for _, f := range jsonFields(body, false) {
		name, form := fieldName(f), parts0(f.Tag.Get("form"))
		if form != "" && form != "-" && form != name {
			renames[name] = form
		}
	}
	if len(renames) == 0 {
		return *base
	}
	form := *source
	form.Extras = maps.Clone(source.Extras)
	form.Properties = jsonschema.NewProperties()
	for name := range source.Properties.KeysFromOldest() {
		wireName := name
		if renamed, ok := renames[name]; ok {
			wireName = renamed
		}
		if _, duplicate := form.Properties.Get(wireName); duplicate {
			panic("specout: duplicate form field " + wireName)
		}
		form.Properties.Set(wireName, source.Properties.Value(name))
	}
	form.Required = slices.Clone(source.Required)
	for i, name := range form.Required {
		if renamed, ok := renames[name]; ok {
			form.Required[i] = renamed
		}
	}
	return form
}

// requestBodyFor picks the request content type for a body type: a bare File
// is an octet-stream payload, a struct with a binary field is
// multipart/form-data, anything else is JSON.
func requestBodyFor(body reflect.Type, sr *schemaRegistry) (string, *jsonschema.Schema) {
	if body == reflect.TypeFor[File]() {
		return "application/octet-stream", binarySchema()
	}
	schema := &jsonschema.Schema{Ref: sr.refFor(body)}
	if hasBinaryField(body) {
		return "multipart/form-data", schema
	}
	return "application/json", schema
}

// binarySchema is the schema of a raw byte payload.
func binarySchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Format: "binary"}
}

// hasBinaryField reports whether any field is a File marker or tagged format=binary.
func hasBinaryField(t reflect.Type) bool {
	t = deref(t)
	if t.Kind() != reflect.Struct {
		return false
	}
	fileT := reflect.TypeFor[File]()
	if t == fileT {
		return true
	}
	for _, field := range jsonFields(t, false) {
		if parts0(field.Tag.Get("jsonschema")) == "-" {
			continue
		}
		ft := deref(field.Type)
		if ft == fileT || (ft.Kind() == reflect.Slice && deref(ft.Elem()) == fileT) {
			return true
		}
		if tagValue(field.Tag.Get("jsonschema"), "format") == "binary" {
			return true
		}
	}
	return false
}
