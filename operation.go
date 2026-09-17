package specout

import (
	"fmt"
	"slices"
	"strings"
)

// operationFor builds one operation object: the metadata fields, the path and
// query parameters, the request body and the responses. build places the
// result at paths[docPath][method] unless the record is a catch-all.
func (d *Generator) operationFor(rec *routeRecord, sr *schemaRegistry) *obj {
	op := newObj()
	op.setIf("summary", rec.summary)
	op.setIf("description", rec.description)
	opID := rec.operationID
	if opID == "" {
		opID = operationID(rec.method, docPath(rec.full))
	}
	// a malformed segment ({}) derives no id: omit the field rather than
	// emit an empty, unusable operationId
	op.setIf("operationId", opID)
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
	params := slices.Concat(pathParamObjs(rec.full, rec.req), taggedParams(rec.req))
	if len(params) > 0 {
		op.set("parameters", params)
	}
	// the body is Req minus its parameter fields; no body type means no body
	if bt, name := sr.bodyType(rec.req); bt != nil {
		if name != "" {
			sr.overrideName(bt, name)
		}
		op.set("requestBody", requestBodyObj(rec, bt, sr))
	}
	op.set("responses", d.responsesFor(rec, sr))
	if len(rec.raw) > 0 {
		op = mergeRaw(op, rec.raw)
	}
	return op
}

// operationID derives a deterministic id from method+path:
// GET /onboarding/{id}/sync -> getOnboardingByIdSync.
func operationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for seg := range strings.SplitSeq(strings.Trim(path, "/"), "/") {
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
			return fmt.Errorf(
				"specout: duplicate operationId %s (%s and %s %s) — set OperationID on one route",
				id, first, rec.method, rec.full,
			)
		}
		seen[id] = rec.method + " " + rec.full
	}
	return nil
}
