package specout

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// build assembles the OpenAPI document: resolve chi paths via a single walk,
// merge std patterns, reflect schemas, emit operations in path order.
func (d *Generator) build() (*obj, error) {
	if err := d.resolveLocked(); err != nil {
		return nil, err
	}
	// No sort here: paths come out in the order the code declares them, so the
	// document reads like the router. Sorting put every DELETE first and
	// scattered one resource across the paths object.
	records := slices.Clone(d.records)
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
		pathItem := paths.get(docP)
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

func toAny[T any](s []T) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
