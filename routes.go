package specout

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

// routeSource is the unexported seam the chi binder implements: walk reports
// every (method, canonical pattern, handler) the router can serve, so relative
// group patterns resolve at build time. Routers that report full paths at
// registration (echo, gin, fiber) use the exported Bind instead.
type routeSource interface {
	walk(fn func(method, pattern string, h http.Handler))
}

// routeRecord holds metadata captured at registration time. Relative
// patterns (chi groups) resolve to full at build time; absolute sources set
// full at registration and are never touched by a walk.
type routeRecord struct {
	method       string
	pattern      string // as passed at registration
	full         string // resolved canonical path; absolute sources set it here
	absolute     bool   // std mux, gorilla, Bind, Document: full is final
	literalStars bool   // std mux, gorilla and Bind treat stars outside parameters literally
	byPath       bool   // Bind: the router table shows method and path, never the handler
	catchAll     bool   // gorilla: a parameter regex can match a slash
	req, res     reflect.Type
	body         Request
	responses    []Response
	tags         []string
	summary      string
	operationID  string
	description  string
	externalDocs *ExternalDocs
	security     []Security
	servers      []Server
	callbacks    []Callback
	raw          map[string]any
	deprecated   bool
	public       bool
	outgoing     bool // webhook or callback: the API sends it, nothing serves it
	fn           http.HandlerFunc
	owner        *Generator // non-nil when the record is also the registered handler
}

// ServeHTTP preserves the original handler while giving each registration a
// distinct identity that router walks retain, including through chi middleware.
func (rec *routeRecord) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec.fn(w, r)
}

func (d *Generator) addRoute(rec routeRecord, bind func(*routeRecord)) {
	if rec.fn == nil {
		panic("specout: " + rec.label() +
			": HandlerFunc is nil; set it, or use Webhook or NewCallback for a request that your API sends")
	}
	// Config cannot change after New, so the check needs no lock.
	d.checkRoute(&rec)
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

// lookup checks bound registration identity, or a Bind or Document record by
// its absolute method and path: their routers never hand back the handler.
func (d *Generator) lookup(method, pattern string, h http.Handler) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	pattern = canonicalPath(pattern)
	if rec := d.ownRecord(method, h); rec != nil {
		return !rec.absolute || canonicalPath(rec.full) == pattern
	}
	return slices.ContainsFunc(d.records, func(rec *routeRecord) bool {
		return (rec.owner == nil || rec.byPath) && rec.method == method && canonicalPath(rec.full) == pattern
	})
}

// ownRecord returns h when it is a record this generator registered for
// method, and nil otherwise.
func (d *Generator) ownRecord(method string, h http.Handler) *routeRecord {
	rec, ok := h.(*routeRecord)
	if !ok || rec == nil || rec.owner != d || rec.method != method {
		return nil
	}
	return rec
}

// addSource remembers a router to walk at build time. Idempotent per router
// that Go can compare. Another router value is kept each time: == on it would
// panic, and a second walk of one router finds the same paths.
func (d *Generator) addSource(s routeSource) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if canCompare(s) && slices.Contains(d.sources, s) {
		return
	}
	d.sources = append(d.sources, s)
	d.inputs++
}

// canCompare reports whether == on v cannot panic. The type alone does not
// tell: an interface field can hold a struct with a slice, map or func.
func canCompare(v any) bool { return reflect.ValueOf(v).Comparable() }

// recOf pulls the metadata fields off a Handler into a routeRecord. Every
// binder (std, gorilla, Document) starts here.
func recOf[Req, Res any](h Handler[Req, Res]) routeRecord {
	return routeRecord{
		req: reflect.TypeFor[Req](), res: reflect.TypeFor[Res](),
		responses: h.Responses, tags: h.Tags, summary: h.Summary,
		operationID: h.OperationID, description: h.Description,
		externalDocs: h.ExternalDocs, body: h.Request, security: h.Security,
		servers: h.Servers, callbacks: h.Callbacks, raw: h.Raw,
		deprecated: h.Deprecated, public: h.Public, fn: h.HandlerFunc,
	}
}

