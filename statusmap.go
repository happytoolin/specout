package specout

// DeclaredStatuses exposes the method+path -> codes map a route declares for
// itself, for the recorder's "declared but never produced" drift check. It
// excludes the global DefaultErrors envelope: no test is expected to trigger
// a 500 on every route.
func (d *Generator) DeclaredStatuses() (map[RouteKey]map[int]bool, error) {
	return d.statusMap(false)
}

// SpecStatuses is DeclaredStatuses plus the global DefaultErrors envelope:
// exactly the codes the emitted spec lists for each route. The recorder uses
// it for the other drift direction, so a handler that returns a declared
// default (say a 404) is not reported as "spec does not declare it".
func (d *Generator) SpecStatuses() (map[RouteKey]map[int]bool, error) {
	return d.statusMap(true)
}

// statusMap folds responsePlan into method+path -> codes. withDefaults adds the
// envelope and expands a range key to all 100 codes: the spec allows the whole
// range, and no test can be expected to produce all of it.
func (d *Generator) statusMap(withDefaults bool) (map[RouteKey]map[int]bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.resolveLocked(); err != nil {
		return nil, err
	}
	out := make(map[RouteKey]map[int]bool)
	for _, rec := range d.records {
		if rec.full == "" {
			continue
		}
		codes := map[int]bool{}
		for _, e := range d.responsePlan(rec, withDefaults) {
			if lo, hi, ok := rangeOf(e.key); ok {
				if e.code != 0 {
					// the caller named one code of the range for coverage
					codes[e.code] = true
				}
				if withDefaults {
					for c := lo; c <= hi; c++ {
						codes[c] = true
					}
				}
				continue
			}
			// status 0 is the "default" response: it names no code, so it is
			// a coverage expectation nothing can satisfy. SpecStatuses keeps
			// it as the "any code allowed" marker instead.
			if e.code == 0 && !withDefaults {
				continue
			}
			codes[e.code] = true
		}
		out[NewRouteKey(rec.method, rec.full)] = codes
	}
	return out, nil
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
