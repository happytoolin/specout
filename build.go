package specout

import (
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// build assembles the OpenAPI document: resolve chi paths via a single walk,
// merge std patterns, reflect schemas, emit operations in path order.
func (d *Generator) build() (*obj, error) {
	d.resolvePathsLocked()
	records := d.flatRecords()

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
			e.set("description", s.Description) // empty description omitted by omitempty-free writer? keep simple
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
		if rec.deprecated {
			op.set("deprecated", true)
		}
		if len(rec.tags) > 0 {
			op.set("tags", toAny(rec.tags))
		}
		// path params from the resolved pattern, query params from Req tags
		var parameters []any
		parameters = append(parameters, pathParamObjs(rec.full)...)
		qp, hasBody := queryParams(rec.req, sr)
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
		pathItem, _ := paths.get(rec.full)
		if pathItem == nil {
			pathItem = newObj()
			paths.set(rec.full, pathItem)
		}
		pathItem.(*obj).set(strings.ToLower(rec.method), op)
	}
	spec.set("paths", paths)
	if len(sr.order) > 0 {
		schemas := newObj()
		for _, t := range sr.order {
			e := sr.byType[t]
			schemas.set(e.name, e.s)
		}
		spec.set("components", newObj().set("schemas", schemas))
	}
	return spec, nil
}

func (d *Generator) flatRecords() []*routeRecord {
	keys := make([]uintptr, 0, len(d.routes))
	for k := range d.routes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	var out []*routeRecord
	for _, k := range keys {
		out = append(out, d.routes[k]...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].method != out[j].method {
			return out[i].method < out[j].method
		}
		return out[i].pattern < out[j].pattern
	})
	return out
}

// isEmptyStruct: Res = struct{} means 204, no body — Go's own idiom for
// "nothing". Replaces the old NoContent marker type.
func isEmptyStruct(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Struct && t.NumField() == 0
}

// NormalizePath canonicalizes route patterns for comparison: chi walks emit
// a trailing slash for "/" patterns inside Route groups, while RoutePattern
// at serve time does not. One shape everywhere.
func NormalizePath(p string) string {
	if len(p) > 1 {
		return strings.TrimSuffix(p, "/")
	}
	return p
}

// resolvePathsLocked stitches full paths via chi walks. Caller holds d.mu.
func (d *Generator) resolvePathsLocked() {
	if d.resolved {
		return
	}
	hits, err := d.walkAll()
	if err != nil {
		return
	}
	for _, recs := range d.routes {
		for _, rec := range recs {
			if hit, ok := hits[rkey{reflect.ValueOf(rec.fn).Pointer(), rec.method}]; ok {
				rec.full = hit
			}
		}
	}
	d.resolved = true
}

// DeclaredStatuses exposes the declared method+path -> codes map, for the
// recorder's drift check.
func (d *Generator) DeclaredStatuses() map[RouteKey]map[int]bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resolvePathsLocked()
	records := d.flatRecords()
	out := make(map[RouteKey]map[int]bool)
	for _, rec := range records {
		if rec.full == "" {
			continue
		}
		key := RouteKey{Method: rec.method, Path: NormalizePath(rec.full)}
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
		for _, resp := range rec.responses {
			if !resp.Omit {
				codes[resp.Status] = true
			} else {
				delete(codes, resp.Status)
			}
		}
	}
	return out
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

func toAny[T any](s []T) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
