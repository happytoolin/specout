package specout_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/adapters/specoutstd"
	"github.com/happytoolin/specout/internal/bridge"
	"github.com/happytoolin/specout/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBadRouteDataFailsAtRegistration(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	bad := specout.Handler[struct{}, string]{HandlerFunc: noop, Responses: []specout.Response{{
		Status: http.StatusOK, Headers: []specout.Header{{Name: "X-Id"}, {Name: "x-id"}},
	}}}
	assert.PanicsWithValue(t, `specout: GET /x: response 200: header "x-id" is repeated; header names ignore case`, func() {
		specout.Bind(d, specoutstd.New(mux)).Handle("GET", "/x", bad)
	})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)
	paths, _ := buildDoc(t, d)["paths"].(map[string]any)
	assert.NotContains(t, paths, "/x")
}

func TestUnicodeRoutes(t *testing.T) {
	for _, path := range []string{"/日本語", "/éclair", "/🦀", "/items/İstanbul", "/items/a*b", "/items/*"} { //nolint:gosmopolitan // Exercise Unicode route names.
		t.Run(path, func(t *testing.T) {
			d, mux := newGen(), http.NewServeMux()
			specout.Bind(d, specoutstd.New(mux)).Handle("GET", path, okGet)
			w := httptest.NewRecorder()
			target := (&url.URL{Path: path}).String()
			mux.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
			require.Equal(t, http.StatusNoContent, w.Code)
			assert.Contains(t, docPaths(t, d), path)
		})
	}
}

func TestSharedHandlerPairingStable(t *testing.T) {
	build := func() []string {
		d := newGen()
		rc := specout.Bind(d, specoutstd.New(http.NewServeMux()))
		rc.Handle("GET", "/a/{id}", okGet)
		rc.Handle("GET", "/b/{id}", okGet)
		return keys(docPaths(t, d))
	}
	first, second := build(), build()
	require.Equal(t, first, second, "pairing unstable across two builds")
	require.Len(t, first, 2, "want 2 paths")
}

func TestAllowedStatusesIncludesDefaults(t *testing.T) {
	d := specout.New(specout.Config{
		Title: "t", Version: "1",
		ErrorType: Problem{}, DefaultErrors: []int{400, 404, 500},
	})
	rc := specout.Bind(d, specoutstd.New(http.NewServeMux()))
	rc.Handle("GET", "/x", okGet)

	key := bridge.Key{Method: "GET", Path: "/x"}
	decl := declaredStatuses(t, d)
	assert.False(t, decl[key][404], "the required statuses carry the global envelope: %v", decl[key])
	assert.True(t, decl[key][204], "the required statuses miss the route's own 204: %v", decl[key])
	spec := specStatuses(t, d)
	for _, c := range []int{204, 400, 404, 500} {
		assert.True(t, spec[key][c], "the allowed statuses miss %d: %v", c, spec[key])
	}

	rc.Handle("GET", "/y", noBody{HandlerFunc: okBody, Responses: []specout.Response{{Status: 404, Omit: true}}})
	key = bridge.Key{Method: "GET", Path: "/y"}
	decl, spec = declaredStatuses(t, d), specStatuses(t, d)
	assert.False(t, decl[key][404] || spec[key][404], "Omit did not remove 404: decl=%v spec=%v", decl[key], spec[key])
	assert.True(t, spec[key][500], "sibling default 500 dropped: %v", spec[key])
}

func TestDuplicateCanonicalFails(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x", okGet)
	d.Document(http.MethodGet, "/x", noBody{HandlerFunc: okBody})
	wantBuildErr(t, d, "duplicate")
}

func TestDocument(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/anything", okGet)
	require.Contains(t, docPaths(t, d), "/anything", "documented route missing")
}

func TestDocumentForeignRouterEndToEnd(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	d.Document(http.MethodGet, "/echo/items/{id}", okGet)
	mux.HandleFunc("GET /echo/items/{id}", okGet.HandlerFunc)
	require.Contains(t, docPaths(t, d), "/echo/items/{id}", "Document path missing from spec")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/echo/items/7", nil))
	assert.Equal(t, 204, rec.Code, "foreign mux serve")
}

