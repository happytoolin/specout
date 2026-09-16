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
	if d.cfg.TermsOfService != "" {
		info.set("termsOfService", d.cfg.TermsOfService)
	}
	if c := d.cfg.Contact; c != nil {
		info.set("contact", strObj("name", c.Name, "url", c.URL, "email", c.Email))
	}
	if l := d.cfg.License; l != nil {
		info.set("license", strObj("name", l.Name, "url", l.URL))
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
		if rec.externalDocs != nil {
			op.set("externalDocs", externalDocsObj(rec.externalDocs))
		}
		if rec.public && len(d.cfg.Auth) > 0 {
			op.set("security", []any{})
		}
		if len(rec.tags) > 0 {
			op.set("tags", toAny(rec.tags))
		}
		// path params from the resolved pattern, query params from Req tags
		var parameters []any
		parameters = append(parameters, pathParamObjs(rec.full, rec.req)...)
		parameters = append(parameters, taggedParams(rec.req, sr)...)
		if len(parameters) > 0 {
			op.set("parameters", parameters)
		}
		// the body is Req minus its parameter fields; nil means no body
		if bt, name := sr.bodyType(rec.req); bt != nil {
			if name != "" {
				sr.overrideName(bt, name)
			}
			ct, media := requestBodyFor(bt, sr)
			content := newObj()
			if len(rec.reqContentTypes) > 0 {
				// one body shape under several media types
				for _, c := range rec.reqContentTypes {
					content.set(c, media)
				}
			} else {
				content.set(ct, media)
			}
			op.set("requestBody", newObj().
				set("required", true).
				set("content", content))
		}
		op.set("responses", d.responsesFor(rec, sr))
		if len(rec.raw) > 0 {
			op = mergeRaw(op, rec.raw)
		}
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
			if t.ExternalDocs != nil {
				e.set("externalDocs", externalDocsObj(t.ExternalDocs))
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
		spec.set("externalDocs", externalDocsObj(d.cfg.ExternalDocs))
	}
	return spec, nil
}

// strObj builds an object from name/value pairs, skipping empty values.
// Every one of these (contact, license, externalDocs) is optional per field.
func strObj(pairs ...string) *obj {
	o := newObj()
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			o.set(pairs[i], pairs[i+1])
		}
	}
	return o
}

