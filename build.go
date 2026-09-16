package specout

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// build assembles the OpenAPI document: resolve chi paths via a single walk,
// merge std patterns, reflect schemas, emit operations in path order.
func (d *Generator) build() (*obj, error) {
	if err := d.resolveLocked(); err != nil {
		return nil, err
	}
	records := d.flatRecords()
	if err := checkOperationIDs(records); err != nil {
		return nil, err
	}
	for _, rec := range records {
		if isCatchAll(rec.full) {
			rec.omit = true
		}
	}

	spec := newObj()
	info := newObj().set("title", d.cfg.Title).set("version", d.cfg.Version)
	if d.cfg.Description != "" {
		info.set("description", d.cfg.Description)
	}
	spec.set("openapi", "3.1.0").set("info", info)
	if len(d.cfg.Servers) > 0 {
		servers := make([]any, 0, len(d.cfg.Servers))
		for _, s := range d.cfg.Servers {
			e := newObj().set("url", s.URL)
			if s.Description != "" {
				e.set("description", s.Description)
			}
			servers = append(servers, e)
		}
		spec.set("servers", servers)
	}

	paths := newObj()
	sr := newSchemaRegistry(d.cfg)
	for name, t := range d.variants {
		sr.registerVariant(t, name)
	}
	for t, name := range d.nameOverrides {
		sr.overrideName(t, name)
	}

	for _, rec := range records {
		op := newObj()
		if rec.summary != "" {
			op.set("summary", rec.summary)
		}
		if rec.description != "" {
			op.set("description", rec.description)
		}
		opID := rec.operationID
		if opID == "" {
			opID = operationID(rec.method, docPath(rec.full))
		}
		// a malformed segment ({}) derives no id: omit the field rather than
		// emit an empty, unusable operationId
		if opID != "" {
			op.set("operationId", opID)
		}
		if rec.deprecated {
			op.set("deprecated", true)
		}
		if rec.public && len(d.cfg.Auth) > 0 {
			op.set("security", []any{})
		}
		if len(rec.tags) > 0 {
			op.set("tags", toAny(rec.tags))
		}
		// path params from the resolved pattern, query params from Req tags
		var parameters []any
		parameters = append(parameters, pathParamObjs(rec.full)...)
		qp, hasBody := taggedParams(rec.req, sr)
		parameters = append(parameters, qp...)
		if len(parameters) > 0 {
			op.set("parameters", parameters)
		}
		if hasBody && rec.req != nil && !isEmptyStruct(rec.req) {
			ct, media := requestBodyFor(rec.req, sr)
			op.set("requestBody", newObj().
				set("required", true).
				set("content", newObj().set(ct, media)))
		}
		op.set("responses", d.responsesFor(rec, sr))
		if rec.omit {
			continue
		}
		docP := docPath(rec.full)
		pathItem, _ := paths.get(docP)
		if pathItem == nil {
			pathItem = newObj()
			paths.set(docP, pathItem)
		}
		pathItem.(*obj).set(strings.ToLower(rec.method), op)
	}
	spec.set("paths", paths)

	if len(d.cfg.Tags) > 0 {
		tags := make([]any, 0, len(d.cfg.Tags))
		for _, t := range d.cfg.Tags {
			e := newObj().set("name", t.Name)
			if t.Description != "" {
				e.set("description", t.Description)
			}
			tags = append(tags, e)
		}
		spec.set("tags", tags)
	}

	// security: one named scheme, top-level requirement; per-op override
	// on public routes (security: []).
	components := newObj()
	if len(d.cfg.Auth) > 0 {
		schemes := newObj()
		alts := make([]any, 0, len(d.cfg.Auth))
		for _, a := range d.cfg.Auth {
			schemes.set(a.Name, securitySchemeObj(a))
			alts = append(alts, newObj().set(a.Name, []any{}))
		}
		components.set("securitySchemes", schemes)
		spec.set("security", alts)
	}
	if len(sr.order) > 0 {
		schemas := newObj()
		for _, t := range sr.order {
			e := sr.byType[t]
			schemas.set(e.name, e.s)
		}
		for _, n := range sr.nameOrder {
			if schemas.has(n) {
				continue
			}
			schemas.set(n, sr.byName[n])
		}
		components.set("schemas", schemas)
	}
	if len(components.keys) > 0 {
		spec.set("components", components)
	}
	if d.cfg.ExternalDocs != nil {
		ed := newObj().set("url", d.cfg.ExternalDocs.URL)
		if d.cfg.ExternalDocs.Description != "" {
			ed.set("description", d.cfg.ExternalDocs.Description)
		}
		spec.set("externalDocs", ed)
	}
	return spec, nil
}

