package specout

import "github.com/happytoolin/specout/internal/bridge"

// statusMap folds responsePlan into method+path -> codes. Without
// withDefaults, it holds the codes that each route declares for itself, which
// a test must produce. withDefaults adds the codes of Config.DefaultErrors,
// and expands a range key to all 100 codes: the codes that a response may
// have.
func (g *Generator) statusMap(withDefaults bool) (map[bridge.Key]map[int]bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.resolveLocked(); err != nil {
		return nil, err
	}
	out := make(map[bridge.Key]map[int]bool, len(g.records))
	for _, rec := range g.records {
		out[newKey(rec.method, rec.full)] = planStatuses(g.responsePlan(rec, withDefaults), withDefaults)
	}
	return out, nil
}

// planStatuses folds one route's response plan into its codes. A default
// permits any code, but only an explicit status creates a coverage
// expectation. With withDefaults, code 0 means "any code".
func planStatuses(plan []respEntry, withDefaults bool) map[int]bool {
	codes := map[int]bool{}
	for _, e := range plan {
		if e.key == defaultResponseKey && withDefaults {
			codes[0] = true
			continue
		}
		if e.code != 0 {
			codes[e.code] = true
		}
		if lo, hi, ok := rangeOf(e.key); ok && withDefaults {
			for c := lo; c <= hi; c++ {
				codes[c] = true
			}
		}
	}
	return codes
}

// statusRangeSpan is the width of one OpenAPI status range: "4XX" is 400-499.
const statusRangeSpan = 100

// rangeOf parses a range response key ("4XX") into its bounds.
func rangeOf(key string) (int, int, bool) {
	if len(key) != 3 || key[1] != 'X' || key[2] != 'X' || key[0] < '1' || key[0] > '5' {
		return 0, 0, false
	}
	lo := int(key[0]-'0') * statusRangeSpan
	return lo, lo + statusRangeSpan - 1, true
}

// newKey returns the key of a route, with router syntax normalized. Trailing
// slashes stay, so /a and /a/ are two routes.
func newKey(method, pattern string) bridge.Key {
	return bridge.Key{Method: method, Path: docPath(canonicalPath(pattern))}
}
