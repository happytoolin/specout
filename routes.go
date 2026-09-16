package specout

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"

	"github.com/go-chi/chi/v5"
)

// routeSource is the unexported seam adapters implement: walk reports every
// (method, canonical pattern, handler) the router can serve. In-repo
// adapters share it; an exported hook waits for an out-of-repo router that
// composes prefixes invisibly (echo, fiber, gin use absolute patterns and
// Document instead).
type routeSource interface {
	walk(func(method, pattern string, h http.Handler))
}

// routeRecord holds metadata captured at registration time. Relative
// patterns (chi groups) resolve to full at build time; absolute sources set
// full at registration and are never touched by a walk.
type routeRecord struct {
	method      string
	pattern     string // as passed at registration
	full        string // resolved canonical path; absolute sources set it here
	absolute    bool   // std mux, gorilla, Document: full is final
	omit        bool   // catch-all: excluded from paths, kept for drift
	req, res    reflect.Type
	responses   []Response
	tags        []string
	summary     string
	operationID string
	description string
	deprecated  bool
	public      bool
	fn          http.HandlerFunc
}

func (d *Generator) register(rec routeRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frozen {
		panic("specout: registration after the spec was built")
	}
	d.records = append(d.records, &rec)
	d.known[reflect.ValueOf(rec.fn).Pointer()] = true
}

func (d *Generator) lookup(ptr uintptr) (bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.known[ptr]
	return ok, d.frozen
}

// addSource remembers a router to walk at build time. Idempotent per router.
func (d *Generator) addSource(s routeSource) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, x := range d.sources {
		if x == s {
			return
		}
	}
	d.sources = append(d.sources, s)
}

// resolveLocked assigns every record a full path. Caller holds d.mu.
func (d *Generator) resolveLocked() error {
	hits := make(map[rkey][]string)
	for _, src := range d.sources {
		src.walk(func(method, pattern string, h http.Handler) {
			ptr := reflect.ValueOf(h).Pointer()
			k := rkey{ptr, method}
			hits[k] = append(hits[k], pattern)
		})
	}

	// pair records sharing (ptr, method) deterministically: records sorted
	// by as-passed pattern, candidates by length desc then lexical, each
	// claims one.
	type pairing struct {
		rec   *routeRecord
		cands []string
	}
	var pairs []pairing
	for i := range d.records {
		rec := d.records[i]
		if rec.absolute || rec.full != "" {
			continue
		}
		k := rkey{reflect.ValueOf(rec.fn).Pointer(), rec.method}
		pairs = append(pairs, pairing{rec, hits[k]})
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].rec.pattern < pairs[j].rec.pattern })
	claimed := make(map[string]bool)
	for _, p := range pairs {
		sort.Slice(p.cands, func(i, j int) bool {
			if len(p.cands[i]) != len(p.cands[j]) {
				return len(p.cands[i]) > len(p.cands[j])
			}
			return p.cands[i] < p.cands[j]
		})
		k := reflect.ValueOf(p.rec.fn).Pointer()
		for _, c := range p.cands {
			ck := fmt.Sprintf("%d|%s|%s", k, p.rec.method, c)
			if claimed[ck] {
				continue
			}
			p.rec.full = c
			claimed[ck] = true
			break
		}
	}

	// unresolved records are a build error: name the pattern
	for _, rec := range d.records {
		if rec.full == "" {
			return fmt.Errorf("specout: %s %q never resolved; adopt the outermost router (Adopt) or use Document", rec.method, rec.pattern)
		}
	}

	// duplicate canonical (path, method) from two registrations fails loud.
	seen := make(map[string]string)
	for _, rec := range d.records {
		ck := rec.full + "|" + rec.method
		if first, dup := seen[ck]; dup {
			return fmt.Errorf("specout: duplicate route %s %s (registered as %q and %q)", rec.method, rec.full, first, rec.pattern)
		}
		seen[ck] = rec.pattern
	}
	return nil
}

type rkey struct {
	ptr    uintptr
	method string
}

// chi.Routes implementation: the generator has no subroutes, so mounting
// the spec on a chi router does not surface as stray handler entries.
func (d *Generator) Routes() []chi.Route { return nil }

func (d *Generator) Middlewares() chi.Middlewares { return nil }

func (d *Generator) Match(rctx *chi.Context, method, path string) bool { return false }

func (d *Generator) Find(rctx *chi.Context, method, path string) string { return "" }
