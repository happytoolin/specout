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
	where := rec.label()
	if raw, ok := rec.raw["responses"]; ok {
		return rawResponsePlan(where, raw)
	}
	p := plannedResponses{byKey: map[string]respEntry{}, omitted: map[string]bool{}}
	if e, ok := resDefault(rec); ok {
		p.put(e)
	}
	for i := range rec.responses {
		resp := &rec.responses[i]
		if resp.Status == 0 && resp.Key == "" {
			panic("specout: " + where + `: a Response needs a Status, or a Key such as "default" or "4XX"`)
		}
		key := responseKey(where, resp.Status, resp.Key)
		if resp.Omit {
			// a later explicit entry with the same key un-omits it
			p.omitted[key] = true
			continue
		}
		p.put(respEntry{key: key, code: resp.Status, typ: declaredType(where+": response "+key, rec.res, resp), resp: resp})
	}
	// global defaults, exactly as the drift maps count them
	if withDefaults && !rec.outgoing {
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
// replaces it (e.g. {Status:200, Type:File{}}), and with none
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
func declaredType(where string, res reflect.Type, resp *Response) reflect.Type {
	if resp.Type != nil {
		return valueType(where, resp.Type)
	}
	if bodylessResponse(resp) {
		return nil
	}
	return res
}

// Raw replaces the entire responses object, including inherited defaults.
// Read its JSON keys so typed maps and json.RawMessage behave like plain maps.
func rawResponsePlan(where string, raw any) []respEntry {
	where += ": Raw.responses"
	responses := rawResponses(where, raw)
	var entries []respEntry
	for _, key := range sortedKeys(responses) {
		if !strings.HasPrefix(key, "x-") {
			entries = append(entries, rawEntry(where, key, responses[key]))
		}
	}
	if len(entries) == 0 {
		panic("specout: " + where + ": declare at least one response")
	}
	return entries
}

// rawResponses reads Raw.responses as one JSON object.
func rawResponses(where string, raw any) map[string]jsontext.Value {
	data, err := json.Marshal(raw)
	if err != nil {
		panic("specout: " + where + ": cannot be encoded as JSON: " + err.Error())
	}
	var responses map[string]jsontext.Value
	if err = json.Unmarshal(data, &responses); err != nil || responses == nil {
		panic("specout: " + where + ": must be a JSON object")
	}
	return responses
}

// rawEntry checks one Raw.responses entry. Its key is default, a range, or a
// status code, and its value is a Response or Reference object.
func rawEntry(where, key string, v jsontext.Value) respEntry {
	e := respEntry{key: key}
	if _, _, isRange := rangeOf(key); key != defaultResponseKey && !isRange {
		code, err := strconv.Atoi(key)
		if err != nil || !isStatus(code) || strconv.Itoa(code) != key {
			panic("specout: " + where + ": key " + strconv.Quote(key) +
				` is not valid; use a status, a range such as "4XX", or "default"`)
		}
		e.code = code
	}
	if v.Kind() != '{' {
		panic("specout: " + where + ": " + key + " must be a Response object")
	}
	return e
}

// isStatus reports whether code is an HTTP status code, 100-599.
func isStatus(code int) bool {
	return code >= 100 && code <= 599
}

// newRespEntry builds a response the route derived rather than declared.
func newRespEntry(code int, t reflect.Type) respEntry {
	return respEntry{key: responseKey("response", code, ""), code: code, typ: t}
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
	et := valueType("Config.ErrorType", d.cfg.ErrorType)
	for _, code := range d.cfg.DefaultErrors {
		key := responseKey("Config.DefaultErrors", code, "")
		_, declared := p.byKey[key]
		if p.omitted[key] || declared {
			continue
		}
		p.put(respEntry{key: key, code: code, typ: et})
	}
}

// checkDefaultErrors checks the error responses that Config adds to every
// route: ErrorType is a value, and each DefaultErrors entry is a status code
// that can carry it. DefaultErrors alone would add nothing, with no message.
func (d *Generator) checkDefaultErrors() {
	if d.cfg.ErrorType == nil {
		if len(d.cfg.DefaultErrors) > 0 {
			panic("specout: Config.DefaultErrors: set Config.ErrorType, the body of these responses")
		}
		return
	}
	valueType("Config.ErrorType", d.cfg.ErrorType)
	for _, code := range d.cfg.DefaultErrors {
		responseKey("Config.DefaultErrors", code, "")
		if bodyless(code) {
			panic("specout: Config.DefaultErrors: status " + strconv.Itoa(code) +
				" has no body, so it cannot carry ErrorType; remove it")
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
		resp.ContentTypes = nil
		resp.Example, resp.Examples = nil, nil
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
		panic("specout: " + rec.label() + ": every response is omitted; declare at least one")
	}
	for _, e := range plan {
		if e.resp != nil {
			checkResponse(rec.label()+": response "+e.key, e)
		}
	}
}

// checkResponse checks the content, headers and Raw keys of one declared
// Response. where names the response.
func checkResponse(where string, e respEntry) {
	resp := e.resp
	switch {
	case bodylessResponse(resp) && (len(resp.ContentTypes) > 0 || hasBody(e.typ)):
		panic("specout: " + where + ": this status has no body; remove Type and ContentTypes")
	case len(resp.ContentTypes) > 0 && !hasBody(e.typ):
		panic("specout: " + where + ": ContentTypes needs a body type; set Type, or use a Res with a body")
	}
	for _, ct := range resp.ContentTypes {
		mediaType(where, ct)
	}
	checkResponseExamples(where, e)
	checkHeaders(where, resp.Headers)
	linkObjs(where, resp.Links)
	checkRawKeys(where, resp.Raw, responseFields)
}

// checkResponseExamples checks that examples can be written, and that the
// response has a body for them to show.
func checkResponseExamples(where string, e respEntry) {
	resp := e.resp
	if resp.Example == nil && len(resp.Examples) == 0 {
		return
	}
	if !hasBody(e.typ) {
		panic("specout: " + where + ": sets an example, but this response has no body; set Type, or remove the example")
	}
	checkExamples(where, resp.Example, resp.Examples)
}

// checkHeaders checks the headers of one Response or Encoding. HTTP header names ignore
// case, so X-Id and x-id are one header.
func checkHeaders(where string, headers []Header) {
	seen := make(map[string]bool, len(headers))
	for _, hd := range headers {
		key := strings.ToLower(hd.Name)
		if hd.Name == "" {
			panic("specout: " + where + ": a Header needs a Name")
		}
		if seen[key] {
			panic("specout: " + where + ": header " + strconv.Quote(hd.Name) + " is repeated; header names ignore case")
		}
		seen[key] = true
		if hd.Type != nil {
			valueType(where+": header "+strconv.Quote(hd.Name), hd.Type)
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
	body.setIf("description", resp.Description)
	setDeclaredContent(body, e, sr)
	setResponseExamples(body, resp)
	if len(resp.Headers) > 0 {
		body.set("headers", headerObjs(resp.Headers, sr))
	}
	if len(resp.Links) > 0 {
		body.set("links", linkObjs("response "+e.key, resp.Links))
	}
	return mergeRaw(body, resp.Raw)
}

// setDeclaredContent replaces the inherited content of body with the media
// types that e's Response declares, if it declares any.
func setDeclaredContent(body *obj, e respEntry, sr *schemaRegistry) {
	if types := e.resp.ContentTypes; len(types) > 0 {
		body.set("content", sharedContent(e.typ, types, sr))
	}
}

// setResponseExamples writes the examples of resp on each of its media types.
func setResponseExamples(body *obj, resp *Response) {
	content, ok := body.get("content").(*obj)
	if !ok {
		return
	}
	for _, ct := range content.keys {
		if media, isObj := content.get(ct).(*obj); isObj {
			setExamples(media, "response", resp.Example, resp.Examples)
		}
	}
}

// sharedContent is one body under several media types: published documents
// write the same shape as JSON and XML. The schema is the same one
// contentResponse would use.
func sharedContent(t reflect.Type, types []string, sr *schemaRegistry) *obj {
	content := newObj()
	for _, ct := range types {
		content.set(ct, newObj().set("schema", responseSchema(t, sr)))
	}
	return content
}

// responseSchema is the schema of a response body: a File is binary.
func responseSchema(t reflect.Type, sr *schemaRegistry) any {
	if t == reflect.TypeFor[File]() {
		return binarySchema()
	}
	return schemaFor(t, sr)
}

// mediaType panics when ct, a content map key, is empty.
func mediaType(where, ct string) {
	if ct == "" {
		panic("specout: " + where + ": ContentTypes has an empty media type")
	}
}

// headerObjs is one headers object, in declared order.
func headerObjs(headers []Header, sr *schemaRegistry) *obj {
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

// isEmptyStruct reports a type with no body. File has no fields, but it is a
// binary body.
func isEmptyStruct(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Struct && t.NumField() == 0 && t != reflect.TypeFor[File]()
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
func responseKey(where string, code int, key string) string {
	if code != 0 && !isStatus(code) {
		panic("specout: " + where + ": status " + strconv.Itoa(code) + ` is not valid; use 100-599, or Key: "default"`)
	}
	if key != "" {
		return checkedKey(where, code, key)
	}
	if code == 0 {
		return defaultResponseKey
	}
	return strconv.Itoa(code)
}

// checkedKey checks a Response.Key: default, or a range like 4XX that holds
// code when code is set.
func checkedKey(where string, code int, key string) string {
	if key == defaultResponseKey {
		return key
	}
	lo, hi, ok := rangeOf(key)
	if !ok {
		panic("specout: " + where + ": Response.Key " + strconv.Quote(key) +
			` is not valid; use "default" or a range such as "4XX"`)
	}
	if code != 0 && (code < lo || code > hi) {
		panic("specout: " + where + ": status " + strconv.Itoa(code) + " is outside the range " + key)
	}
	return key
}

// The fields that Raw may replace on an OpenAPI 3.1 Operation and Response
// object. Any other key, except an x- extension, panics.
var (
	operationFields = []string{
		"tags", "summary", keyDescription, "externalDocs", "operationId", "parameters",
		"requestBody", "responses", keyDeprecated, "security",
	}
	responseFields = []string{keyDescription, "headers", "content"}
	// typedFields names the typed form of the fields that Raw does not take, so
	// the panic says what to write instead.
	typedFields = map[string]string{
		"jsonSchemaDialect": "Config.JSONSchemaDialect",
		"webhooks":          "Generator.Webhook",
		"callbacks":         "Handler.Callbacks and NewCallback",
		"servers":           "Config.Servers or Handler.Servers",
		"links":             "Response.Links",
	}
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
// the keys that object may carry. where names the object.
func checkRawKeys(where string, raw map[string]any, fields []string) {
	for _, k := range sortedKeys(raw) {
		if strings.HasPrefix(k, "x-") || slices.Contains(fields, k) {
			continue
		}
		if typed, ok := typedFields[k]; ok {
			panic("specout: " + where + ": Raw key " + strconv.Quote(k) + " has a typed form; use " + typed)
		}
		panic("specout: " + where + ": Raw key " + strconv.Quote(k) + " is not allowed here; start an extension with x-" +
			didYouMean(k, fields))
	}
}

func contentResponse(code int, t reflect.Type, sr *schemaRegistry) *obj {
	if t == nil || isEmptyStruct(t) {
		return newObj().set("description", statusText(code))
	}
	ct := "application/json"
	if t == reflect.TypeFor[File]() {
		ct = "application/octet-stream"
	}
	return newObj().set("description", statusText(code)).
		set("content", sharedContent(t, []string{ct}, sr))
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
	o := newObj().setIf("description", hd.Description)
	if hd.Required {
		o.set("required", true)
	}
	if hd.Deprecated {
		o.set("deprecated", true)
	}
	if hd.Type == nil {
		return o.set("schema", newObj().set("type", typeString))
	}
	return o.set("schema", schemaFor(valueType("header "+strconv.Quote(hd.Name), hd.Type), sr))
}

// valueType is the type of the value in a Type field. A reflect.Type there is
// a mistake: it would document the reflect package's own rtype struct.
func valueType(where string, v any) reflect.Type {
	if _, ok := v.(reflect.Type); ok {
		panic("specout: " + where + ": a Type field takes a value such as Problem{}, not a reflect.Type")
	}
	return reflect.TypeOf(v)
}
