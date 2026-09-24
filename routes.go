package specout

import (
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
	literalStars    bool   // std mux and gorilla treat stars outside parameters literally
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
	owner           *Generator // non-nil when the record is also the registered handler
}

// ServeHTTP preserves the original handler while giving each registration a
// distinct identity that router walks retain, including through chi middleware.
func (r *routeRecord) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.fn(w, req)
}

func (d *Generator) register(rec routeRecord, bind func(*routeRecord)) {
	if rec.fn == nil {
		panic("specout: nil HandlerFunc registered for " + rec.method + " " + rec.pattern)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mustBeOpen()
	// Check freeze before changing the live router. Append only after the
	// router accepts the registration, so a router panic leaves no record.
	if bind != nil {
		rec.owner = d
		bind(&rec)
	}
	d.records = append(d.records, &rec)
}

// lookup checks bound registration identity or an explicit Document declaration.
// Manual declarations are authoritative for their absolute method and path.
func (d *Generator) lookup(method, pattern string, h http.Handler) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	pattern = canonicalPath(pattern)
	if rec, ok := h.(*routeRecord); ok && rec != nil && rec.owner == d && rec.method == method {
		return !rec.absolute || canonicalPath(rec.full) == pattern
	}
	for _, rec := range d.records {
		if rec.owner == nil && rec.method == method && canonicalPath(rec.full) == pattern {
			return true
		}
	}
	return false
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

// resolveLocked assigns every record a full path, then checks the result.
// Caller holds d.mu.
func (d *Generator) resolveLocked() error {
	if err := d.pairPaths(); err != nil {
		return err
	}
	return d.validateRecords()
}

// walkHits collects distinct paths by registration identity. Unknown handler
// values are ignored here and reported by Adopt, without reflection assumptions.
func (d *Generator) walkHits() map[*routeRecord][]string {
	hits := make(map[*routeRecord][]string)
	for _, src := range d.sources {
		src.walk(func(method, pattern string, h http.Handler) {
			rec, ok := h.(*routeRecord)
			if !ok || rec == nil || rec.owner != d || rec.method != method {
				return
			}
			if !slices.Contains(hits[rec], pattern) {
				hits[rec] = append(hits[rec], pattern)
			}
		})
	}
	return hits
}

// pairPaths requires exactly one live path for each relative registration.
// Re-walk resolved records too: status queries do not freeze the router, so a
// mount added after a query must not silently leave the earlier path in place.
func (d *Generator) pairPaths() error {
	hits := d.walkHits()
	for _, rec := range d.records {
		if rec.absolute {
			continue
		}
		paths := hits[rec]
		rec.full = ""
		switch len(paths) {
		case 0:
			// validateRecords reports the unresolved registration below.
		case 1:
			rec.full = paths[0]
		default:
			slices.Sort(paths)
			return fmt.Errorf(
				"specout: %s %s is served at multiple paths: %s; adopt only the "+
					"outermost router, or register each path with Document",
				rec.method, rec.pattern, strings.Join(paths, ", "),
			)
		}
	}
	return nil
}

// validateRecords checks the resolved records. First problem wins.
func (d *Generator) validateRecords() error {
	seen := make(map[string]string)
	templates := make(map[string]string)
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
		documented := docPath(rec.full)
		// Catch-alls stay out of paths, so only emitted templates participate
		// in the OpenAPI hierarchy check. Exact method duplicates still fail
		// below because they would overwrite one drift-status entry.
		if !rec.isCatchAll() {
			// OpenAPI forbids two templated paths with the same hierarchy but
			// different parameter names, even when they hold different methods.
			// The same documented path may carry several methods.
			hierarchy := pathParamRe.ReplaceAllString(documented, "{}")
			if first, dup := templates[hierarchy]; dup && first != documented {
				return fmt.Errorf(
					"specout: equivalent templated paths %s and %s use different parameter names",
					first, documented,
				)
			}
			templates[hierarchy] = documented
		}
		// duplicate canonical (path, method) from two registrations fails loud.
		// Keyed on the documented path: /x/{id:[0-9]+} and /x/{id:[a-z]+} are
		// two distinct route patterns but one OpenAPI path, and the later one
		// would silently overwrite the earlier operation.
		ck := documented + "|" + rec.method
		if first, dup := seen[ck]; dup {
			return fmt.Errorf(
				"specout: duplicate route %s %s (registered as %q and %q)",
				rec.method, documented, first, rec.pattern,
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
