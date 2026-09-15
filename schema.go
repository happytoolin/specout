package specout

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/invopop/jsonschema"
)

// schemaRegistry reflects Go types into JSON Schema, dedupes by Go type, and
// names components.
type schemaRegistry struct {
	byType map[reflect.Type]*schemaEntry
	order  []reflect.Type
}

type schemaEntry struct {
	name string
	s    *jsonschema.Schema
}

func newSchemaRegistry() *schemaRegistry {
	return &schemaRegistry{byType: make(map[reflect.Type]*schemaEntry)}
}

var genericBrackets = regexp.MustCompile(`[\[\].]`)

// refFor reflects t, registers it as a component, returns its $ref path.
func (sr *schemaRegistry) refFor(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeFor[NoContent]() || t == reflect.TypeFor[Binary]() {
		return ""
	}
	if e, ok := sr.byType[t]; ok {
		return "#/components/schemas/" + e.name
	}
	r := &jsonschema.Reflector{DoNotReference: true}
	s := r.Reflect(reflect.New(t).Interface())
	s.ID = ""
	s.Version = ""
	e := &schemaEntry{name: sanitizeName(t), s: s}
	sr.byType[t] = e
	sr.order = append(sr.order, t)
	return "#/components/schemas/" + e.name
}

func sanitizeName(t reflect.Type) string {
	n := t.String() // pkg.Page[full/pkg.T]
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return sanitizeName(t.Elem()) + "List"
	}
	// strip generic package qualifiers inside brackets first
	idx := strings.Index(n, "[")
	if idx >= 0 {
		base := n[:idx]
		arg := n[idx+1 : len(n)-1]
		arg = arg[strings.LastIndex(arg, ".")+1:]
		base = base[strings.LastIndex(base, ".")+1:]
		return base + arg
	}
	return n[strings.LastIndex(n, ".")+1:]
}
