package specout

import (
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
)

// unwrapDefs pulls a reflected schema out of its $ref/$defs wrapper.
// The def named after t becomes the component body; sibling defs register
// as components too. All #/$defs/ refs become #/components/schemas/ so
// the emitted tree is self-contained.
func (sr *schemaRegistry) unwrapDefs(t reflect.Type, s *jsonschema.Schema) *jsonschema.Schema {
	if s == nil || len(s.Definitions) == 0 {
		return s
	}
	name := sanitizeName(t)
	if n, ok := sr.overrides[t]; ok {
		name = n
	}
	body := s
	if s.Ref != "" && strings.HasSuffix(s.Ref, "/"+name) {
		body = s.Definitions[name]
	}
	// $defs is a map; registration order feeds component order, so walk the
	// names sorted to keep the output byte-deterministic.
	defNames := slices.DeleteFunc(slices.Sorted(maps.Keys(s.Definitions)), func(defName string) bool {
		return defName == name
	})
	for _, defName := range defNames {
		if sr.byName[defName] == nil {
			sr.addDef(defName, s.Definitions[defName])
		}
	}
	remapDefs(body)
	if body == s {
		// Every $def here was hoisted into components.schemas; a second copy
		// of the whole tree inside the body is dead weight.
		s.Definitions = nil
	}
	return body
}

// addDef registers a hoisted $def as a component directly by name; no
// Go type backs it, so it rides the same order/byType machinery keyed by
// a synthetic type.
func (sr *schemaRegistry) addDef(name string, def *jsonschema.Schema) {
	def.Version = ""
	splitEnums(def)
	normalizeOneOf(def, sr)
	if sr.closed {
		closeSchema(def)
	}
	remapDefs(def)
	if _, ok := sr.byName[name]; !ok {
		sr.byName[name] = def
		sr.nameOrder = append(sr.nameOrder, name)
	}
}

func lastSeg(s string) string {
	if _, last, ok := strings.CutLast(s, "."); ok {
		return last
	}
	return s
}
