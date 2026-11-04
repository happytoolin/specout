package specout

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Encoding describes how one field of a form or multipart request body travels.
type Encoding struct {
	// ContentType is the media type of the field, such as "image/png".
	ContentType string
	// Headers are the headers of a multipart field.
	Headers []Header
	// Style is form, spaceDelimited, pipeDelimited or deepObject.
	Style string
	// Explode writes each value of a list or map apart. Leave it nil for the
	// OpenAPI default, or set it with new(true) or new(false).
	Explode *bool
	// AllowReserved keeps reserved characters of the value unescaped.
	AllowReserved bool
}

const (
	mediaMultipart = "multipart/form-data"
	mediaForm      = "application/x-www-form-urlencoded"
)

// baseMediaType is ct without its parameters, in lower case. So
// "Application/X-WWW-Form-Urlencoded; charset=utf-8" is a form type.
func baseMediaType(ct string) string {
	t, _, _ := strings.Cut(ct, ";")
	return strings.ToLower(strings.TrimSpace(t))
}

// formMedia reports whether media type ct sends the body as form fields, which
// form tags name.
func formMedia(ct string) bool {
	t := baseMediaType(ct)
	return t == mediaMultipart || t == mediaForm
}

// takesEncoding reports whether OpenAPI applies an encoding to media type ct.
func takesEncoding(ct string) bool {
	t := baseMediaType(ct)
	return t == mediaForm || strings.HasPrefix(t, "multipart/")
}

// checkEncoding checks that the request body is a form, and that each encoding
// names a field of it on each media type that takes the encoding. A form type
// uses the form names of the fields, and another multipart type the JSON names.
func checkEncoding(rec *routeRecord, body reflect.Type) {
	if len(rec.body.Encoding) == 0 {
		return
	}
	types := rec.body.ContentTypes
	if len(types) == 0 {
		types = []string{requestContentType(body)}
	}
	where := rec.label() + ": Request.Encoding"
	if !slices.ContainsFunc(types, takesEncoding) {
		panic("specout: " + where + ": the request body is not a form; " +
			"set Request.ContentTypes to " + mediaMultipart + " or " + mediaForm)
	}
	for _, ct := range types {
		if takesEncoding(ct) {
			checkEncodingNames(where, ct, bodyFieldNames(body, formMedia(ct)), rec.body.Encoding)
		}
	}
	for _, name := range sortedKeys(rec.body.Encoding) {
		field := where + " " + strconv.Quote(name)
		enc := rec.body.Encoding[name]
		checkHeaders(field, enc.Headers)
		if styles := parameterStyles[InQuery]; enc.Style != "" && !slices.Contains(styles, enc.Style) {
			panic("specout: " + field + ": Style " + strconv.Quote(enc.Style) + " is not valid; use " +
				strings.Join(styles, ", "))
		}
	}
}

// checkEncodingNames panics on an encoding that names no field of the body of
// media type ct. fields lists the names of the fields on ct.
func checkEncodingNames(where, ct string, fields []string, encodings map[string]Encoding) {
	for _, name := range sortedKeys(encodings) {
		if !slices.Contains(fields, name) {
			panic("specout: " + where + " " + strconv.Quote(name) + ": the " + ct +
				" body has no such field, only " + strings.Join(fields, ", ") + didYouMean(name, fields))
		}
	}
}

// bodyFieldNames lists the names of a body's fields on the wire: the JSON
// names, or the form names when form is true. A field that the schema hides
// with jsonschema:"-" is not listed, nor is a form:"-" field on a form.
func bodyFieldNames(body reflect.Type, form bool) []string {
	if body.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for _, f := range jsonFields(body, false) {
		name := fieldName(f)
		if form {
			name = formName(f)
		}
		if name != "-" && parts0(f.Tag.Get("jsonschema")) != "-" {
			names = append(names, name)
		}
	}
	return names
}

// encodingObjs is the encoding object of one media type.
func encodingObjs(encodings map[string]Encoding, sr *schemaRegistry) *obj {
	out := newObj()
	for _, name := range sortedKeys(encodings) {
		enc := encodings[name]
		o := newObj().setIf("contentType", enc.ContentType)
		if len(enc.Headers) > 0 {
			o.set("headers", headerObjs(enc.Headers, sr))
		}
		o.setIf("style", enc.Style)
		if enc.Explode != nil {
			o.set("explode", *enc.Explode)
		}
		if enc.AllowReserved {
			o.set("allowReserved", true)
		}
		out.set(name, o)
	}
	return out
}