// externalDocsObj builds an externalDocs object. OpenAPI requires the url
// field, so an empty one is a mistake rather than an omission.
func externalDocsObj(ed *ExternalDocs) *obj {
	if ed.URL == "" {
		panic("specout: ExternalDocs needs a URL")
	}
	return strObj("url", ed.URL, "description", ed.Description)
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
// AuthScheme declaration. A typo in Type, Key, In or URL would emit a
// securityScheme object the OpenAPI validator rejects (type must be one of
// apiKey/http/mutualTLS/oauth2/openIdConnect; apiKey needs a real in), so
// the declaration is checked here rather than shipped broken.
func securitySchemeObj(a AuthScheme) *obj {
	if a.Name == "" {
		panic("specout: AuthScheme.Name must not be empty (it is the securitySchemes key)")
	}
	o := newObj()
	switch a.Type {
	case "httpBearer":
		o.set("type", "http").set("scheme", "bearer")
	case "apiKey":
		if a.Key == "" {
			panic("specout: AuthScheme " + a.Name + " is apiKey but has no Key (the parameter name)")
		}
		switch a.In {
		case InHeader, InQuery, InCookie:
		default:
			panic("specout: AuthScheme " + a.Name + " has in=" + a.In + ", must be header, query or cookie")
		}
		o.set("type", "apiKey").set("name", a.Key).set("in", a.In)
	case "oauth2":
		if len(a.Flows) == 0 {
			panic("specout: AuthScheme " + a.Name + " is oauth2 but has no Flows")
		}
		o.set("type", "oauth2").set("flows", oauth2Flows(a))
	case "openIdConnect":
		if a.URL == "" {
			panic("specout: AuthScheme " + a.Name + " is openIdConnect but has no URL")
		}
		o.set("type", "openIdConnect").set("openIdConnectUrl", a.URL)
	default:
		panic("specout: AuthScheme " + a.Name + " has type " + a.Type + ", must be httpBearer, apiKey, oauth2 or openIdConnect")
	}
	return o
}

// oauth2Flows emits an oauth2 scheme's flows object. Flow and scope names are
// sorted: a Go map has no order and the document must stay byte-stable.
func oauth2Flows(a AuthScheme) *obj {
	flows := newObj()
	for _, name := range sortedKeys(a.Flows) {
		f := a.Flows[name]
		fo := newObj()
		// OpenAPI requires a URL per flow: implicit and authorizationCode
		// authorize in the browser, the other two take credentials directly.
		switch name {
		case "implicit":
			requireURL(a, name, "AuthorizationURL", f.AuthorizationURL)
			fo.set("authorizationUrl", f.AuthorizationURL)
		case "password", "clientCredentials":
			requireURL(a, name, "TokenURL", f.TokenURL)
			fo.set("tokenUrl", f.TokenURL)
		case "authorizationCode":
			requireURL(a, name, "AuthorizationURL", f.AuthorizationURL)
			requireURL(a, name, "TokenURL", f.TokenURL)
			fo.set("authorizationUrl", f.AuthorizationURL).set("tokenUrl", f.TokenURL)
		default:
			panic("specout: AuthScheme " + a.Name + " has flow " + name + ", must be implicit, password, clientCredentials or authorizationCode")
		}
		if f.RefreshURL != "" {
			fo.set("refreshUrl", f.RefreshURL)
		}
		scopes := newObj()
		for _, s := range sortedKeys(f.Scopes) {
			scopes.set(s, f.Scopes[s])
		}
		flows.set(name, fo.set("scopes", scopes))
	}
	return flows
}

func requireURL(a AuthScheme, flow, field, url string) {
	if url == "" {
		panic("specout: AuthScheme " + a.Name + " flow " + flow + " has no " + field)
	}
}

// sortedKeys returns a string-keyed map's keys in sorted order: anything
// driven by a Go map would otherwise emit a different document per run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (d *Generator) flatRecords() []*routeRecord {
	out := make([]*routeRecord, len(d.records))
	copy(out, d.records)
	// Registration order: paths come out in the order the code declares them,
	// so the document reads like the router. Sorting here put every DELETE
	// first and scattered one resource across the paths object.
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
			if resp.Omit {
				omitted[resp.Status] = true
				delete(codes, resp.Status)
				continue
			}
			// a range key (4XX) is 100 codes at once: the spec allows the
			// whole range, and nothing per-code is declared for it, so the
			// coverage check requires nothing from it. Status names one
			// concrete code of the range when the caller sets it.
			if lo, hi, ok := rangeOf(resp.Key); ok {
				if resp.Status != 0 {
					// the caller named one code of the range for coverage
					codes[resp.Status] = true
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
			if resp.Status == 0 && !withDefaults {
				continue
			}
			codes[resp.Status] = true
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
	// keyed by the emitted response key, not the status: {Status: 0, Key:
	// "4XX"} and {Status: 0, Key: "5XX"} are two entries with one status.
	ordered := []string{}
	statuses := map[string]*obj{}
	omitted := map[string]bool{}

	add := func(key string, body *obj) {
		if _, ok := statuses[key]; !ok {
			ordered = append(ordered, key)
		}
		statuses[key] = body
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
			add(responseKey(204, ""), newObj().set("description", "No content"))
		}
	} else {
		// schemaFor inlines a scalar or a map of scalars (real specs write the
		// object body out) and $refs everything else; the description matches
		// the old refResponse exactly, so no diff for named struct responses.
		add(responseKey(200, ""), contentResponse(200, rec.res, sr))
	}
	for _, resp := range rec.responses {
		key := responseKey(resp.Status, resp.Key)
		if resp.Omit {
			omitted[key] = true
			delete(statuses, key)
			for i, c := range ordered {
				if c == key {
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
		if resp.Status == 0 && resp.Key != "" && resp.Key != "default" {
			// a range has no status text of its own; published documents
			// carry their own wording in Raw. OpenAPI requires the field,
			// so a placeholder beats an empty string.
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
			// one body under several media types: published documents write
			// the same shape as JSON and XML. The schema is the same one
			// contentResponse would use.
			if t == nil || isEmptyStruct(t) {
				panic("specout: Response.ContentTypes needs a response type (the Res parameter or Response.Type)")
			}
			content := newObj()
			for _, ct := range resp.ContentTypes {
				content.set(ct, newObj().set("schema", schemaFor(t, sr)))
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
		add(key, body)
	}
	// global defaults: only for codes not already declared per-route
	errType := reflect.TypeOf(d.cfg.ErrorType)
	for _, code := range d.cfg.DefaultErrors {
		key := responseKey(code, "")
		if _, ok := statuses[key]; ok {
			continue
		}
		if omitted[key] {
			continue
		}
		if errType != nil {
			add(key, contentResponse(code, errType, sr))
		}
	}
	for _, key := range ordered {
		if body, ok := statuses[key]; ok {
			out.set(key, body)
		}
	}
	return out
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

func toAny[T any](s []T) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
