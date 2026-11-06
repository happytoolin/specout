package specout

import "github.com/happytoolin/specout/internal/bridge"

//nolint:gochecknoinits // the recorder reaches the core only through these hooks
func init() {
	bridge.NewKey = newKey
	bridge.Statuses = func(doc any, withDefaults bool) (map[bridge.Key]map[int]bool, error) {
		return generator(doc).statusMap(withDefaults)
	}
}

// generator returns doc as a *Generator, and panics on a nil one.
func generator(doc any) *Generator {
	g, _ := doc.(*Generator)
	if g == nil {
		panic("specout: the generator is nil; make one with specout.New")
	}
	return g
}
