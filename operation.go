package specout

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// operationFor builds one operation object: the metadata fields, the path and
// query parameters, the request body and the responses. build places the
// result at paths[docPath][method] unless the record is a catch-all.
func (d *Generator) operationFor(rec *routeRecord, sr *schemaRegistry) *obj {
	op := newObj()
	op.setIf("summary", rec.summary)
	op.setIf("description", rec.description)
	// a malformed segment ({}) derives no id: omit the field rather than
	// emit an empty, unusable operationId
	op.setIf("operationId", emittedOperationID(rec))
	if rec.deprecated {
		op.set("deprecated", true)
	}
	if rec.externalDocs != nil {
		op.set("externalDocs", externalDocsObj(rec.externalDocs))
	}
	if security := d.routeSecurity(rec); security != nil {
		op.set("security", security)
	}
	if len(rec.tags) > 0 {
		op.set("tags", rec.tags)
	}
	// path params from the resolved pattern, query params from Req tags
	params := slices.Concat(sr.pathParamObjs(rec.full, rec.req), sr.taggedParams(rec.req))
	if len(params) > 0 {
		op.set("parameters", params)
	}
	// the body is Req minus its parameter fields; no body type means no body
	if bt, name := sr.bodyType(rec.req); bt != nil {
		if name != "" {
			sr.overrides[bt] = name
		}
		op.set("requestBody", requestBodyObj(rec, bt, sr))
	}
	op.set("responses", d.responsesFor(rec, sr))
	return mergeRaw(op, rec.raw)
}

// checkRoute panics on route data that would break the document: Raw keys,
// request media types and responses. Every binder calls it through addRoute
// before the route reaches the router, so the stack names the route and a bad
// route leaves no record. Checks that need the resolved path or the other
// routes run at build.
func (d *Generator) checkRoute(rec *routeRecord) {
	checkRawKeys(rec.raw, operationFields)
	checkRequestBody(rec)
	d.checkSecurity(rec)
	d.checkResponses(rec)
}

// braces removes the parameter braces from a path segment.
var braces = strings.NewReplacer("{", "", "}", "")

// operationID derives a deterministic id from method+path:
// GET /onboarding/{id}/sync -> getOnboardingByIdSync.
func operationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for seg := range strings.SplitSeq(strings.Trim(path, "/"), "/") {
		if seg == "" {
			continue
		}
		// every brace goes, so /files/{name}.{ext} gives FilesName.ext
		seg = braces.Replace(seg)
		// braces with nothing to name: no derivable id
		if seg == "" {
			return ""
		}
		_, size := utf8.DecodeRuneInString(seg)
		b.WriteString(strings.ToUpper(seg[:size]))
		b.WriteString(seg[size:])
	}
	return b.String()
}

// emittedOperationID is the operationId the document carries for rec. A Raw
// operationId wins, because mergeRaw applies Raw last.
func emittedOperationID(rec *routeRecord) string {
	if id, ok := rec.raw["operationId"].(string); ok {
		return id
	}
	return cmp.Or(rec.operationID, operationID(rec.method, docPath(rec.full)))
}

// checkOperationIDs fails loud when two operations would share an id:
// client codegen assumes uniqueness.
func checkOperationIDs(records []*routeRecord) error {
	seen := make(map[string]string)
	for _, rec := range records {
		if rec.isCatchAll() {
			continue
		}
		id := emittedOperationID(rec)
		if id == "" {
			continue
		}
		if first, dup := seen[id]; dup {
			return fmt.Errorf(
				"specout: duplicate operationId %s (%s and %s %s) — set OperationID on one route",
				id, first, rec.method, rec.full,
			)
		}
		seen[id] = rec.method + " " + rec.full
	}
	return nil
}
