package specout

import (
	"cmp"
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
	p := plannedResponses{byKey: map[string]respEntry{}, omitted: map[string]bool{}}
	if e, ok := resDefault(rec); ok {
		p.put(e)
	}
	for i := range rec.responses {
		resp := &rec.responses[i]
		key := responseKey(resp.Status, resp.Key)
		if resp.Omit {
			// a later explicit entry with the same key un-omits it
			p.omitted[key] = true
			continue
		}
		p.put(respEntry{key: key, code: resp.Status, typ: declaredType(rec.res, resp), resp: resp})
	}
	// global defaults, exactly as the drift maps count them
	if withDefaults {
		d.putDefaultErrors(&p)
	}
	return p.entries()
}

// plannedResponses collects a route's responses by key, in the order each key
// first appears. A later entry with the same key replaces the earlier one.
type plannedResponses struct {
	order   []string
	byKey   map[string]respEntry
	omitted map[string]bool
}

// put adds or replaces the entry for e.key, and un-omits the key.
func (p *plannedResponses) put(e respEntry) {
	if _, exists := p.byKey[e.key]; !exists {
		p.order = append(p.order, e.key)
	}
	delete(p.omitted, e.key)
	p.byKey[e.key] = e
}

// entries returns the entries that are not omitted, in key order.
func (p *plannedResponses) entries() []respEntry {
	out := make([]respEntry, 0, len(p.order))
	for _, key := range p.order {
		if !p.omitted[key] {
			out = append(out, p.byKey[key])
		}
	}
	return out
}

// resDefault is the response that Res declares on its own: 200 with Res as the
// body. A struct{} Res declares nothing on its own: any explicit Response
// replaces it (e.g. {Status:200, ContentType:"application/pdf"}), and with none
// the route answers 204.
func resDefault(rec *routeRecord) (respEntry, bool) {
	if !isEmptyStruct(rec.res) {
		return newRespEntry(http.StatusOK, rec.res), true
	}
	if anyExplicit(rec.responses) {
		return respEntry{}, false
	}
	return newRespEntry(http.StatusNoContent, nil), true
}

// declaredType is the body type of an explicit Response: its Type, else Res.
// 1xx, 204, 205 and 304 carry no body: inheriting Res would emit content the
// HTTP spec forbids, and checkResponse rejects a Type with a body there.
func declaredType(res reflect.Type, resp *Response) reflect.Type {
	if resp.Type != nil {
		return valueType(resp.Type)
	}
	if bodylessResponse(resp) {
		return nil
	}
	return res
}

// Raw replaces the entire responses object, including inherited defaults.
// Read its JSON keys so typed maps and json.RawMessage behave like plain maps.
func rawResponsePlan(raw any) []respEntry {
	responses := rawResponses(raw)
	var entries []respEntry
	for _, key := range sortedKeys(responses) {
		if !strings.HasPrefix(key, "x-") {
			entries = append(entries, rawEntry(key, responses[key]))
		}
	}
	if len(entries) == 0 {
		panic("specout: Raw.responses must declare a response")
	}
	return entries
}

// rawResponses reads Raw.responses as one JSON object.
func rawResponses(raw any) map[string]jsontext.Value {
	data, err := json.Marshal(raw)
	if err != nil {
		panic("specout: invalid Raw.responses: " + err.Error())
	}
	var responses map[string]jsontext.Value
	if err = json.Unmarshal(data, &responses); err != nil || responses == nil {
		panic("specout: Raw.responses must be an object")
	}
	return responses
}

// rawEntry checks one Raw.responses entry. Its key is default, a range, or a
// status code, and its value is a Response or Reference object.
func rawEntry(key string, v jsontext.Value) respEntry {
	e := respEntry{key: key}
	if _, _, isRange := rangeOf(key); key != defaultResponseKey && !isRange {
		code, err := strconv.Atoi(key)
		if err != nil || !isStatus(code) || strconv.Itoa(code) != key {
			panic("specout: invalid Raw.responses status " + key)
		}
		e.code = code
	}
	if v.Kind() != '{' {
		panic("specout: Raw.responses " + key + " must be a Response object")
	}
	return e
}

// isStatus reports whether code is an HTTP status code, 100-599.
func isStatus(code int) bool {
	return code >= 100 && code <= 599
}

// newRespEntry builds a response the route derived rather than declared.
func newRespEntry(code int, t reflect.Type) respEntry {
	return respEntry{key: responseKey(code, ""), code: code, typ: t}
}

// bodyless reports whether a status carries no body: RFC 9110 forbids content
// in a 1xx, 204, 205 or 304 response.
func bodyless(code int) bool {
	return (code >= http.StatusContinue && code < http.StatusOK) || code == http.StatusNoContent ||
		code == http.StatusResetContent || code == http.StatusNotModified
}

