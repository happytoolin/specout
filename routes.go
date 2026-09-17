package specout

import (
	"cmp"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

// routeSource is the unexported seam adapters implement: walk reports every
// (method, canonical pattern, handler) the router can serve. In-repo
// adapters share it; an exported hook waits for an out-of-repo router that
// composes prefixes invisibly (echo, fiber, gin use absolute patterns and
// Document instead).
type routeSource interface {
	walk(fn func(method, pattern string, h http.Handler))
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
	d.mustBeOpen()
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
	if slices.Contains(d.sources, s) {
		return
	}
	d.sources = append(d.sources, s)
}

// recOf pulls the metadata fields off a Handler into a routeRecord. Every
// binder (std, gorilla, Document) starts here.
func recOf[Req, Res any](h Handler[Req, Res]) routeRecord {
	return routeRecord{
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary,
		operationID: h.OperationID, description: h.Description,
		externalDocs: h.ExternalDocs, reqContentTypes: h.RequestContentTypes,
		raw:        h.Raw,
		deprecated: h.Deprecated, public: h.Public, fn: h.HandlerFunc,
	}
}

// pairing is one record and the paths a walk reported for its handler.
type pairing struct {
	rec   *routeRecord
	cands []string
}

// resolveLocked assigns every record a full path, then checks the result.
// Caller holds d.mu.
func (d *Generator) resolveLocked() error {
	if err := d.pairPaths(); err != nil {
		return err
	}
	return d.validateRecords()
}

// walkHits collects every path a walk reports, per (handler, method).
func (d *Generator) walkHits() map[rkey][]string {
	hits := make(map[rkey][]string)
	for _, src := range d.sources {
		src.walk(func(method, pattern string, h http.Handler) {
			k := rkey{reflect.ValueOf(h).Pointer(), method}
			// chi reports one r.Handle(mALL) route once per method, so the
			// same path can arrive twice; the surplus check counts distinct
			// paths, not observations.
			if !slices.Contains(hits[k], pattern) {
				hits[k] = append(hits[k], pattern)
			}
		})
	}
	return hits
}

// pairPaths gives every record its full path. Candidates equal to the
// as-passed pattern win (the common case: the pattern is already absolute),
// then longest, then lexical. Records are sorted by pattern.
func (d *Generator) pairPaths() error {
	hits := d.walkHits()
	var pairs []pairing
	for i := range d.records {
		rec := d.records[i]
		if rec.absolute || rec.full != "" {
			continue
		}
		pairs = append(pairs, pairing{rec, hits[rec.key()]})
	}
	slices.SortStableFunc(pairs, func(a, b pairing) int {
		return strings.Compare(a.rec.pattern, b.rec.pattern)
	})
	claimed := d.claimedPaths()
	for _, p := range pairs {
		slices.SortFunc(p.cands, func(a, b string) int {
			if ea, eb := a == p.rec.pattern, b == p.rec.pattern; ea != eb {
				if ea {
					return -1
				}
				return 1
			}
			return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b))
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

	return d.checkSurplus(pairs, claimed)
}

// claimedPaths seeds the claim set with paths an earlier pass resolved. The
// resolve runs once per build and once per statusMap call, so a route
// registered between two resolves shares its handler's walk hits with the
// record that is already resolved; without this seed the surplus check reports
// that sibling path as a second mount of the new route.
func (d *Generator) claimedPaths() map[candidate]bool {
	claimed := make(map[candidate]bool)
	for _, rec := range d.records {
		if rec.full != "" && !rec.absolute {
			claimed[candidate{rec.key(), rec.full}] = true
		}
	}
	return claimed
}

// checkSurplus fails when a handler is served at more paths than it has
// records: chi cannot say which mount the caller meant, and documenting one
// drops the rest, so fail loud and name every path.
func (d *Generator) checkSurplus(pairs []pairing, claimed map[candidate]bool) error {
	for _, p := range pairs {
		var extra []string
		for _, c := range p.cands {
			if !claimed[candidate{p.rec.key(), c}] {
				extra = append(extra, c)
			}
		}
		if len(extra) > 0 {
			return fmt.Errorf(
				"specout: %s %s is also served at %s; adopt only the outermost "+
					"router, or register each path with Document",
				p.rec.method, p.rec.pattern, strings.Join(extra, ", "),
			)
		}
	}
	return nil
}

// validateRecords checks the resolved records. First problem wins.
func (d *Generator) validateRecords() error {
	seen := make(map[string]string)
	for _, rec := range d.records {
		// unresolved: name the pattern
		if rec.full == "" {
			return fmt.Errorf(
				"specout: %s %q never resolved; adopt the outermost router "+
					"(Adopt) or use Document",
				rec.method, rec.pattern,
			)
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
			return fmt.Errorf(
				"specout: %s %q has a Req field tagged path:%q, but the pattern "+
					"has no {%s}",
				rec.method, rec.pattern, name, name,
			)
		}
		// duplicate canonical (path, method) from two registrations fails loud.
		// Keyed on the documented path: /x/{id:[0-9]+} and /x/{id:[a-z]+} are
		// two distinct route patterns but one OpenAPI path, and the later one
		// would silently overwrite the earlier operation.
		ck := docPath(rec.full) + "|" + rec.method
		if first, dup := seen[ck]; dup {
			return fmt.Errorf(
				"specout: duplicate route %s %s (registered as %q and %q)",
				rec.method, docPath(rec.full), first, rec.pattern,
			)
		}
		seen[ck] = rec.pattern
	}
	return nil
}

// Routes implements chi.Routes. The generator has no subroutes, so chi.Walk
// stops at a mounted spec instead of reporting its ten all-method mount paths
// as strays. The four chi.Routes methods exist only for that interface.
func (d *Generator) Routes() []chi.Route { return nil }

// Middlewares implements chi.Routes. A Generator has none.
func (d *Generator) Middlewares() chi.Middlewares { return nil }

// Match implements chi.Routes. A Generator matches nothing.
func (d *Generator) Match(*chi.Context, string, string) bool { return false }

// Find implements chi.Routes. A Generator finds nothing.
func (d *Generator) Find(*chi.Context, string, string) string { return "" }
