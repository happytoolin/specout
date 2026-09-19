package specout

import "reflect"

// deref strips pointer levels. A nil type stays nil.
func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// exportedFields resolves promoted fields once for parameters.
// VisibleFields handles shadowing, ambiguous embeddings and recursive types.
func exportedFields(t reflect.Type) []reflect.StructField {
	t = deref(t)
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	out := make([]reflect.StructField, 0, t.NumField())
	for _, f := range reflect.VisibleFields(t) {
		if (f.PkgPath == "" || f.Anonymous) && (!promotes(f) || isParamField(f)) && promotedThrough(t, f.Index) {
			out = append(out, f)
		}
	}
	return out
}

// A named JSON object or an object-valued parameter owns its nested fields.
func promotedThrough(t reflect.Type, index []int) bool {
	for _, i := range index[:len(index)-1] {
		f := t.Field(i)
		if !promotes(f) || isParamField(f) {
			return false
		}
		t = deref(f.Type)
	}
	return true
}

// promotes reports whether f is an untagged embedded struct: the fields it
// promotes are properties of the parent object.
func promotes(f reflect.StructField) bool {
	if !f.Anonymous || parts0(f.Tag.Get("json")) != "" {
		return false
	}
	return deref(f.Type).Kind() == reflect.Struct
}
