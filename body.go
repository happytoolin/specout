package specout

import "reflect"

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
	v := bodyView{t: reflect.StructOf(fields), name: sr.nameFor(req)}
	sr.bodyViews[req] = v
	return v.t, v.name
}

// bodyFields lists the StructOf fields of a request-body view: req's exported,
// non-parameter fields, with untagged embedded structs expanded the way
// encoding/json expands them. reflect.StructOf rejects an unexported field,
// and the promoted fields belong in the body, so the expansion is required: a
// body that embeds a base type (ids, timestamps) must not panic the moment it
// also carries a query parameter.
func bodyFields(req reflect.Type) []reflect.StructField {
	// a direct field shadows a promoted one of the same name, and StructOf
	// rejects the duplicate: reserve the direct names before expanding
	taken := make(map[string]bool, req.NumField())
	for i := 0; i < req.NumField(); i++ {
		if f := req.Field(i); !isParamField(f) && !promotes(f) {
			taken[f.Name] = true
		}
	}
	var out []reflect.StructField
	for i := 0; i < req.NumField(); i++ {
		if f := req.Field(i); !isParamField(f) {
			out = bodyField(out, f, taken)
		}
	}
	return out
}

// ponytail: first declaration wins when two embedded structs promote the same
// name; encoding/json drops both as ambiguous. Add a depth map if that shows up.
func bodyField(out []reflect.StructField, f reflect.StructField, taken map[string]bool) []reflect.StructField {
	if promotes(f) {
		et := deref(f.Type)
		for i := 0; i < et.NumField(); i++ {
			sub := et.Field(i)
			if sub.PkgPath != "" && !sub.Anonymous {
				continue // unexported plain field: never marshaled
			}
			if taken[sub.Name] {
				continue // shadowed by a field at this or a shallower level
			}
			taken[sub.Name] = true
			out = bodyField(out, sub, taken)
		}
		return out
	}
	if f.PkgPath != "" {
		if !f.Anonymous {
			return out // unexported plain field: never marshaled
		}
		// unexported embedded type: StructOf needs an exported Go name; the
		// JSON property name still comes from the tag.
		if f.Tag.Get("json") == "" {
			f.Tag = reflect.StructTag("json:\"" + f.Name + "\" " + string(f.Tag))
		}
		f.Anonymous = false
		f.Name = "Embedded" + f.Name
		f.PkgPath = ""
	}
	return append(out, f)
}