// bodylessResponse reports whether resp describes responses that carry no
// body. A Key describes a whole range or the default response, so only 1XX
// is bodyless; Status then names one code of it for coverage.
func bodylessResponse(resp *Response) bool {
	if resp.Key != "" {
		return resp.Key == "1XX"
	}
	return bodyless(resp.Status)
}

// putDefaultErrors adds the configured error responses for every code the route
// neither declared nor omitted.
func (d *Generator) putDefaultErrors(p *plannedResponses) {
	if d.cfg.ErrorType == nil {
		return
	}
	et := valueType(d.cfg.ErrorType)
	for _, code := range d.cfg.DefaultErrors {
		key := responseKey(code, "")
		_, declared := p.byKey[key]
		if p.omitted[key] || declared {
			continue
		}
		p.put(respEntry{key: key, code: code, typ: et})
	}
}

// checkDefaultErrors checks the error responses that Config adds to every
// route: ErrorType is a value, and each DefaultErrors entry is a status code
// that can carry it.
func (d *Generator) checkDefaultErrors() {
	if d.cfg.ErrorType == nil {
		return
	}
	valueType(d.cfg.ErrorType)
	for _, code := range d.cfg.DefaultErrors {
		responseKey(code, "")
		if bodyless(code) {
			panic("specout: DefaultErrors holds " + strconv.Itoa(code) + ", which carries no body for ErrorType")
		}
	}
}

// responsesFor renders a route's derived responses as the operation's
// responses object, in plan order. checkResponses checked them when the route
// was registered.
func (d *Generator) responsesFor(rec *routeRecord, sr *schemaRegistry) *obj {
	out := newObj()
	for _, e := range d.responsePlan(rec, true) {
		if rec.method == http.MethodHead {
			e = withoutBody(e)
		}
		out.set(e.key, responseBody(e, sr))
	}
	return out
}

// withoutBody returns e for a HEAD operation: the same status, description
// and headers, with no content. RFC 9110 gives a HEAD response no body.
func withoutBody(e respEntry) respEntry {
	e.typ = nil
	if e.resp != nil {
		resp := *e.resp
		resp.ContentType, resp.ContentTypes = "", nil
		e.resp = &resp
	}
	return e
}

// checkResponses panics on a route's responses when they would break the
// document. The plan itself checks the status codes, keys, Type values and
// Raw.responses.
func (d *Generator) checkResponses(rec *routeRecord) {
	plan := d.responsePlan(rec, true)
	// OpenAPI requires at least one response
	if len(plan) == 0 {
		panic("specout: " + rec.method + " " + rec.pattern + " omits every response; declare at least one")
	}
	for _, e := range plan {
		if e.resp != nil {
			checkResponse(e)
		}
	}
}

// checkResponse checks the content, headers and Raw keys of one declared
// Response.
func checkResponse(e respEntry) {
	resp := e.resp
	switch {
	case bodylessResponse(resp) && (resp.ContentType != "" || len(resp.ContentTypes) > 0 || hasBody(e.typ)):
		panic("specout: response " + e.key + " carries no body; remove its Type, ContentType or ContentTypes")
	case resp.ContentType != "" && len(resp.ContentTypes) > 0:
		panic("specout: Response sets both ContentType and ContentTypes; set one")
	case len(resp.ContentTypes) > 0 && !hasBody(e.typ):
		panic("specout: Response.ContentTypes needs a response type (the Res parameter or Response.Type)")
	}
	for _, ct := range resp.ContentTypes {
		mediaType(ct, "Response.ContentTypes")
	}
	checkHeaders(resp.Headers)
	checkRawKeys(resp.Raw, responseFields)
}

// checkHeaders checks the headers of one Response. HTTP header names ignore
// case, so X-Id and x-id are one header.
func checkHeaders(headers []Header) {
	seen := make(map[string]bool, len(headers))
	for _, hd := range headers {
		key := strings.ToLower(hd.Name)
		if hd.Name == "" || seen[key] {
			panic("specout: empty or duplicate response header " + strconv.Quote(hd.Name))
		}
		seen[key] = true
		if hd.Type != nil {
			valueType(hd.Type)
		}
	}
}

