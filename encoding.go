package specout

import (
	"cmp"
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

// takesEncoding reports whether OpenAPI applies an encoding to media type ct.
func takesEncoding(ct string) bool {
	return ct == mediaForm || strings.HasPrefix(ct, "multipart/")
}

// checkEncoding checks that the request body is a form, and that each encoding
// names a field of it.
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
	fields := formFieldNames(body)
	for _, name := range sortedKeys(rec.body.Encoding) {
		field := where + " " + strconv.Quote(name)
		if !slices.Contains(fields, name) {
			panic("specout: " + field + ": the request body has no such field, only " + strings.Join(fields, ", ") +
				didYouMean(name, fields))
		}
		enc := rec.body.Encoding[name]
		checkHeaders(field, enc.Headers)
		if styles := parameterStyles[InQuery]; enc.Style != "" && !slices.Contains(styles, enc.Style) {
			panic("specout: " + field + ": Style " + strconv.Quote(enc.Style) + " is not valid; use " +
				strings.Join(styles, ", "))
		}
	}
}

// formFieldNames lists the names of a body's fields on a form. A form tag
// replaces the JSON name, and form:"-" drops the field.
func formFieldNames(body reflect.Type) []string {
	if body.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for _, f := range jsonFields(body, false) {
		name := cmp.Or(parts0(f.Tag.Get("form")), fieldName(f))
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
