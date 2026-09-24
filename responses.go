package specout

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const defaultResponseKey = "default"

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
	if raw, ok := rec.raw["responses"]; ok {
		return rawResponsePlan(raw)
	}
	var order []string
	byKey := map[string]respEntry{}
	omitted := map[string]bool{}
	put := func(e respEntry) {
		if _, exists := byKey[e.key]; !exists {
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
			put(newRespEntry(http.StatusNoContent, nil))
		}
	} else {
		put(newRespEntry(http.StatusOK, rec.res))
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
		if resp.Type == nil && bodyless(resp.Status) {
			t = nil
		}
		put(respEntry{key: key, code: resp.Status, typ: t, resp: resp})
	}

	// global defaults, exactly as the drift maps count them
	if withDefaults {
		d.putDefaultErrors(put, byKey, omitted)
	}

	out := make([]respEntry, 0, len(order))
	for _, key := range order {
		if !omitted[key] {
			out = append(out, byKey[key])
		}
	}
	return out
}

// Raw replaces the entire responses object, including inherited defaults.
// Read its JSON keys so typed maps and json.RawMessage behave like plain maps.
func rawResponsePlan(raw any) []respEntry {
	data, err := json.Marshal(raw)
	if err != nil {
		panic("specout: invalid Raw.responses: " + err.Error())
	}
	var responses map[string]jsontext.Value
	if err := json.Unmarshal(data, &responses); err != nil || responses == nil {
		panic("specout: Raw.responses must be an object")
	}
	var entries []respEntry
	for _, key := range sortedKeys(responses) {
		if strings.HasPrefix(key, "x-") {
			continue
		}
		if _, _, isRange := rangeOf(key); key == defaultResponseKey || isRange {
			entries = append(entries, respEntry{key: key})
			continue
		}
		code, err := strconv.Atoi(key)
		if err != nil || code < 100 || code > 599 || strconv.Itoa(code) != key {
			panic("specout: invalid Raw.responses status " + key)
		}
		entries = append(entries, respEntry{key: key, code: code})
	}
	if len(entries) == 0 {
		panic("specout: Raw.responses must declare a response")
	}
	return entries
}

// newRespEntry builds a response the route derived rather than declared.
func newRespEntry(code int, t reflect.Type) respEntry {
	return respEntry{key: responseKey(code, ""), code: code, typ: t}
}

// bodyless reports whether a status carries no body.
func bodyless(code int) bool {
	return code == http.StatusNoContent || code == http.StatusNotModified
}

// putDefaultErrors adds the configured error responses for every code the route
// neither declared nor omitted.
func (d *Generator) putDefaultErrors(put func(respEntry), byKey map[string]respEntry, omitted map[string]bool) {
	et := reflect.TypeOf(d.cfg.ErrorType)
	if et == nil {
		return
	}
	for _, code := range d.cfg.DefaultErrors {
		key := responseKey(code, "")
		_, declared := byKey[key]
		if omitted[key] || declared {
			continue
		}
		put(respEntry{key: key, code: code, typ: et})
	}
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
	if resp.Status == 0 && resp.Key != "" && resp.Key != defaultResponseKey {
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
	return mergeRaw(body, resp.Raw)
}

// anyExplicit reports whether a route declares a response of its own. One
// non-omitted entry is enough to replace the Res default (e.g. a 200 binary
// download on a handler whose Res is struct{}).
func anyExplicit(responses []Response) bool {
	return slices.ContainsFunc(responses, func(r Response) bool { return !r.Omit })
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
	if code != 0 && (code < 100 || code > 599) {
		panic("specout: response status must be 0 (default) or 100-599, got " + strconv.Itoa(code))
	}
	if key != "" {
		if key == defaultResponseKey {
			return key
		}
		lo, hi, ok := rangeOf(key)
		if !ok {
			panic("specout: Response.Key must be default or a range like 4XX, got " + key)
		}
		if code != 0 && (code < lo || code > hi) {
			panic("specout: response status " + strconv.Itoa(code) + " is outside range " + key)
		}
		return key
	}
	if code == 0 {
		return defaultResponseKey
	}
	return strconv.Itoa(code)
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