func TestBadMethodPanics(t *testing.T) {
	mux := http.NewServeMux()
	for _, bad := range [][2]string{{"get", "/x"}, {"GET", " /x"}, {"GET", "x"}} {
		require.Panics(t, func() { specout.Bind(newGen(), specoutstd.New(mux)).Handle(bad[0], bad[1], okGet) })
	}
	require.Panics(t, func() { newGen().Document("get", "/x", okGet) })
	require.Panics(t, func() { newGen().Document("GET", "x", okGet) })
}

func TestStdMultiSegmentWildcardOmitted(t *testing.T) {
	d := newGen()
	specout.Bind(d, specoutstd.New(http.NewServeMux())).Handle("GET", "/files/{path...}", okGet)
	assert.NotContains(t, docPaths(t, d), "/files/{path...}", "std {path...} leaked into paths")
	assert.Contains(t, declaredStatuses(t, d), bridge.Key{Method: "GET", Path: "/files/{path...}"}, "catch-all missing from drift keys")
}

func TestSharedHandlerMetadataNotSwapped(t *testing.T) {
	d := newGen()
	rc := specout.Bind(d, specoutstd.New(http.NewServeMux()))
	h := noBody{HandlerFunc: okBody, Summary: "SHORT"}
	rc.Handle("GET", "/a", h)
	h.Summary = "LONG"
	rc.Handle("GET", "/b/c", h)
	for path, summary := range map[string]string{"/a": "SHORT", "/b/c": "LONG"} {
		assert.Equal(t, summary, opOf(t, buildDoc(t, d), path, "get")["summary"], path)
	}
}

func TestBraceQuantifierParamNormalized(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x/{id:[0-9]{4}}", okGet)
	op, ok := docPaths(t, d)["/x/{id}"].(map[string]any)
	require.True(t, ok, "want /x/{id}")
	assert.Equal(t, "getXId", op["get"].(map[string]any)["operationId"])
}

func TestNilHandlerPanics(t *testing.T) {
	require.Panics(t, func() {
		specout.Bind(newGen(), specoutstd.New(http.NewServeMux())).Handle("GET", "/x", noBody{Summary: "documents a segfault"})
	})
}

func TestNonOpenAPIMethodPanics(t *testing.T) {
	require.Panics(t, func() { newGen().Document("CONNECT", "/x", okGet) })
	require.Panics(t, func() { specout.Bind(newGen(), specoutstd.New(http.NewServeMux())).Handle("CONNECT", "/x", okGet) })
}

type strayPathReq struct {
	ID string `path:"Id"`
}

func TestStrayPathFieldFails(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x/{id}", specout.Handler[strayPathReq, specout.NoContent]{HandlerFunc: okBody})
	wantBuildErr(t, d, `path:"Id"`)
}

func TestBraceInParameterNameFails(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x/{{id}}", okGet)
	wantBuildErr(t, d, "a path parameter name has a brace")
}

func TestRepeatedPathParameterFails(t *testing.T) {
	for name, register := range map[string]func(d *specout.Generator){
		"document": func(d *specout.Generator) { d.Document(http.MethodGet, "/a/{id}/b/{id}", okGet) },
		"bind": func(d *specout.Generator) {
			bind(d, http.MethodGet, "/a/:id/b/:id", okGet, func(http.Handler) string { return "/a/{id}/b/{id}" })
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := newGen()
			register(d)
			wantBuildErr(t, d, `path parameter "id" appears twice`)
		})
	}
}

func TestPathFieldMatchesPattern(t *testing.T) {
	type req struct {
		ID   string `json:"-"    path:"id"`
		Skip string `json:"skip" path:"-"`
	}
	d := newGen()
	d.Document(http.MethodGet, "/x/{id}", specout.Handler[req, specout.NoContent]{HandlerFunc: okBody})
	require.Contains(t, docPaths(t, d), "/x/{id}", "path-typed route missing")
}

