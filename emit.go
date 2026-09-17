package specout

import "sort"

// strObj builds an object from name/value pairs, skipping empty values.
// Every one of these (contact, license, externalDocs) is optional per field.
func strObj(pairs ...string) *obj {
	o := newObj()
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			o.set(pairs[i], pairs[i+1])
		}
	}
	return o
}

// externalDocsObj builds an externalDocs object. OpenAPI requires the url
// field, so an empty one is a mistake rather than an omission.
func externalDocsObj(ed *ExternalDocs) *obj {
	if ed.URL == "" {
		panic("specout: ExternalDocs needs a URL")
	}
	return strObj("url", ed.URL, "description", ed.Description)
}

// sortedKeys returns a string-keyed map's keys in sorted order: anything
// driven by a Go map would otherwise emit a different document per run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func toAny[T any](s []T) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
