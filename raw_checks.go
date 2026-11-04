package specout

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"slices"
	"strconv"
)

// Raw replaces what specout writes, so the checks that specout runs on the
// typed fields run on the Raw values too.

// checkRawValues panics on a Raw value of an operation that would break the
// document.
func (d *Generator) checkRawValues(rec *routeRecord) {
	where := rec.label()
	if id, ok := rec.raw["operationId"]; ok {
		if text, isText := id.(string); !isText || text == "" {
			panic("specout: " + where + `: Raw key "operationId" must be a string that is not empty; set OperationID`)
		}
	}
	if sec, ok := rec.raw["security"]; ok {
		d.checkRawSecurity(where, sec)
	}
	if raw, ok := rec.raw["responses"]; ok && rec.method == http.MethodHead {
		for key, v := range rawResponses(where+": Raw.responses", raw) {
			if rawHas(v, "content") {
				panic("specout: " + where + ": Raw.responses " + key + ": a HEAD response has no body; remove its content")
			}
		}
	}
}

// checkRawSecurity panics when a Raw security list names a scheme that
// Config.Auth does not declare.
func (d *Generator) checkRawSecurity(where string, raw any) {
	var requirements []map[string][]string
	if data, err := json.Marshal(raw); err != nil || json.Unmarshal(data, &requirements) != nil {
		panic("specout: " + where + `: Raw key "security" must be a list of Security Requirement objects, ` +
			`such as [{"bearerAuth": []}]`)
	}
	names := make([]string, 0, len(d.cfg.Auth))
	for _, a := range d.cfg.Auth {
		names = append(names, a.Name)
	}
	for _, requirement := range requirements {
		for _, scheme := range sortedKeys(requirement) {
			if !slices.Contains(names, scheme) {
				panic("specout: " + where + `: Raw key "security" names scheme ` + strconv.Quote(scheme) +
					", but Config.Auth does not declare it" + didYouMean(scheme, names))
			}
		}
	}
}

// checkRawParameters fails when Raw.parameters leaves out a path parameter of
// the full path: each {name} needs a parameter with in: path and required:
// true. A $ref entry can hold any parameter, so a list with one is not checked.
func checkRawParameters(rec *routeRecord) error {
	raw, ok := rec.raw["parameters"]
	if !ok {
		return nil
	}
	var params []map[string]any
	if data, err := json.Marshal(raw); err != nil || json.Unmarshal(data, &params) != nil {
		return fmt.Errorf("specout: %s: Raw key \"parameters\" must be a list of Parameter objects", rec.label())
	}
	covered := map[string]bool{}
	for _, p := range params {
		if _, isRef := p["$ref"]; isRef {
			return nil
		}
		required, _ := p["required"].(bool)
		if name, _ := p["name"].(string); p["in"] == pathLocation && required {
			covered[name] = true
		}
	}
	for _, name := range pathParams(docPath(rec.full)) {
		if !covered[name] {
			return fmt.Errorf("specout: %s: Raw.parameters has no path parameter %q; add "+
				`{"name": %q, "in": "path", "required": true}, or remove the Raw key`, rec.label(), name, name)
		}
	}
	return nil
}

// checkRawResponse panics on a Raw.responses entry that is not a Response or
// Reference object with a description, or that gives a body to a status that
// has none.
func checkRawResponse(where string, code int, v jsontext.Value) {
	if rawHas(v, "$ref") {
		return
	}
	var resp map[string]jsontext.Value
	if json.Unmarshal(v, &resp) != nil || resp["description"].Kind() != '"' {
		panic("specout: " + where + `: a Response object needs a description; add "description"`)
	}
	if _, ok := resp["content"]; ok && bodyless(code) {
		panic("specout: " + where + ": status " + strconv.Itoa(code) + " has no body; remove its content")
	}
}

// rawHas reports whether the JSON object v has key.
func rawHas(v jsontext.Value, key string) bool {
	var fields map[string]jsontext.Value
	if json.Unmarshal(v, &fields) != nil {
		return false
	}
	_, ok := fields[key]
	return ok
}
