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
)

// routeRecord holds metadata captured at registration time.
type routeRecord struct {
	method       string
	pattern      string // as passed at registration
	full         string // the full path, in OpenAPI syntax
	literalStars bool   // Bind treats stars outside parameters literally
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
	outgoing     bool         // webhook or callback: the API sends it, nothing serves it
	parent       *routeRecord // callback: the operation that it belongs to
	fn           http.HandlerFunc
}

// ServeHTTP runs the handler of the route.
func (rec *routeRecord) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec.fn(w, r)
}

func (g *Generator) addRoute(rec routeRecord, bind func(*routeRecord)) {
	if rec.fn == nil {
		panic("specout: " + rec.label() +
			": HandlerFunc is nil; set it, or use Webhook or NewCallback for a request that your API sends")
	}
	// Config cannot change after New, so the check needs no lock.
	g.checkRoute(&rec)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.beginChange()
	// Check freeze before changing the live router. Append only after the
	// router accepts the registration, so a router panic leaves no record.
	if bind != nil {
		bind(&rec)
	}
	g.records = append(g.records, &rec)
}

// lookup reports whether a record has method and the path pattern.
func (g *Generator) lookup(method, pattern string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	pattern = canonicalPath(pattern)
	return slices.ContainsFunc(g.records, func(rec *routeRecord) bool {
		return rec.method == method && canonicalPath(rec.full) == pattern
	})
}

// recOf pulls the metadata fields off a Handler into a routeRecord. Every
// registration starts here.
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

func (rec *routeRecord) describesRequestBody() bool {
	b := rec.body
	return len(b.ContentTypes) > 0 || b.Description != "" || b.Optional ||
		len(b.Examples) > 0 || len(b.Encoding) > 0
}

// label names the record in a message: its method, and its path or its name.
// A callback also names the operation that it belongs to.
func (rec *routeRecord) label() string {
	own := rec.method + " " + cmp.Or(rec.full, rec.pattern)
	if rec.parent != nil {
		return rec.parent.label() + ": " + own
	}
	return own
}

// namePanic adds rec's label to the recovered panic v, when there is one. The
// label is built only for a panic.
func (rec *routeRecord) namePanic(v any) {
	if v != nil {
		addContext(rec.label(), v)
	}
}

// resolveLocked checks the paths of every record. Caller holds g.mu.
func (g *Generator) resolveLocked() error {
	return g.validateRecords()
}

func (g *Generator) validateRecords() error {
	claims := pathClaims{operations: map[string]string{}, templates: map[string]string{}}
	errs := make([]error, 0, len(g.records))
	for _, rec := range g.records {
		errs = append(errs, checkPathParams(rec), claims.claim(rec))
	}
	return errors.Join(errs...)
}

// templateParamRe matches one OpenAPI path template parameter, {name}.
var templateParamRe = regexp.MustCompile(`{[^{}]*}`)

func checkPathParams(rec *routeRecord) error {
	// an empty parameter name (/{} ) is not a legal OpenAPI path template.
	if strings.Contains(docPath(rec.full), "{}") {
		return fmt.Errorf("specout: %s %s: empty path parameter {}; give it a name", rec.method, rec.pattern)
	}
	// /x/{{id}} and /x/{a{b}} balance their braces, but a template parameter
	// name holds no brace.
	if strings.ContainsAny(templateParamRe.ReplaceAllString(docPath(rec.full), ""), "{}") {
		return fmt.Errorf("specout: %s %s: a path parameter name has a brace in it; remove the brace",
			rec.method, rec.pattern)
	}
	// one name for two segments gives the handler one value.
	if name, ok := repeatedParam(docPath(rec.full)); ok {
		return fmt.Errorf("specout: %s %s: path parameter %q appears twice; give each one a different name",
			rec.method, rec.pattern, name)
	}
	// a * in a parameter name would drop the route as a catch-all.
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
	return checkRawParameters(rec)
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

func repeatedParam(p string) (string, bool) {
	names := pathParams(p)
	for i, name := range names {
		if slices.Contains(names[:i], name) {
			return name, true
		}
	}
	return "", false
}

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
			"specout: %s %s: duplicate route, registered as %q and %q",
			rec.method, documented, first, rec.pattern,
		)
	}
	c.operations[key] = rec.pattern
	return nil
}
