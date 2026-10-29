package specout

import (
	"reflect"
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
	if req == nil {
		return nil, ""
	}
	req = deref(req)
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
	fields := bodyFields(req)
	if len(fields) == 0 {
		// every field is a parameter: the operation has no body at all
		return nil, ""
	}
	return reflect.StructOf(fields), sr.nameFor(req) + "Body"
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

func bodyJSONTag(tag reflect.StructTag, name string) reflect.StructTag {
	if _, options, ok := strings.Cut(tag.Get("json"), ","); ok && options != "" {
		name += "," + options
	}
	return reflect.StructTag("json:" + strconv.Quote(name) + " " + string(tag))
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