// responseBody builds one emitted response object.
func responseBody(e respEntry, sr *schemaRegistry) *obj {
	resp := e.resp
	if resp == nil {
		// the Res default, or an envelope entry: a nil type on 204 is
		// NoContent's "No content", anything else describes its status and
		// carries its schema. A HEAD operation has nil types on every code.
		if e.typ == nil && e.code == http.StatusNoContent {
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
	setDeclaredContent(body, e, sr)
	if len(resp.Headers) > 0 {
		body.set("headers", responseHeaders(resp.Headers, sr))
	}
	return mergeRaw(body, resp.Raw)
}

// setDeclaredContent replaces the inherited content of body with the media
// types that e's Response declares, if it declares any.
func setDeclaredContent(body *obj, e respEntry, sr *schemaRegistry) {
	switch resp := e.resp; {
	case resp.ContentType != "":
		body.set("content", binaryContent(resp.ContentType))
	case len(resp.ContentTypes) > 0:
		body.set("content", sharedContent(e.typ, resp.ContentTypes, sr))
	}
}

// binaryContent is the content object for Response.ContentType: one media type
// with a binary schema. "binary" is short for application/octet-stream.
func binaryContent(ct string) *obj {
	if ct == "binary" {
		ct = "application/octet-stream"
	}
	return newObj().set(ct, newObj().set("schema", binarySchema()))
}

// sharedContent is one body under several media types: published documents
// write the same shape as JSON and XML. The schema is the same one
// contentResponse would use.
func sharedContent(t reflect.Type, types []string, sr *schemaRegistry) *obj {
	content := newObj()
	for _, ct := range types {
		content.set(ct, newObj().set("schema", schemaFor(t, sr)))
	}
	return content
}

// mediaType panics when ct, a content map key, is empty.
func mediaType(ct, field string) {
	if ct == "" {
		panic("specout: " + field + " has an empty media type")
	}
}

// responseHeaders is the headers object of one response, in declared order.
func responseHeaders(headers []Header, sr *schemaRegistry) *obj {
	out := newObj()
	for _, hd := range headers {
		out.set(hd.Name, headerObj(hd, sr))
	}
	return out
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

// hasBody reports whether a response type puts content in the document.
func hasBody(t reflect.Type) bool {
	return t != nil && !isEmptyStruct(t)
}

// responseKey names a response object key: Key when the caller set one
// (default or an NXX range), else the status number. Status 0 means the
// OpenAPI "default" response: real published specs (petstore, MS Graph,
// GitHub) carry one on most operations, and OpenAPI 3.1 requires a numeric
// code or "default" — "0" is neither, so it validates against nothing. Any
// other code outside 100-599 is a typo, not a status.
func responseKey(code int, key string) string {
	if code != 0 && !isStatus(code) {
		panic("specout: response status must be 0 (default) or 100-599, got " + strconv.Itoa(code))
	}
	if key != "" {
		return checkedKey(code, key)
	}
	if code == 0 {
		return defaultResponseKey
	}
	return strconv.Itoa(code)
}

// checkedKey checks a Response.Key: default, or a range like 4XX that holds
// code when code is set.
func checkedKey(code int, key string) string {
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

// The fields an OpenAPI 3.1 Operation and Response object may carry. Any other
// key, except an x- extension, makes the document invalid.
var (
	operationFields = []string{
		"tags", "summary", "description", "externalDocs", "operationId", "parameters",
		"requestBody", "responses", "callbacks", "deprecated", "security", "servers",
	}
	responseFields = []string{"description", "headers", "content", "links"}
)

// mergeRaw splices arbitrary OpenAPI fragments into an operation or response
// object.
func mergeRaw(base *obj, raw map[string]any) *obj {
	for _, k := range sortedKeys(raw) {
		base.set(k, raw[k])
	}
	return base
}

// checkRawKeys panics on a Raw key that the object cannot carry. fields lists
// the keys that object may carry.
func checkRawKeys(raw map[string]any, fields []string) {
	for k := range raw {
		if !strings.HasPrefix(k, "x-") && !slices.Contains(fields, k) {
			panic("specout: Raw key " + strconv.Quote(k) + " is not an OpenAPI field or an x- extension")
		}
	}
}

func contentResponse(code int, t reflect.Type, sr *schemaRegistry) *obj {
	if t == nil || isEmptyStruct(t) {
		return newObj().set("description", statusText(code))
	}
	return newObj().set("description", statusText(code)).
		set("content", newObj().set("application/json",
			newObj().set("schema", schemaFor(t, sr))))
}

// statusText describes a response: the status text, or a placeholder for the
// default response and for a code Go has no text for. OpenAPI requires the
// field, and an empty one tells the reader nothing.
func statusText(code int) string {
	if code == 0 {
		return defaultResponseKey + " response"
	}
	return cmp.Or(http.StatusText(code), strconv.Itoa(code)+" response")
}

// headerObj builds one OpenAPI header object: bare Name = string schema;
// Type set = the type schema (components dedupe applies).
func headerObj(hd Header, sr *schemaRegistry) *obj {
	if hd.Type == nil {
		return newObj().set("schema", newObj().set("type", typeString))
	}
	return newObj().set("schema", schemaFor(valueType(hd.Type), sr))
}

// valueType is the type of the value in a Type field. A reflect.Type there is
// a mistake: it would document the reflect package's own rtype struct.
func valueType(v any) reflect.Type {
	if _, ok := v.(reflect.Type); ok {
		panic("specout: a Type field takes a value such as Error{}, not a reflect.Type")
	}
	return reflect.TypeOf(v)
}
