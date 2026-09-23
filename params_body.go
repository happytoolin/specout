package specout

import "reflect"

// requestBodyObj builds one operation's requestBody: always required, with the
// media types the route declared or the single type the body schema prefers.
func requestBodyObj(rec *routeRecord, body reflect.Type, sr *schemaRegistry) *obj {
	ct, media := requestBodyFor(body, sr)
	content := newObj()
	if len(rec.reqContentTypes) > 0 {
		// one body shape under several media types
		for _, c := range rec.reqContentTypes {
			content.set(c, media)
		}
	} else {
		content.set(ct, media)
	}
	return newObj().set("required", true).set("content", content)
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