func TestEquivalentTemplatePathsFail(t *testing.T) {
	for _, methods := range [][2]string{
		{http.MethodGet, http.MethodGet},
		{http.MethodGet, http.MethodPost},
	} {
		t.Run(methods[0]+"_"+methods[1], func(t *testing.T) {
			d := newGen()
			d.Document(methods[0], "/x/{id}", okGet)
			d.Document(methods[1], "/x/{name}", okGet)
			wantBuildErr(t, d, "is the same path to OpenAPI")
		})
	}
}

func TestSameTemplateAllowsDifferentMethods(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/x/{id}", okGet)
	d.Document(http.MethodPost, "/x/{id}", okGet)
	item := docPaths(t, d)["/x/{id}"].(map[string]any)
	assert.Contains(t, item, "get")
	assert.Contains(t, item, "post")
}

func TestEquivalentCatchAllsRemainExcluded(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/files/{path...}", okGet)
	d.Document(http.MethodPost, "/files/{rest...}", okGet)
	paths := docPaths(t, d)
	assert.NotContains(t, paths, "/files/{path...}")
	assert.NotContains(t, paths, "/files/{rest...}")
}

func TestDuplicateCatchAllMethodStillFails(t *testing.T) {
	d := newGen()
	d.Document(http.MethodGet, "/files/{path...}", okGet)
	d.Document(http.MethodGet, "/files/{path...}", okGet)
	wantBuildErr(t, d, "duplicate route")
}

