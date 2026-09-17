// Package handlers holds the demo factories: types ride the type parameters, the closure stays a plain http.HandlerFunc.
package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/onboarding"
)

// Deps is what every factory receives; Mapper is the app's own error mapper.
type Deps struct {
	Store  *onboarding.Store
	Mapper api.Mapper
}

// op builds a handler in one call: shared metadata plus the closure; res is optional.
func op[Req, Res any](tag, summary string, public bool, fn http.HandlerFunc, res ...specout.Response) specout.Handler[Req, Res] {
	return specout.Handler[Req, Res]{HandlerFunc: fn, Summary: summary, Tags: []string{tag}, Public: public, Responses: res}
}

func writeErr(w http.ResponseWriter, d Deps, err error) { api.Error(w, d.Mapper, err) }

// queryInt reads an int off the raw request (query tags are not auto-bound); malformed input keeps def.
func queryInt(q url.Values, key string, def int) int {
	if v := q.Get(key); v != "" {
		json.Unmarshal([]byte(v), &def)
	}
	return def
}