// describesRequestBody reports whether the route sets any request body field.
func (rec *routeRecord) describesRequestBody() bool {
	b := rec.body
	return len(b.ContentTypes) > 0 || b.Description != "" || b.Optional ||
		b.Example != nil || len(b.Examples) > 0 || len(b.Encoding) > 0
}

// label names the record in a message: its method, and its path or its name.
func (rec *routeRecord) label() string { return rec.method + " " + cmp.Or(rec.full, rec.pattern) }

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
func (d *Generator) walkHits(sources []routeSource) map[*routeRecord][]string {
	hits := make(map[*routeRecord][]string)
	for _, src := range sources {
		src.walk(func(method, pattern string, h http.Handler) {
			rec := d.ownRecord(method, h)
			if rec != nil && !slices.Contains(hits[rec], pattern) {
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
	hits := d.walkHits(d.sources)
	var errs []error
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
			errs = append(errs, fmt.Errorf(
				"specout: %s %s: served at multiple paths: %s; adopt only the "+
					"outermost router, or register each path with Document",
				rec.method, rec.pattern, strings.Join(paths, ", "),
			))
		}
	}
	return errors.Join(errs...)
}

// validateRecords checks the resolved records, and returns every problem.
func (d *Generator) validateRecords() error {
	claims := pathClaims{operations: map[string]string{}, templates: map[string]string{}}
	var errs []error
	for _, rec := range d.records {
		if err := d.checkResolved(rec); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, checkPathParams(rec), claims.claim(rec))
	}
	return errors.Join(errs...)
}

// checkResolved names a record that no router walk gave a full path.
func (d *Generator) checkResolved(rec *routeRecord) error {
	if rec.full != "" {
		return nil
	}
	if len(d.sources) > 0 {
		return fmt.Errorf(
			"specout: %s %s: no adopted router serves it; a later route with the "+
				"same method and path replaced it, or its router is not under the one passed to Adopt",
			rec.method, rec.pattern,
		)
	}
	return fmt.Errorf(
		"specout: %s %s: this route has no full path; call Adopt on the outermost router, or use Document",
		rec.method, rec.pattern,
	)
}

// templateParamRe matches one OpenAPI path template parameter, {name}.
var templateParamRe = regexp.MustCompile(`{[^{}]*}`)

// checkPathParams checks a resolved record's path parameters.
func checkPathParams(rec *routeRecord) error {
	// an empty parameter name (/{} ) is not a legal OpenAPI path template.
	// chi accepts the route, so fail here instead of emitting a broken path.
	if strings.Contains(docPath(rec.full), "{}") {
		return fmt.Errorf("specout: %s %s: empty path parameter {}; give it a name", rec.method, rec.pattern)
	}
	// /x/{{id}} and /x/{a{b}} balance their braces, and chi and gorilla accept
	// them, but a template parameter name holds no brace.
	if strings.ContainsAny(templateParamRe.ReplaceAllString(docPath(rec.full), ""), "{}") {
		return fmt.Errorf("specout: %s %s: a path parameter name has a brace in it; remove the brace",
			rec.method, rec.pattern)
	}
	// one name for two segments gives the handler one value: chi returns the
	// last, Fiber the first. chi rejects it in one pattern, not across Mount.
	if name, ok := repeatedParam(docPath(rec.full)); ok {
		return fmt.Errorf("specout: %s %s: path parameter %q appears twice; give each one a different name",
			rec.method, rec.pattern, name)
	}
	// chi reads {x*} as one segment named x*, not as a wildcard, but a * in
	// the path would drop the route as a catch-all.
	if name, ok := starredParam(docPath(rec.full)); ok {
		return fmt.Errorf("specout: %s %s: path parameter %q has a * in its name; "+
			"it is not a wildcard there, so remove it", rec.method, rec.pattern, name)
	}
	// a Req field tagged path:"name" with no {name} in the pattern is a
	// typo: the field leaves the body and the placeholder stays a plain
	// string.
	if name, ok := strayPathField(rec.req, rec.full); ok {
		return fmt.Errorf(
			"specout: %s %s: Req has a field tagged path:%q, but the pattern has no {%s}%s",
			rec.method, rec.pattern, name, name, didYouMean(name, pathParams(rec.full)),
		)
	}
	return nil
}

// starredParam returns a path parameter name of p that holds a *. A
// catch-all such as {*...}, which the adapters write for a wildcard, is no
// parameter name.
func starredParam(p string) (string, bool) {
	for _, name := range pathParams(p) {
		if strings.Contains(name, "*") && !strings.HasSuffix(name, "...") {
			return name, true
		}
	}
	return "", false
}

// repeatedParam returns a path parameter name that p uses twice.
func repeatedParam(p string) (string, bool) {
	names := pathParams(p)
	for i, name := range names {
		if slices.Contains(names[:i], name) {
			return name, true
		}
	}
	return "", false
}

// pathClaims holds the documented paths that earlier records claimed.
type pathClaims struct {
	// operations maps "documented path|method" to the registered pattern.
	operations map[string]string
	// templates maps a path hierarchy, with its parameter names blanked, to
	// the documented path that has it.
	templates map[string]string
}

// claim adds rec's documented path. It fails when the path conflicts with an
// earlier record.
func (c *pathClaims) claim(rec *routeRecord) error {
	documented := docPath(rec.full)
	// Catch-alls stay out of paths, so only emitted templates participate
	// in the OpenAPI hierarchy check. Exact method duplicates still fail
	// below because they would overwrite one drift-status entry.
	if !rec.isCatchAll() {
		// OpenAPI forbids two templated paths with the same hierarchy but
		// different parameter names, even when they hold different methods.
		// The same documented path may carry several methods.
		hierarchy := pathParamRe.ReplaceAllString(documented, "{}")
		if first, dup := c.templates[hierarchy]; dup && first != documented {
			return fmt.Errorf(
				"specout: %s %s: path %s is the same path to OpenAPI, with other parameter names; use one name in both",
				rec.method, documented, first,
			)
		}
		c.templates[hierarchy] = documented
	}
	// duplicate canonical (path, method) from two registrations fails loud.
	// Keyed on the documented path: /x/{id:[0-9]+} and /x/{id:[a-z]+} are
	// two distinct route patterns but one OpenAPI path, and the later one
	// would silently overwrite the earlier operation.
	key := documented + "|" + rec.method
	if first, dup := c.operations[key]; dup {
		return fmt.Errorf(
			"specout: %s %s: duplicate route, registered as %q and %q%s",
			rec.method, documented, first, rec.pattern, endAnchorHint(first, rec.pattern),
		)
	}
	c.operations[key] = rec.pattern
	return nil
}

// endAnchorHint explains a duplicate of std's /a/{$} and /a/: net/http serves
// the exact path and the paths below it apart, but both are the OpenAPI path
// /a/, with one drift entry. It says how to keep both routes.
func endAnchorHint(first, second string) string {
	if first+"{$}" != second && second+"{$}" != first {
		return ""
	}
	return ": net/http serves both, but OpenAPI has one path for them; document one, " +
		"register the other on the mux itself, and pass specout.Skip with its pattern to the recorder"
}

// Routes implements chi.Routes. The generator has no subroutes, so chi.Walk
// stops at a mounted spec instead of reporting its ten all-method mount paths
// as strays. The four chi.Routes methods exist only for that interface.
func (*Generator) Routes() []chi.Route { return nil }

// Middlewares implements chi.Routes. A Generator has none.
func (*Generator) Middlewares() chi.Middlewares { return nil }

// Match implements chi.Routes. A Generator matches nothing.
func (*Generator) Match(*chi.Context, string, string) bool { return false }

// Find implements chi.Routes. A Generator finds nothing.
func (*Generator) Find(*chi.Context, string, string) string { return "" }