// operationID derives a deterministic id from method+path:
// GET /onboarding/{id}/sync -> getOnboardingByIdSync
func operationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg == "" {
			continue
		}
		seg = strings.Trim(seg, "{}")
		// braces with nothing to name: no derivable id
		if seg == "" {
			return ""
		}
		b.WriteString(strings.ToUpper(seg[:1]) + seg[1:])
	}
	return b.String()
}

// checkOperationIDs fails loud when two operations would share an id:
// client codegen assumes uniqueness.
func checkOperationIDs(records []*routeRecord) error {
	seen := make(map[string]string)
	for _, rec := range records {
		if rec.omit {
			continue
		}
		id := rec.operationID
		if id == "" {
			id = operationID(rec.method, docPath(rec.full))
		}
		if id == "" {
			continue
		}
		if first, dup := seen[id]; dup {
			return fmt.Errorf("specout: duplicate operationId %s (%s and %s %s) — set OperationID on one route", id, first, rec.method, rec.full)
		}
		seen[id] = rec.method + " " + rec.full
	}
	return nil
}

// securitySchemeObj builds the named scheme entry from the doc-only
// AuthScheme declaration.
func securitySchemeObj(a AuthScheme) *obj {
	o := newObj()
	switch a.Type {
	case "httpBearer":
		o.set("type", "http").set("scheme", "bearer")
	case "apiKey":
		o.set("type", "apiKey").set("name", a.Key).set("in", a.In)
	case "openIdConnect":
		o.set("type", "openIdConnect").set("openIdConnectUrl", a.URL)
	default:
		o.set("type", a.Type)
	}
	return o
}

func (d *Generator) flatRecords() []*routeRecord {
	out := make([]*routeRecord, len(d.records))
	copy(out, d.records)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].method != out[j].method {
			return out[i].method < out[j].method
		}
		return out[i].pattern < out[j].pattern
	})
	return out
}
func isEmptyStruct(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Struct && t.NumField() == 0
}

// driftKey canonicalizes a served pattern for the recorder drift check.
// chi RoutePattern collapses trailing slashes (both /a and /a/ report
// "/a"), so served-side keys trim one trailing slash to match. Registered
// doc paths keep their exact form: /a and /a/ are distinct OpenAPI paths.
func driftKey(p string) string {
	p = docPath(p)
	if len(p) > 1 {
		return strings.TrimSuffix(p, "/")
	}
	return p
}

// docPath rewrites router regex constraints to plain OpenAPI templates:
// chi and gorilla report /items/{id:[0-9]+}, OpenAPI only knows {id}.
func docPath(p string) string {
	return pathParamRe.ReplaceAllString(p, "{$1}")
}

// isCatchAll reports patterns OpenAPI cannot express: trailing wildcards,
// both the chi/std form (/files/*) and the std multi-segment form
// (/files/{path...}).
func isCatchAll(p string) bool {
	return strings.Contains(p, "*") || strings.Contains(p, "...}")
}

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

// statusMap is one implementation for both views so the drift check and the
// emitted spec cannot diverge (responsesFor stamps the same defaults).
func (d *Generator) statusMap(withDefaults bool) (map[RouteKey]map[int]bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.resolveLocked(); err != nil {
		return nil, err
	}
	records := d.flatRecords()
	out := make(map[RouteKey]map[int]bool)
	for _, rec := range records {
		if rec.full == "" {
			continue
		}
		key := RouteKey{Method: rec.method, Path: driftKey(rec.full)}
		codes := out[key]
		if codes == nil {
			codes = make(map[int]bool)
			out[key] = codes
		}
		if isEmptyStruct(rec.res) {
			// struct{} Res declares nothing on its own; explicit entries
			// replace it entirely (e.g. a 200 binary download).
			hasExplicit := false
			for _, resp := range rec.responses {
				if !resp.Omit {
					hasExplicit = true
				}
			}
			if !hasExplicit {
				codes[204] = true
			}
		} else {
			codes[200] = true
		}
		omitted := map[int]bool{}
		for _, resp := range rec.responses {
			if !resp.Omit {
				codes[resp.Status] = true
			} else {
				omitted[resp.Status] = true
				delete(codes, resp.Status)
			}
		}
		// global defaults, exactly as responsesFor stamps them
		if withDefaults && reflect.TypeOf(d.cfg.ErrorType) != nil {
			for _, code := range d.cfg.DefaultErrors {
				if codes[code] || omitted[code] {
					continue
				}
				codes[code] = true
			}
		}
	}
	return out, nil
}

