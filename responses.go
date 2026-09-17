package specout

import (
	"net/http"
	"reflect"
	"strconv"
)

// respEntry is one response a route derives: its emitted key, the concrete code
// it names (0 for a range or "default" key), the body type, and the declaration
// that asked for it (nil for the Res default and the global error envelope).
type respEntry struct {
	key  string
	code int
	typ  reflect.Type
	resp *Response
}

// responsePlan derives a route's responses once, in emission order: the Res
// default, then each explicit Response, then the DefaultErrors envelope for
// codes the route did not declare. The emitted responses object (responsesFor)
// and the two drift-check views (statusMap) both read this one walk, so the
// document and the drift check cannot disagree about what a route returns.
func (d *Generator) responsePlan(rec *routeRecord, withDefaults bool) []respEntry {
	var order []string
	seen := map[string]bool{}
	byKey := map[string]respEntry{}
	omitted := map[string]bool{}
	put := func(e respEntry) {
		if !seen[e.key] {
			seen[e.key] = true
			order = append(order, e.key)
		}
		delete(omitted, e.key)
		byKey[e.key] = e
	}

	// default from Res
	if isEmptyStruct(rec.res) {
		// struct{} Res declares nothing on its own; any explicit Response
		// replaces it (e.g. {Status:200, ContentType:"application/pdf"}).
		if !anyExplicit(rec.responses) {
			put(respEntry{key: "204", code: 204})
		}
	} else {
		put(respEntry{key: "200", code: 200, typ: rec.res})
	}

	for i := range rec.responses {
		resp := &rec.responses[i]
		key := responseKey(resp.Status, resp.Key)
		if resp.Omit {
			// a later explicit entry with the same key un-omits it
			omitted[key] = true
			continue
		}
		t := rec.res
		if resp.Type != nil {
			t = reflect.TypeOf(resp.Type)
		}
		// 204 and 304 carry no body: inheriting Res would emit content the
		// HTTP spec forbids. An explicit Type still wins.
		if resp.Type == nil && (key == "204" || key == "304") {
			t = nil
		}
		put(respEntry{key: key, code: resp.Status, typ: t, resp: resp})
	}

	// global defaults, exactly as the drift maps count them
	if withDefaults {
		if et := reflect.TypeOf(d.cfg.ErrorType); et != nil {
			for _, code := range d.cfg.DefaultErrors {
				key := responseKey(code, "")
				if omitted[key] {
					continue
				}
				if _, declared := byKey[key]; declared {
					continue
				}
				put(respEntry{key: key, code: code, typ: et})
			}
		}
	}

	out := make([]respEntry, 0, len(order))
	for _, key := range order {
		if !omitted[key] {
			out = append(out, byKey[key])
		}
	}
	return out
}

// responsesFor renders a route's derived responses as the operation's
// responses object, in plan order.
func (d *Generator) responsesFor(rec *routeRecord, sr *schemaRegistry) *obj {
	out := newObj()
	for _, e := range d.responsePlan(rec, true) {
		out.set(e.key, responseBody(e, sr))
	}
	return out
}

// responseBody builds one emitted response object.
func responseBody(e respEntry, sr *schemaRegistry) *obj {
	resp := e.resp
	if resp == nil {
		// the Res default, or an envelope entry: a nil type is NoContent's
		// "No content", anything else carries its schema.
		if e.typ == nil {
			return newObj().set("description", "No content")
		}
		return contentResponse(e.code, e.typ, sr)
	}
	body := contentResponse(e.code, e.typ, sr)
	if resp.Status == 0 && resp.Key != "" && resp.Key != "default" {
		// a range has no status text of its own; published documents carry
		// their own wording in Raw. OpenAPI requires the field, so a
		// placeholder beats an empty string.
		body.set("description", resp.Key+" response")
	}
	if resp.ContentType != "" {
		ct := resp.ContentType
		if ct == "binary" {
			ct = "application/octet-stream"
		}
		body.set("content", newObj().set(ct,
			newObj().set("schema", binarySchema())))
	}
	if len(resp.ContentTypes) > 0 {
		// one body under several media types: published documents write the
		// same shape as JSON and XML. The schema is the same one
		// contentResponse would use.
		if e.typ == nil || isEmptyStruct(e.typ) {
			panic("specout: Response.ContentTypes needs a response type (the Res parameter or Response.Type)")
		}
		content := newObj()
		for _, ct := range resp.ContentTypes {
			content.set(ct, newObj().set("schema", schemaFor(e.typ, sr)))
		}
		body.set("content", content)
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
	return body
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
	for _, rec := range d.flatRecords() {
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

// anyExplicit reports whether a route declares a response of its own. One
// non-omitted entry is enough to replace the Res default (e.g. a 200 binary
// download on a handler whose Res is struct{}).
func anyExplicit(responses []Response) bool {
	for _, r := range responses {
		if !r.Omit {
			return true
		}
	}
	return false
}

func isEmptyStruct(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Struct && t.NumField() == 0
}

// responseKey names a response object key: Key when the caller set one
// (default or an NXX range), else the status number. Status 0 means the
// OpenAPI "default" response: real published specs (petstore, MS Graph,
// GitHub) carry one on most operations, and OpenAPI 3.1 requires a numeric
// code or "default" — "0" is neither, so it validates against nothing. Any
// other code outside 100-599 is a typo, not a status.
func responseKey(code int, key string) string {
	if key != "" {
		if key == "default" {
			return key
		}
		if _, _, ok := rangeOf(key); !ok {
			panic("specout: Response.Key must be default or a range like 4XX, got " + key)
		}
		return key
	}
	if code == 0 {
		return "default"
	}
	if code < 100 || code > 599 {
		panic("specout: response status must be 0 (default) or 100-599, got " + strconv.Itoa(code))
	}
	return strconv.Itoa(code)
}

// rangeOf parses a range response key ("4XX") into its bounds.
func rangeOf(key string) (lo, hi int, ok bool) {
	if len(key) != 3 || key[1] != 'X' || key[2] != 'X' || key[0] < '1' || key[0] > '5' {
		return 0, 0, false
	}
	lo = int(key[0]-'0') * 100
	return lo, lo + 99, true
}

// mergeRaw splices arbitrary OpenAPI fragments into a response object.
func mergeRaw(base *obj, raw map[string]any) *obj {
	for _, k := range sortedKeys(raw) {
		base.set(k, raw[k])
	}
	return base
}

func contentResponse(code int, t reflect.Type, sr *schemaRegistry) *obj {
	if t == nil || isEmptyStruct(t) {
		return newObj().set("description", http.StatusText(code))
	}
	return newObj().set("description", http.StatusText(code)).
		set("content", newObj().set("application/json",
			newObj().set("schema", schemaFor(t, sr))))
}

// headerObj builds one OpenAPI header object: bare Name = string schema;
// Type set = the type schema (components dedupe applies).
func headerObj(hd Header, sr *schemaRegistry) *obj {
	if hd.Type == nil {
		return newObj().set("schema", newObj().set("type", "string"))
	}
	return newObj().set("schema", schemaFor(reflect.TypeOf(hd.Type), sr))
}
