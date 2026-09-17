package specout

import (
	"maps"
	"slices"
)

// externalDocsObj builds an externalDocs object. OpenAPI requires the url
// field, so an empty one is a mistake rather than an omission.
func externalDocsObj(ed *ExternalDocs) *obj {
	if ed.URL == "" {
		panic("specout: ExternalDocs needs a URL")
	}
	return newObj().setIf("url", ed.URL).setIf("description", ed.Description)
}

// sortedKeys returns a string-keyed map's keys in sorted order: anything
// driven by a Go map would otherwise emit a different document per run.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

func toAny[T any](s []T) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
