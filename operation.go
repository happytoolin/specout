package specout

import (
	"cmp"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"
	"unicode/utf8"
)

// operationFor builds one operation object: the metadata fields, the path and
// query parameters, the request body and the responses. build places the
// result at paths[docPath][method] unless the record is a catch-all.
func (g *Generator) operationFor(rec *routeRecord, sr *schemaRegistry) *obj {
	op := newObj()
	// a schema panic deep in the reflection does not know the route
	defer func() { rec.namePanic(recover()) }()
	op.setIf("summary", rec.summary)
	op.setIf("description", rec.description)
	// a malformed segment ({}) derives no id: omit the field rather than
	// emit an empty, unusable operationId
	op.setIf("operationId", emittedOperationID(rec))
	if rec.deprecated {
		op.set("deprecated", true)
	}
	if rec.externalDocs != nil {
		op.set("externalDocs", externalDocsObj(rec.label(), rec.externalDocs))
	}
	if security := g.routeSecurity(rec); security != nil {
		op.set("security", security)
	}
	if len(rec.servers) > 0 {
		op.set("servers", serverObjs(rec.label()+": Servers", rec.servers))
	}
	if len(rec.tags) > 0 {
		op.set("tags", operationTags(rec.label(), rec.tags))
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
	op.set("responses", g.responsesFor(rec, sr))
	if len(rec.callbacks) > 0 {
		op.set("callbacks", g.callbackObjs(rec.callbacks, sr))
	}
	return mergeRaw(op, rec.raw)
}

// checkRoute panics on route data that would break the document: Raw keys,
// request media types and responses. Every binder calls it through addRoute
// before the route reaches the router, so the stack names the route and a bad
// route leaves no record. Checks that need the resolved path or the other
// routes run at build.
func (g *Generator) checkRoute(rec *routeRecord) {
	checkRawKeys(rec.label(), rec.raw, operationFields)
	checkRawReplaces(rec.label(), rec.raw, []replacedField{
		{"summary", "Summary", rec.summary != ""},
		{keyDescription, "Description", rec.description != ""},
		{"operationId", "OperationID", rec.operationID != ""},
		{"tags", "Tags", len(rec.tags) > 0},
		{"externalDocs", "ExternalDocs", rec.externalDocs != nil},
		{keyDeprecated, "Deprecated", rec.deprecated},
		// a webhook or callback is public unless it sets Security
		{"security", "Security or Public", len(rec.security) > 0 || (rec.public && !rec.outgoing)},
		{"requestBody", "Request", rec.describesRequestBody()},
		{"responses", "Responses", len(rec.responses) > 0},
	})
	g.checkRawValues(rec)
	operationTags(rec.label(), rec.tags)
	serverObjs(rec.label()+": Servers", rec.servers)
	if rec.externalDocs != nil {
		externalDocsObj(rec.label(), rec.externalDocs)
	}
	checkRequestBody(rec)
	checkBodyFiles(rec)
	checkTraceBody(rec)
	g.checkSecurity(rec)
	g.checkResponses(rec)
	g.checkCallbacks(rec)
}

var braces = strings.NewReplacer("{", "", "}", "")

// operationID derives a deterministic id from method+path:
// GET /onboarding/{id}/sync -> getOnboardingIdSync.
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
// operationId wins, because mergeRaw applies Raw last. A webhook or callback has
// no path to derive an id from, so it has only the id that it sets.
func emittedOperationID(rec *routeRecord) string {
	if id, ok := rec.raw["operationId"].(string); ok {
		return id
	}
	// An unresolved route has no path to derive an ID from, and the build reports it.
	if rec.outgoing || rec.full == "" {
		return rec.operationID
	}
	return cmp.Or(rec.operationID, operationID(rec.method, docPath(rec.full)))
}

// checkOperationIDs fails loud when two operations would share an id:
// client codegen assumes uniqueness. It returns the operation of each id. Two
// registrations of one route also derive one id, and validateRecords reports
// that duplicate route.
func checkOperationIDs(operations iter.Seq[*routeRecord]) (map[string]*routeRecord, error) {
	seen := make(map[string]*routeRecord)
	var errs []error
	for rec := range operations {
		id := emittedOperationID(rec)
		if id == "" {
			continue
		}
		first, dup := seen[id]
		switch {
		case !dup:
			seen[id] = rec
		case sameRoute(first, rec):
		case first.parent != nil && rec.parent != nil && first.pattern == rec.pattern:
			errs = append(errs, fmt.Errorf(
				"specout: %s: duplicate operationId %q, also on %s; a Callback on several routes cannot set "+
					"OperationID, so remove it, or make one Callback for each route",
				rec.label(), id, first.label(),
			))
		default:
			errs = append(errs, fmt.Errorf(
				"specout: %s: duplicate operationId %q, also on %s; set another OperationID on one of them",
				rec.label(), id, first.label(),
			))
		}
	}
	return seen, errors.Join(errs...)
}

// sameRoute reports whether a and b are one method on one documented path.
func sameRoute(a, b *routeRecord) bool {
	return a.full != "" && b.full != "" && a.method == b.method && docPath(a.full) == docPath(b.full)
}