// responsesFor assembles the responses object for one operation: the Res
// default (200+ref or 204 for NoContent), explicit Responses entries, and
// global DefaultErrors stamped with the ErrorType shape.
func (d *Generator) responsesFor(rec *routeRecord, sr *schemaRegistry) *obj {
	out := newObj()
	ordered := []int{}
	statuses := map[int]*obj{}
	omitted := map[int]bool{}

	add := func(code int, body *obj) {
		if _, ok := statuses[code]; !ok {
			ordered = append(ordered, code)
		}
		statuses[code] = body
	}

	// default from Res
	if isEmptyStruct(rec.res) {
		// struct{} Res declares nothing on its own; any explicit Response
		// replaces it (e.g. {Status:200, ContentType:"application/pdf"}).
		hasExplicit := false
		for _, resp := range rec.responses {
			if !resp.Omit {
				hasExplicit = true
			}
		}
		if !hasExplicit {
			add(204, newObj().set("description", "No content"))
		}
	} else {
		ref := sr.refFor(rec.res)
		add(200, refResponse("OK", ref))
	}
	for _, resp := range rec.responses {
		if resp.Omit {
			omitted[resp.Status] = true
			delete(statuses, resp.Status)
			for i, c := range ordered {
				if c == resp.Status {
					ordered = append(ordered[:i], ordered[i+1:]...)
					break
				}
			}
			continue
		}
		t := rec.res
		if resp.Type != nil {
			t = reflect.TypeOf(resp.Type)
		}
		body := contentResponse(resp.Status, t, sr)
		if resp.ContentType != "" {
			ct := resp.ContentType
			if ct == "binary" {
				ct = "application/octet-stream"
			}
			body.set("content", newObj().set(ct,
				newObj().set("schema", newObj().set("type", "string").set("format", "binary"))))
		}
		if len(resp.Headers) > 0 {
			hdrs := newObj()
			for _, hd := range resp.Headers {
				hdrs.set(hd.Name, headerObj(hd, sr))
			}
			body.set("headers", hdrs)
		}
		if len(resp.Raw) > 0 {
			body = mergeRaw(body, resp.Raw)
		}
		add(resp.Status, body)
	}
	// global defaults: only for codes not already declared per-route
	errType := reflect.TypeOf(d.cfg.ErrorType)
	for _, code := range d.cfg.DefaultErrors {
		if _, ok := statuses[code]; ok {
			continue
		}
		if omitted[code] {
			continue
		}
		if errType != nil {
			add(code, contentResponse(code, errType, sr))
		}
	}
	for _, code := range ordered {
		if _, ok := statuses[code]; ok {
			out.set(strconv.Itoa(code), statuses[code])
		}
	}
	return out
}

// mergeRaw splices arbitrary OpenAPI fragments into a response object.
func mergeRaw(base *obj, raw map[string]any) *obj {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		base.set(k, raw[k])
	}
	return base
}

func contentResponse(code int, t reflect.Type, sr *schemaRegistry) *obj {
	if t == nil || isEmptyStruct(t) {
		return newObj().set("description", http.StatusText(code))
	}
	ref := sr.refFor(t)
	return refResponse(http.StatusText(code), ref)
}

func refResponse(desc, ref string) *obj {
	return newObj().set("description", desc).set("content",
		newObj().set("application/json",
			newObj().set("schema", newObj().set("$ref", ref))))
}

// headerObj builds one OpenAPI header object: bare Name = string schema;
// Type set = the type schema (components dedupe applies).
func headerObj(hd Header, sr *schemaRegistry) *obj {
	if hd.Type == nil {
		return newObj().set("schema", newObj().set("type", "string"))
	}
	return newObj().set("schema", newObj().set("$ref", sr.refFor(reflect.TypeOf(hd.Type))))
}

func toAny[T any](s []T) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
