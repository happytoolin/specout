package specout

import (
	"cmp"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/invopop/jsonschema"
)

// requestBodyObj builds one operation's requestBody, with the media types the
// route declared or the single type the body schema prefers. It is required
// unless the route says otherwise.
func requestBodyObj(rec *routeRecord, body reflect.Type, sr *schemaRegistry) *obj {
	ct, base := requestBodyFor(body, sr)
	content := newObj()
	types := rec.body.ContentTypes
	if len(types) == 0 {
		types = []string{ct}
	}
	for _, c := range types {
		schema := *base
		if c == mediaMultipart || c == mediaForm {
			schema = sr.formSchema(body, base)
		}
		if rec.req.Kind() == reflect.Pointer {
			nullable(&schema)
		}
		sr.inline = append(sr.inline, &schema)
		media := newObj().set("schema", &schema)
		setExamples(media, rec.label(), rec.body.Example, rec.body.Examples)
		if takesEncoding(c) && len(rec.body.Encoding) > 0 {
			media.set("encoding", encodingObjs(rec.body.Encoding, sr))
		}
		content.set(c, media)
	}
	return newObj().setIf("description", rec.body.Description).
		set("required", !rec.body.Optional).set("content", content)
}

// checkRequestBody checks that Req carries a body for the route's request
// fields to describe, that each Request.ContentTypes entry names a media type,
// that the examples can be written, and that the encodings fit the body.
func checkRequestBody(rec *routeRecord) {
	if !rec.describesRequestBody() {
		return
	}
	body, _ := requestBody(rec.req)
	if body == nil {
		panic("specout: " + rec.label() + ": Handler.Request describes a body, but Req has no body; " +
			"add a field with no path, query, header or cookie tag, or remove Handler.Request")
	}
	checkEncoding(rec, body)
	checkExamples(rec.label()+": Request", rec.body.Example, rec.body.Examples)
	for _, c := range rec.body.ContentTypes {
		mediaType(rec.label()+": Request", c)
	}
}

// formSchema copies the JSON component only when form tags change its names.
// Rename together so swapped names cannot overwrite fields before they are read.
func (sr *schemaRegistry) formSchema(body reflect.Type, base *jsonschema.Schema) jsonschema.Schema {
	source := sr.bodyForRef(base.Ref)
	if source == nil || source.Properties == nil {
		return *base
	}
	renames := formRenames(body)
	if len(renames) == 0 {
		return *base
	}
	return renamedSchema(source, renames)
}

// formRenames maps the JSON name of each body field to its form tag name,
// for the fields where the two differ. No form name is empty. A form:"-"
// field maps to "-": form binders skip it, so the form schema drops it.
func formRenames(body reflect.Type) map[string]string {
	renames := make(map[string]string)
	for _, f := range jsonFields(body, false) {
		name, form := fieldName(f), parts0(f.Tag.Get("form"))
		if form != "" && form != name {
			renames[name] = form
		}
	}
	return renames
}

// renamedSchema copies source with its property and required names renamed,
// and without the properties that renames drops.
func renamedSchema(source *jsonschema.Schema, renames map[string]string) jsonschema.Schema {
	dropped := func(name string) bool { return renames[name] == "-" }
	form := *source
	form.Extras = maps.Clone(source.Extras)
	form.Properties = jsonschema.NewProperties()
	for name := range source.Properties.KeysFromOldest() {
		if dropped(name) {
			continue
		}
		wireName := cmp.Or(renames[name], name)
		if _, duplicate := form.Properties.Get(wireName); duplicate {
			panic("specout: two fields have the form name " + strconv.Quote(wireName) + "; change one form tag")
		}
		form.Properties.Set(wireName, source.Properties.Value(name))
	}
	form.Required = slices.DeleteFunc(slices.Clone(source.Required), dropped)
	for i, name := range form.Required {
		form.Required[i] = cmp.Or(renames[name], name)
	}
	return form
}

// requestBodyFor picks the request content type for a body type: a bare File
// is an octet-stream payload, a struct with a binary field is
// multipart/form-data, anything else is JSON.
func requestBodyFor(body reflect.Type, sr *schemaRegistry) (string, *jsonschema.Schema) {
	ct := requestContentType(body)
	if body == reflect.TypeFor[File]() {
		return ct, binarySchema()
	}
	return ct, &jsonschema.Schema{Ref: sr.refFor(body)}
}

// requestContentType is the media type that a body type implies.
func requestContentType(body reflect.Type) string {
	switch {
	case body == reflect.TypeFor[File]():
		return "application/octet-stream"
	case hasBinaryField(body):
		return mediaMultipart
	default:
		return "application/json"
	}
}

// binarySchema is the schema of a raw byte payload.
func binarySchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: typeString, Format: formatBinary}
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
		if tagValue(field.Tag.Get("jsonschema"), "format") == formatBinary {
			return true
		}
	}
	return false
}