func TestRegistrationFailureDoesNotMutateRouter(t *testing.T) {
	for failure, h := range map[string]noBody{"nil": {}, "frozen": okGet} {
		t.Run(failure, func(t *testing.T) {
			d, mux := newGen(), http.NewServeMux()
			if failure == "frozen" {
				require.NotNil(t, buildDoc(t, d))
			}
			require.Panics(t, func() { specout.Bind(d, specoutstd.New(mux)).Handle("GET", "/late", h) })
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/late", nil))
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func panicValue(fn func()) any {
	var v any
	func() {
		defer func() { v = recover() }()
		fn()
	}()
	return v
}

func TestDocumentRejectsNonOpenAPIPaths(t *testing.T) {
	for pattern, want := range map[string]string{
		"/items/:id": ":id is router syntax; write the parameter as {id}",
		"/items/{id": "{ without a matching }",
		"/items/id}": "} without a matching {",
		"/a//b":      "empty segment (//)",
		"/c/{$}":     "{$} is the net/http end anchor",
		"/d/./e":     "dot segment (/.)",
		"/f?x=1":     `the character '?'`,
		"/g h":       `the character ' '`,
		"items/{id}": "must start with /",
	} {
		t.Run(pattern, func(t *testing.T) {
			msg := fmt.Sprint(panicValue(func() { newGen().Document(http.MethodGet, pattern, okGet) }))
			assert.Contains(t, msg, want)
		})
	}
	assert.Contains(t, docOf(t, http.MethodPost, "/items/{id}:cancel", okGet)["paths"], "/items/{id}:cancel")
}

func TestBindRejectsEmptyAndDotSegments(t *testing.T) {
	for _, p := range []string{"/a//b", "/a/./b", "/a/../b", "/a/..", "//a"} {
		t.Run(p, func(t *testing.T) {
			d := newGen()
			msg := fmt.Sprint(panicValue(func() {
				bind(d, http.MethodGet, p, okGet, func(http.Handler) string { return p })
			}))
			assert.Contains(t, msg, "mounted at "+p+": the path has")
			assert.Empty(t, docPaths(t, d))
		})
	}
	d := newGen()
	bind(d, http.MethodGet, "/files/", okGet, func(http.Handler) string { return "/files/" })
	assert.Equal(t, []string{"/files/"}, keys(docPaths(t, d)))
}

func TestBindRecordsOnlyAfterTheRouterAccepts(t *testing.T) {
	d := newGen()
	require.Panics(t, func() {
		bind(d, http.MethodGet, "/boom", okGet, func(http.Handler) string { panic("router said no") })
	})
	msg := fmt.Sprint(panicValue(func() {
		bind(d, http.MethodGet, "items", okGet, func(http.Handler) string { return "items" })
	}))
	assert.Contains(t, msg, "mounted at items: the path must start with /")
	bind(d, http.MethodGet, "/ok/:id", okGet, func(next http.Handler) string {
		require.NotNil(t, next)
		return "/ok/{id}"
	})
	assert.Equal(t, []string{"/ok/{id}"}, keys(docPaths(t, d)))

	called := false
	require.Panics(t, func() {
		bind(d, http.MethodGet, "/late", okGet, func(http.Handler) string { called = true; return "/late" })
	})
	assert.False(t, called, "a frozen generator must not touch the router")
}

func TestBindNamesTheRouteInRouterPanics(t *testing.T) {
	errRouter := errors.New("router said no")
	for name, tc := range map[string]struct {
		value any
		want  string
	}{
		"string":  {"wildcards must be named", "specout: GET /a*b: wildcards must be named"},
		"error":   {errRouter, "specout: GET /a*b: router said no"},
		"other":   {42, "specout: GET /a*b: 42"},
		"adapter": {"specoutgin: GET /a*b: bad", "specoutgin: GET /a*b: bad"},
	} {
		t.Run(name, func(t *testing.T) {
			d := newGen()
			v := panicValue(func() {
				bind(d, http.MethodGet, "/a*b", okGet, func(http.Handler) string { panic(tc.value) })
			})
			assert.Equal(t, tc.want, fmt.Sprint(v))
			if err, ok := tc.value.(error); ok {
				require.ErrorIs(t, v.(error), err)
			}
			assert.Empty(t, docPaths(t, d), "the panicked route left a record")
		})
	}
}

func TestBindRejectsADuplicateRoute(t *testing.T) {
	d := newGen()
	mounts := 0
	mount := func(http.Handler) string { mounts++; return "/items/{id}" }
	bind(d, http.MethodGet, "/items/:id", okGet, mount)
	bind(d, http.MethodPost, "/items/:id", okGet, mount)
	msg := fmt.Sprint(panicValue(func() { bind(d, http.MethodGet, "/items/:key", okGet, mount) }))
	assert.Contains(t, msg, `specout: GET /items/{id}: duplicate route, registered as "/items/:id" and "/items/:key"`)
	assert.Equal(t, 3, mounts)
	assert.Len(t, docPaths(t, d)["/items/{id}"], 2, "the duplicate left a record")
}

func TestCheckRoutesMatchesBindByMethodAndPath(t *testing.T) {
	d := newGen()
	bind(d, http.MethodGet, "/items/:id", okGet, func(http.Handler) string { return "/items/{id}" })
	d.Document(http.MethodPost, "/items", okGet)
	err := specout.Check(d, fakeRouter{routes: []specout.Route{
		{Method: http.MethodGet, Path: "/items/{id}"},
		{Method: http.MethodPost, Path: "/items"},
		{Method: http.MethodDelete, Path: "/items/{id}"},
		{Method: http.MethodDelete, Path: "/items/{id}"},
		{Method: "", Path: "/any"},
		{Method: http.MethodGet, Path: "/openapi.json"},
	}}, "/openapi.json")
	stray, ok := errors.AsType[*bridge.StrayError](err)
	require.True(t, ok, "%v is not a stray error", err)
	assert.Equal(t, []string{"? /any (no method constraint)", "DELETE /items/{id}"}, stray.Routes)
	require.NoError(t, specout.Check(d, fakeRouter{}))
}

func TestStdRejectsNilArguments(t *testing.T) {
	for name, fn := range map[string]func(){
		"std generator": func() { specout.Bind(nil, specoutstd.New(http.NewServeMux())) },
		"std mux":       func() { specout.Bind(newGen(), specoutstd.New(nil)) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, fmt.Sprint(panicValue(fn)), "needs a")
		})
	}
}

func TestBindAndDocumentRejectANilGenerator(t *testing.T) {
	assert.PanicsWithValue(t, "specout: Bind needs a generator and a router adapter", func() {
		bind((*specout.Generator)(nil), http.MethodGet, "/x", okGet, func(http.Handler) string { return "/x" })
	})
	assert.PanicsWithValue(t, "specout: Document needs a generator; make one with New", func() {
		(*specout.Generator)(nil).Document(http.MethodGet, "/x", okGet)
	})
}

func TestStdSubtreeIsACatchAll(t *testing.T) {
	d, sm := newGen(), http.NewServeMux()
	std := specout.Bind(d, specoutstd.New(sm))
	std.Handle("GET", "/files/", okGet)
	std.Handle("GET", "/blobs/{path...}", okGet)
	std.Handle("GET", "/dir/{$}", okGet)
	std.Handle("GET", "/{$}", okGet)
	assert.ElementsMatch(t, []string{"/dir/", "/"}, slices.Collect(maps.Keys(docPaths(t, d))))
	declared := declaredStatuses(t, d)
	assert.Contains(t, declared, bridge.Key{Method: http.MethodGet, Path: "/files/{*...}"}, "the subtree keeps its drift entry")
}

type featureQuery struct {
	Filter map[string]string `jsonschema:"style=deepObject,explode=true" query:"filter"`
	Hdr    string            `header:"X-Req"`
}

type featureBody struct {
	Old string `json:"old"     jsonschema:"deprecated"`
	Tag string `json:"tagged"  jsonschema_extras:"x-ms-identifiers=id"`
	Ex  string `json:"example" jsonschema:"example=a,example=b"`
}

type featureRes struct {
	OK bool `json:"ok"`
}

func featureConfig() specout.Config {
	return specout.Config{
		Title: "probe", Version: "1", TermsOfService: "https://x.example/tos",
		Contact: &specout.Contact{Name: "API team", Email: "api@x.example"},
		License: &specout.License{Name: "MIT", URL: "https://x.example/mit"},
		Tags:    []specout.Tag{{Name: "things", ExternalDocs: &specout.ExternalDocs{URL: "https://x.example/things"}}},
		Auth: []specout.AuthScheme{specout.OAuth2("oauth", map[string]specout.OAuth2Flow{
			"implicit": {
				AuthorizationURL: "https://x.example/auth",
				Scopes:           map[string]string{"write:pets": "modify pets", "read:pets": "read pets"},
			},
		}), specout.APIKey("api_key", "X-Key", "header")},
	}
}

func registerFeatures(r *specout.Binder) {
	r.Handle("GET", "/things/{id}", specout.Handler[featureQuery, featureRes]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("fail") != "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		},
		Responses:    []specout.Response{{Key: "4XX", Type: featureBody{}}},
		Tags:         []string{"things"},
		ExternalDocs: &specout.ExternalDocs{URL: "https://x.example/op", Description: "how it works"},
		Raw:          map[string]any{"x-rate-limit": 100},
	})
	r.Handle("POST", "/things", specout.Handler[featureBody, featureRes]{
		HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		Request:     specout.Request{ContentTypes: []string{"application/json", "application/xml"}},
		Responses:   []specout.Response{{Status: 200, ContentTypes: []string{"application/json", "application/xml"}}},
	})
}

func assertFeatures(t *testing.T, doc map[string]any) {
	t.Helper()
	op := opOf(t, doc, "/things/{id}", "get")
	params := paramsOf(t, op)
	assert.Equal(t, "deepObject", params["filter"]["style"], "filter param")
	assert.Equal(t, true, params["filter"]["explode"], "filter param")
	sch := params["filter"]["schema"].(map[string]any)
	assert.Nil(t, sch["style"], "style/explode belong to the parameter: %v", sch)
	assert.Nil(t, sch["explode"], "style/explode belong to the parameter: %v", sch)
	assert.Equal(t, "header", params["X-Req"]["in"], "header param")
	assert.Equal(t, "path", params["id"]["in"], "path param")
	assert.Equal(t, true, params["id"]["required"], "path param")

	resp := op["responses"].(map[string]any)
	assert.Contains(t, resp, "4XX", "responses")
	assert.NotContains(t, resp, "404", "a range key must not fan out into one code")
	assert.Contains(t, resp, "200", "the Res default 200 stays beside an explicit response")
	assert.Equal(t, "how it works", op["externalDocs"].(map[string]any)["description"], "op externalDocs")
	assert.InEpsilon(t, 100, op["x-rate-limit"], 0.0001, "x-rate-limit")

	postOp := opOf(t, doc, "/things", "post")
	assert.Contains(t, postOp["requestBody"].(map[string]any)["content"].(map[string]any), "application/xml", "request content")
	resContent := postOp["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	require.Len(t, resContent, 2, "response content = %v, want two media types", resContent)
	jsonRef := resContent["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	xmlRef := resContent["application/xml"].(map[string]any)["schema"].(map[string]any)["$ref"]
	assert.Equal(t, "#/components/schemas/featureRes", jsonRef, "media type schemas = %v and %v", jsonRef, xmlRef)
	assert.Equal(t, jsonRef, xmlRef, "media type schemas = %v and %v", jsonRef, xmlRef)

	info := doc["info"].(map[string]any)
	assert.Equal(t, "https://x.example/tos", info["termsOfService"], "info")
	assert.Equal(t, "MIT", info["license"].(map[string]any)["name"], "info")
	contact := info["contact"].(map[string]any)
	assert.Equal(t, "api@x.example", contact["email"], "contact")
	assert.NotContains(t, contact, "url", "an empty contact field must not be emitted")
	assert.Equal(t, "https://x.example/things",
		doc["tags"].([]any)[0].(map[string]any)["externalDocs"].(map[string]any)["url"], "tag externalDocs")

	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	implicit := schemes["oauth"].(map[string]any)["flows"].(map[string]any)["implicit"].(map[string]any)
	assert.Equal(t, "https://x.example/auth", implicit["authorizationUrl"], "oauth flows")
	scopes := implicit["scopes"].(map[string]any)
	assert.Len(t, scopes, 2, "scopes")
	assert.Equal(t, "modify pets", scopes["write:pets"], "scopes")
	assert.Equal(t, "X-Key", schemes["api_key"].(map[string]any)["name"], "api_key")

	bProps := schemas(t, doc)["featureBody"].(map[string]any)["properties"].(map[string]any)
	assert.Equal(t, true, bProps["old"].(map[string]any)["deprecated"], "featureBody props")
	assert.Equal(t, "id", bProps["tagged"].(map[string]any)["x-ms-identifiers"], "featureBody props")
	ex, _ := bProps["example"].(map[string]any)["examples"].([]any)
	assert.Len(t, ex, 2, "examples")
}

func checkFeatureInvariant(t *testing.T, d *specout.Generator, doc map[string]any) {
	t.Helper()
	st, declared := specStatuses(t, d), declaredStatuses(t, d)
	paths := doc["paths"].(map[string]any)
	for k := range st {
		assert.Contains(t, paths, k.Path, "allowed status path %s is not in the document", k.Path)
	}
	key := bridge.Key{Method: "GET", Path: "/things/{id}"}
	assert.True(t, st[key][404], "4XX must allow 404")
	assert.False(t, declared[key][404], "a bare range must not require 404")
}

func TestFeaturesOnStd(t *testing.T) {
	d, r := specout.New(featureConfig()), http.NewServeMux()
	registerFeatures(specout.Bind(d, specoutstd.New(r)))
	doc := buildDoc(t, d)
	assertFeatures(t, doc)
	checkFeatureInvariant(t, d, doc)
	rec := recorder.New(r)
	for _, target := range []string{"/things/1", "/things/1?fail=1"} {
		rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
	}
	rec.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/things", strings.NewReader("{}")))
	require.NoError(t, recorder.Verify(d, rec))
}

func TestStdLiteralAsteriskIsDocumented(t *testing.T) {
	d, mux := newGen(), http.NewServeMux()
	const path = "/literal/a*b"
	specout.Bind(d, specoutstd.New(mux)).Handle("GET", path, okGet)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Contains(t, docPaths(t, d), path, "ServeMux treats an asterisk as a literal, not a wildcard")
}
