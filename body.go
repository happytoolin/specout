package specout

import (
	"reflect"
	"strconv"
	"strings"
)

// bodyView is the request-body view of a Req type that mixes parameters and
// body fields: the synthetic body-only struct, and the component name it is
// registered under.
type bodyView struct {
	t    reflect.Type
	name string
}

// bodyType returns the type whose schema is Req's request body plus the
// component name to register it under, or (nil, "") when Req carries no body.
//
// Req is path+query+body together, so a $ref to the whole struct would list a
// path or query parameter as a body property: reify the struct without its
// parameter fields instead. Any other Req is itself the body — []User is a
// JSON array body, and File is an octet-stream payload.
//
// The synthetic type is memoized per Req: StructOf must yield exactly one type
// per Req, or two operations sharing a Req type would each register a
// component under the same name.
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
	if v, ok := sr.bodyViews[req]; ok {
		return v.t, v.name
	}
	fields := bodyFields(req)
	if len(fields) == 0 {
		// every field is a parameter: the operation has no body at all
		return nil, ""
	}
	v := bodyView{t: reflect.StructOf(fields), name: sr.nameFor(req) + "Body"}
	sr.bodyViews[req] = v
	return v.t, v.name
}

type bodyFieldCandidate struct {
	field  reflect.StructField
	name   string
	depth  int
	tagged bool
}

// bodyFields follows encoding/json's wire-name dominance. Parameter fields
// participate in shadowing, then disappear from the resulting body.
func bodyFields(req reflect.Type) []reflect.StructField {
	var candidates []bodyFieldCandidate
	collectBodyFields(req, 0, &candidates, make(map[reflect.Type]bool))
	byName := make(map[string][]bodyFieldCandidate)
	var names []string
	for _, candidate := range candidates {
		if _, ok := byName[candidate.name]; !ok {
			names = append(names, candidate.name)
		}
		byName[candidate.name] = append(byName[candidate.name], candidate)
	}
	var selected []bodyFieldCandidate
	for _, name := range names {
		if winner, ok := dominantBodyField(byName[name]); ok && !isParamField(winner.field) {
			selected = append(selected, winner)
		}
	}
	out := make([]reflect.StructField, 0, len(selected))
	for i, candidate := range selected {
		f := candidate.field
		f.Anonymous = false
		f.Name = "Field" + strconv.Itoa(i+1)
		f.PkgPath = ""
		f.Tag = bodyJSONTag(f.Tag, candidate.name)
		out = append(out, f)
	}
	return out
}

func bodyJSONTag(tag reflect.StructTag, name string) reflect.StructTag {
	if _, options, ok := strings.Cut(tag.Get("json"), ","); ok && options != "" {
		name += "," + options
	}
	return reflect.StructTag("json:" + strconv.Quote(name) + " " + string(tag))
}

func collectBodyFields(t reflect.Type, depth int, out *[]bodyFieldCandidate, stack map[reflect.Type]bool) {
	t = deref(t)
	if t == nil || t.Kind() != reflect.Struct || stack[t] {
		return
	}
	stack[t] = true
	defer delete(stack, t)
	for f := range t.Fields() {
		if promotes(f) && !isParamField(f) {
			collectBodyFields(f.Type, depth+1, out, stack)
			continue
		}
		if f.PkgPath != "" && !f.Anonymous {
			continue
		}
		name := parts0(f.Tag.Get("json"))
		if name == "-" {
			continue
		}
		*out = append(*out, bodyFieldCandidate{
			field: f, name: fieldName(f), depth: depth, tagged: name != "",
		})
	}
}

func dominantBodyField(fields []bodyFieldCandidate) (bodyFieldCandidate, bool) {
	bestDepth := fields[0].depth
	for _, field := range fields[1:] {
		bestDepth = min(bestDepth, field.depth)
	}
	var best []bodyFieldCandidate
	for _, field := range fields {
		if field.depth == bestDepth {
			best = append(best, field)
		}
	}
	if len(best) == 1 {
		return best[0], true
	}
	var tagged []bodyFieldCandidate
	for _, field := range best {
		if field.tagged {
			tagged = append(tagged, field)
		}
	}
	if len(tagged) == 1 {
		return tagged[0], true
	}
	return bodyFieldCandidate{}, false
}
