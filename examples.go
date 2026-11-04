package specout

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"strconv"
)

// Example is one named example of a request or response body. Set Value or
// ExternalValue, not both.
type Example struct {
	Summary       string
	Description   string
	Value         any
	ExternalValue string
}

// checkExamples panics on examples that would break the document. where names
// the field in the message. bodies lists the Go types that the body can take,
// the first one for the message.
func checkExamples(where string, example any, examples map[string]Example, bodies ...reflect.Type) {
	if example != nil && len(examples) > 0 {
		panic("specout: " + where + ": sets both Example and Examples; set one")
	}
	if example != nil {
		checkExampleType(where+": Example", example, bodies)
		exampleValue(where, example)
	}
	for _, name := range sortedKeys(examples) {
		ex := examples[name]
		switch {
		case ex.Value != nil && ex.ExternalValue != "":
			panic("specout: " + where + ": example " + strconv.Quote(name) + " sets both Value and ExternalValue; set one")
		case ex.Value == nil && ex.ExternalValue == "":
			panic("specout: " + where + ": example " + strconv.Quote(name) + " has no value; set Value or ExternalValue")
		case ex.Value != nil:
			checkExampleType(where+": example "+strconv.Quote(name), ex.Value, bodies)
			exampleValue(where, ex.Value)
		}
	}
}

// checkExampleType panics when v is a named struct of a type that the body
// cannot take, such as an error body on a success response. A map, a slice, a
// scalar and raw JSON can show any shape, so they pass. A union body takes a
// value of each of its variants.
func checkExampleType(where string, v any, bodies []reflect.Type) {
	t := deref(reflect.TypeOf(v))
	if len(bodies) == 0 || t.Kind() != reflect.Struct || t.Name() == "" {
		return
	}
	for _, body := range bodies {
		body = deref(body)
		if body == t || body.Kind() == reflect.Interface {
			return
		}
	}
	want := deref(bodies[0])
	panic(fmt.Sprintf("specout: %s: the value is a %s, but the body is a %s; use a %s value", where, t, want, want))
}

// exampleValue encodes an example with encoding/json, the encoder that specout
// schemas describe.
func exampleValue(where string, v any) jsontext.Value {
	data, err := jsonv1.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("specout: %s: a value cannot be encoded as JSON: %v", where, err))
	}
	return jsontext.Value(data)
}

// bodyExamples are the examples of one body, encoded once for all of its media
// types.
type bodyExamples struct {
	example jsontext.Value
	named   map[string]Example
	values  map[string]jsontext.Value
}

// encodeExamples encodes the examples of one body. where names the body.
func encodeExamples(where string, example any, examples map[string]Example) bodyExamples {
	b := bodyExamples{named: examples, values: make(map[string]jsontext.Value, len(examples))}
	if example != nil {
		b.example = exampleValue(where, example)
	}
	for name, ex := range examples {
		if ex.Value != nil {
			b.values[name] = exampleValue(where, ex.Value)
		}
	}
	return b
}

// set writes the examples on one media type object. key gives the name of each
// top-level member on that media type, or false to drop it. A nil key keeps
// the values as they are.
func (b bodyExamples) set(media *obj, key func(string) (string, bool)) {
	if b.example != nil {
		media.set("example", reshapeObject(b.example, key))
	}
	if len(b.named) == 0 {
		return
	}
	out := newObj()
	for _, name := range sortedKeys(b.named) {
		ex := b.named[name]
		o := newObj().setIf("summary", ex.Summary).setIf("description", ex.Description)
		if v, ok := b.values[name]; ok {
			o.set("value", reshapeObject(v, key))
		}
		out.set(name, o.setIf("externalValue", ex.ExternalValue))
	}
	media.set("examples", out)
}

// requestExampleKeys tells how an encoded example of Req fits the body of one
// media type. A Req value also encodes its parameter fields, but the body has
// none. A File field encodes as {}, but its schema is binary, so no JSON value
// can show it. A field hidden with jsonschema:"-" is not in the schema. A form
// names its fields by their form tags, and drops form:"-" fields.
func requestExampleKeys(req, body reflect.Type, form bool) func(string) (string, bool) {
	if body.Kind() != reflect.Struct {
		return nil
	}
	names := bodyKeys(body, form)
	params := paramKeys(req, names)
	return func(key string) (string, bool) {
		name, inBody := names[key]
		switch {
		case params[key] || name == "-":
			return "", false
		case inBody:
			return name, true
		default:
			return key, true
		}
	}
}

// bodyKeys maps the JSON name of each body field to its name on media type
// form or not, or to "-" for a field that the example cannot show.
func bodyKeys(body reflect.Type, form bool) map[string]string {
	names := make(map[string]string)
	for _, f := range jsonFields(body, false) {
		name := fieldName(f)
		switch {
		case isFileField(f) || parts0(f.Tag.Get("jsonschema")) == "-":
			names[name] = "-"
		case form:
			names[name] = formName(f)
		default:
			names[name] = name
		}
	}
	return names
}

// paramKeys are the keys of an encoded Req value that are not in its body:
// the parameter fields. A Req that is its own body has none.
func paramKeys(req reflect.Type, body map[string]string) map[string]bool {
	keys := make(map[string]bool)
	req = deref(req)
	if req.Kind() != reflect.Struct || !hasParamField(req) {
		return keys
	}
	for _, f := range jsonFields(req, false) {
		if _, inBody := body[fieldName(f)]; !inBody {
			keys[fieldName(f)] = true
		}
	}
	return keys
}

// isFileField reports whether f holds a File marker or a list of them.
func isFileField(f reflect.StructField) bool {
	fileT, ft := reflect.TypeFor[File](), deref(f.Type)
	return ft == fileT || (ft.Kind() == reflect.Slice && deref(ft.Elem()) == fileT)
}

// reshapeObject renames or drops the top-level members of a JSON object with
// key. A value that is not an object, or a nil key, stays as it is.
func reshapeObject(v jsontext.Value, key func(string) (string, bool)) any {
	if key == nil || v.Kind() != '{' {
		return v
	}
	out := newObj()
	for _, m := range objectMembers(v) {
		if name, keep := key(m.name); keep {
			out.set(name, m.value)
		}
	}
	return out
}

// member is one member of a JSON object.
type member struct {
	name  string
	value jsontext.Value
}

// objectMembers lists the members of the JSON object v in order. v comes from
// json.Marshal, so a read error is a bug in specout.
func objectMembers(v jsontext.Value) []member {
	fail := func(err error) {
		if err != nil {
			panic("specout: read an encoded example: " + err.Error())
		}
	}
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		fail(err)
	}
	var out []member
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		fail(err)
		// the next read voids the token, so take its text first
		name := token.String()
		value, err := dec.ReadValue()
		fail(err)
		// the value is valid only until the next read, so keep a copy
		out = append(out, member{name: name, value: value.Clone()})
	}
	return out
}
