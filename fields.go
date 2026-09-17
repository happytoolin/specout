package specout

import "reflect"

// deref strips pointer levels. A nil type stays nil.
func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// exportedFields returns t's exported fields after dereferencing, or nil for a
// nil or non-struct type. Every walk over a Req type starts here, so the
// pointer and kind guard lives once.
func exportedFields(t reflect.Type) []reflect.StructField {
	t = deref(t)
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	out := make([]reflect.StructField, 0, t.NumField())
	for f := range t.Fields() {
		if f.PkgPath == "" {
			out = append(out, f)
		}
	}
	return out
}

// promotes reports whether f is an untagged embedded struct: the fields it
// promotes are properties of the parent object.
func promotes(f reflect.StructField) bool {
	if !f.Anonymous || f.Tag.Get("json") != "" {
		return false
	}
	return deref(f.Type).Kind() == reflect.Struct
}
