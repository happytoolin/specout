package specout

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// bodyType returns the type whose schema is Req's request body plus the
// component name to register it under, or (nil, "") when Req carries no body.
//
// Req is path+query+body together, so a $ref to the whole struct would list a
// path or query parameter as a body property: reify the struct without its
// parameter fields instead. Any other Req is itself the body — []User is a
// JSON array body, and File is an octet-stream payload.
//
// StructOf reuses identical types, so repeated requests share one component.
func (sr *schemaRegistry) bodyType(req reflect.Type) (reflect.Type, string) {
	body, split := requestBody(req)
	if !split {
		return body, ""
	}
	return body, sr.nameFor(deref(req)) + "Body"
}

// requestBody returns the type of Req's request body, or nil when Req carries
// no body. split reports a body that is Req without its parameter fields.
func requestBody(req reflect.Type) (reflect.Type, bool) {
	if req == nil {
		return nil, false
	}
	req = deref(req)
	// an empty struct carries no body, but a bare File is an octet-stream
	// payload: it is a marker type, not a body schema.
	if req == reflect.TypeFor[File]() {
		return req, false
	}
	if req.Kind() != reflect.Struct {
		return req, false
	}
	if req.NumField() == 0 {
		return nil, false
	}
	if !hasParamField(req) {
		return req, false
	}
	fields := bodyFields(req)
	if len(fields) == 0 {
		// every field is a parameter: the operation has no body at all
		return nil, false
	}
	return reflect.StructOf(fields), true
}

type bodyFieldCandidate struct {
	field    reflect.StructField
	name     string
	depth    int
	tagged   bool
	optional bool
}

// bodyFields follows encoding/json's wire-name dominance. Parameter fields
// participate in shadowing, then disappear from the resulting body.
func bodyFields(req reflect.Type) []reflect.StructField {
	var out []reflect.StructField
	for _, f := range jsonFields(req, true) {
		if isParamField(f) {
			continue
		}
		f.Tag = bodyJSONTag(f.Tag, fieldName(f))
		f.Anonymous = false
		f.Name = "Field" + strconv.Itoa(len(out)+1)
		f.PkgPath = ""
		out = append(out, f)
	}
	return out
}

// jsonFields resolves JSON name dominance. A split request keeps embedded
// object-valued parameters intact so bodyFields can remove them afterwards.
func jsonFields(req reflect.Type, splitParameters bool) []reflect.StructField {
	candidates := collectBodyFields(req, splitParameters)
	byName := make(map[string][]bodyFieldCandidate)
	var names []string
	for _, candidate := range candidates {
		if _, ok := byName[candidate.name]; !ok {
			names = append(names, candidate.name)
		}
		byName[candidate.name] = append(byName[candidate.name], candidate)
	}
	var out []reflect.StructField
	for _, name := range names {
		if winner, ok := dominantBodyField(byName[name]); ok {
			out = append(out, winner.field)
		}
	}
	return out
}

// jsonTagEntry matches the json key of a struct tag, with its quoted value.
var jsonTagEntry = regexp.MustCompile(`(^|\s)json:"(?:[^"\\]|\\.)*"`)

// bodyJSONTag gives a body field its wire name as the json key and keeps the
// other keys of tag. The old json key goes, so a type name shows one.
func bodyJSONTag(tag reflect.StructTag, name string) reflect.StructTag {
	if _, options, ok := strings.Cut(tag.Get("json"), ","); ok && options != "" {
		name += "," + options
	}
	rest := strings.TrimSpace(jsonTagEntry.ReplaceAllString(string(tag), ""))
	return reflect.StructTag(strings.TrimSpace("json:" + strconv.Quote(name) + " " + rest))
}

// collectBodyFields lists the JSON field candidates of t and of the structs it
// embeds, in declaration order. With splitParameters, an embedded parameter
// field stays one candidate.
func collectBodyFields(t reflect.Type, splitParameters bool) []bodyFieldCandidate {
	c := fieldCollector{stack: make(map[reflect.Type]bool), splitParameters: splitParameters}
	c.collect(t, 0, false)
	return c.out
}

// fieldCollector holds the state of one collectBodyFields walk. stack holds
// the embedded structs on the current path, so an embedding cycle stops.
type fieldCollector struct {
	out             []bodyFieldCandidate
	stack           map[reflect.Type]bool
	splitParameters bool
}

// collect adds the candidates of struct t, found depth embeddings down.
// optional is true below an embedded pointer: those fields can be absent.
func (c *fieldCollector) collect(t reflect.Type, depth int, optional bool) {
	t = deref(t)
	if t == nil || t.Kind() != reflect.Struct || c.stack[t] {
		return
	}
	c.stack[t] = true
	defer delete(c.stack, t)
	for f := range t.Fields() {
		c.field(f, depth, optional)
	}
}

func (c *fieldCollector) field(f reflect.StructField, depth int, optional bool) {
	if f.Tag.Get("json") == "-" {
		return
	}
	if promotes(f) && (!c.splitParameters || !isParamField(f)) {
		if parts0(f.Tag.Get("jsonschema")) != "-" {
			c.collect(f.Type, depth+1, optional || f.Type.Kind() == reflect.Pointer)
		}
		return
	}
	if f.PkgPath != "" && !f.Anonymous {
		return
	}
	name := parts0(f.Tag.Get("json"))
	if optional {
		f.Tag = reflect.StructTag("json:" + strconv.Quote(f.Tag.Get("json")+",omitempty") + " " + string(f.Tag))
	}
	c.out = append(c.out, bodyFieldCandidate{
		field: f, name: fieldName(f), depth: depth, tagged: name != "", optional: optional,
	})
}

func dominantBodyField(fields []bodyFieldCandidate) (bodyFieldCandidate, bool) {
	if len(fields) == 0 {
		return bodyFieldCandidate{}, false
	}
	winner, unique := fields[0], true
	for _, field := range fields[1:] {
		// Shallower fields win; an explicit JSON name breaks a depth tie.
		switch {
		case field.depth < winner.depth || (field.depth == winner.depth && field.tagged && !winner.tagged):
			winner, unique = field, true
		case field.depth == winner.depth && field.tagged == winner.tagged:
			unique = false
		}
	}
	return winner, unique
}
