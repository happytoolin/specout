package specout

import (
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"

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
	method          string
	pattern         string // as passed at registration
	full            string // resolved canonical path; absolute sources set it here
	absolute        bool   // std mux, gorilla, Document: full is final
	omit            bool   // catch-all: excluded from paths, kept for drift
	req, res        reflect.Type
	responses       []Response
	tags            []string
	summary         string
	operationID     string
	description     string
	externalDocs    *ExternalDocs
	reqContentTypes []string
	raw             map[string]any
	deprecated      bool
	public          bool
	fn              http.HandlerFunc
}

func (d *Generator) register(rec routeRecord) {
	// A zero Handler has no func: it would document an endpoint that panics
	// when served. Catch it here, the one funnel every binder goes through.
	if rec.fn == nil {
		panic("specout: nil HandlerFunc registered for " + rec.method + " " + rec.pattern)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frozen {
		panic("specout: registration after the spec was built")
	}
	d.records = append(d.records, &rec)
	d.known[rec.ptr()] = true
}

// ptr is the handler func's code pointer: what a router walk reports for this
// handler, and the identity stray detection and path pairing match on.
func (r *routeRecord) ptr() uintptr { return reflect.ValueOf(r.fn).Pointer() }

// rkey identifies one handler func on one method. A walk can report several
// paths for one rkey: the same func served from two mounts.
type rkey struct {
	ptr    uintptr
	method string
}

func (r *routeRecord) key() rkey { return rkey{r.ptr(), r.method} }

// candidate is one (handler, method, path) a router actually serves.
type candidate struct {
	rkey
	path string
}

// lookup reports whether a handler func is known to specout. ponytail:
// identity is the code pointer, so two routes that share one func literal
// cannot be told apart; a stray reusing a documented handler goes
// unreported. Per-route identity if that ever bites.
func (d *Generator) lookup(ptr uintptr) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.known[ptr]
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
			k := rkey{reflect.ValueOf(h).Pointer(), method}
			// chi reports one r.Handle(mALL) route once per method, so the
			// same path can arrive twice; the surplus check below counts
			// distinct paths, not observations.
			if !slices.Contains(hits[k], pattern) {
				hits[k] = append(hits[k], pattern)
			}
		})
	}

	// pair records sharing (ptr, method) deterministically: candidates equal
	// to the as-passed pattern win (the common case: the pattern is already
	// absolute), then longest, then lexical. Records sorted by pattern.
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
		pairs = append(pairs, pairing{rec, hits[rec.key()]})
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].rec.pattern < pairs[j].rec.pattern })
	claimed := make(map[candidate]bool)
	for _, p := range pairs {
		sort.Slice(p.cands, func(i, j int) bool {
			if ei, ej := p.cands[i] == p.rec.pattern, p.cands[j] == p.rec.pattern; ei != ej {
				return ei
			}
			if len(p.cands[i]) != len(p.cands[j]) {
				return len(p.cands[i]) > len(p.cands[j])
			}
			return p.cands[i] < p.cands[j]
		})
		for _, c := range p.cands {
			ck := candidate{p.rec.key(), c}
			if claimed[ck] {
				continue
			}
			p.rec.full = c
			claimed[ck] = true
			break
		}
	}

	// a handler served at more paths than it has records: chi cannot say
	// which mount the caller meant, and documenting one drops the rest, so
	// fail loud and name every path.
	for _, p := range pairs {
		var extra []string
		for _, c := range p.cands {
			if !claimed[candidate{p.rec.key(), c}] {
				extra = append(extra, c)
			}
		}
		if len(extra) > 0 {
			return fmt.Errorf("specout: %s %s is also served at %s; adopt only the outermost router, or register each path with Document",
				p.rec.method, p.rec.pattern, strings.Join(extra, ", "))
		}
	}

	// one validation pass over the resolved records, first problem wins.
	seen := make(map[string]string)
	for _, rec := range d.records {
		// unresolved: name the pattern
		if rec.full == "" {
			return fmt.Errorf("specout: %s %q never resolved; adopt the outermost router (Adopt) or use Document", rec.method, rec.pattern)
		}
		// an empty parameter name (/{} ) is not a legal OpenAPI path template.
		// chi accepts the route, so fail here instead of emitting a broken path.
		if strings.Contains(docPath(rec.full), "{}") {
			return fmt.Errorf("specout: %s %q has an empty path parameter {}; give it a name", rec.method, rec.pattern)
		}
		// a Req field tagged path:"name" with no {name} in the pattern is a
		// typo: the field leaves the body and the placeholder stays a plain
		// string.
		if name, ok := strayPathField(rec.req, rec.full); ok {
			return fmt.Errorf("specout: %s %q has a Req field tagged path:%q, but the pattern has no {%s}", rec.method, rec.pattern, name, name)
		}
		// duplicate canonical (path, method) from two registrations fails loud.
		// Keyed on the documented path: /x/{id:[0-9]+} and /x/{id:[a-z]+} are
		// two distinct route patterns but one OpenAPI path, and the later one
		// would silently overwrite the earlier operation.
		ck := docPath(rec.full) + "|" + rec.method
		if first, dup := seen[ck]; dup {
			return fmt.Errorf("specout: duplicate route %s %s (registered as %q and %q)", rec.method, docPath(rec.full), first, rec.pattern)
		}
		seen[ck] = rec.pattern
	}
	return nil
}

// chi.Routes implementation: the generator has no subroutes, so mounting
// the spec on a chi router does not surface as stray handler entries.
func (d *Generator) Routes() []chi.Route { return nil }

func (d *Generator) Middlewares() chi.Middlewares { return nil }

func (d *Generator) Match(rctx *chi.Context, method, path string) bool { return false }

func (d *Generator) Find(rctx *chi.Context, method, path string) string { return "" }
