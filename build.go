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
	records := d.flatRecords()

	// resolve chi paths: walk the root router once
	hits, err := d.walkAll()
	if err != nil {
		return nil, fmt.Errorf("chi walk: %w", err)
	}

	var unresolved []string
	for _, rec := range records {
		if hit, ok := hits[rkey{reflect.ValueOf(rec.fn).Pointer(), rec.method}]; ok {
			rec.full = hit
		} else if rec.full == "" {
			unresolved = append(unresolved, rec.method+" "+rec.pattern)
		}
	}
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("specout: unresolved routes: %s", strings.Join(unresolved, ", "))
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
			e.set("description", s.Description) // empty description omitted by omitempty-free writer? keep simple
			servers = append(servers, e)
		}
		spec.set("servers", servers)
	}

	paths := newObj()
	sr := newSchemaRegistry()

	for _, rec := range records {
		op := newObj()
		if rec.summary != "" {
			op.set("summary", rec.summary)
		}
		if rec.deprecated {
			op.set("deprecated", true)
		}
		tags := rec.tags
		if d.cfg.DeriveTags && len(tags) == 0 {
			tags = []string{deriveTag(rec.full)}
		}
		if len(tags) > 0 {
			op.set("tags", toAny(tags))
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

func deriveTag(path string) string {
	seg := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
	return strings.SplitN(seg, "{", 2)[0]
}

// responsesFor assembles the responses object for one operation: the Res
// default (200+ref or 204 for NoContent), explicit Responses entries, and
// global DefaultErrors stamped with the ErrorType shape.
func (d *Generator) responsesFor(rec *routeRecord, sr *schemaRegistry) *obj {
	out := newObj()
	ordered := []int{}
	statuses := map[int]*obj{}

	add := func(code int, body *obj) {
		if _, ok := statuses[code]; !ok {
			ordered = append(ordered, code)
		}
		statuses[code] = body
	}

	// default from Res
	if rec.res == reflect.TypeFor[NoContent]() {
		add(204, newObj().set("description", "No content"))
	} else {
		ref := sr.refFor(rec.res)
		add(200, refResponse("OK", ref))
	}
	for _, resp := range rec.responses {
		if resp.Omit {
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
		add(resp.Status, contentResponse(resp.Status, t, sr))
	}
	// global defaults: only for codes not already declared per-route
	errType := reflect.TypeOf(d.cfg.ErrorType)
	for _, code := range d.cfg.DefaultErrors {
		if _, ok := statuses[code]; ok {
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

func contentResponse(code int, t reflect.Type, sr *schemaRegistry) *obj {
	if t == nil || t == reflect.TypeFor[NoContent]() {
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
