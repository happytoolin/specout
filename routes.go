package specout

import (
	"net/http"
	"reflect"

	"github.com/go-chi/chi/v5"
)

// chiRouter aliases chi.Router so the Generator can hold a root without
// importing chi here (dependency rule: chi stays in routes.go/register_chi.go).
type chiRouter = chi.Router

// routeRecord holds metadata captured at registration time. Full paths
// resolve at build time from a walk of the live router.
type routeRecord struct {
	method      string
	pattern     string // as passed at registration; chi patterns are relative
	req, res    reflect.Type
	responses   []Response
	tags        []string
	summary     string
	description string
	deprecated  bool
	public      bool
	fn          http.HandlerFunc // identity key
	full        string           // resolved full path, set at build
}

// HandlerMeta is the operation-level metadata carried on a Handler.
type HandlerMeta struct {
	Responses   []Response
	Tags        []string
	Summary     string
	Description string
	Deprecated  bool
	Public      bool
}

// metaer lets register pull HandlerMeta off any Handler instantiation
// without reflection on unexported fields.
type metaer interface{ handlerMeta() HandlerMeta }

func (m HandlerMeta) handlerMeta() HandlerMeta { return m }

// register records metadata keyed by the embedded HandlerFunc's func pointer.
// Panics after the spec has frozen.
func (d *Generator) register(rec routeRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.frozen {
		panic("specout: registration after the spec was built")
	}
	d.routes[reflect.ValueOf(rec.fn).Pointer()] = append(d.routes[reflect.ValueOf(rec.fn).Pointer()], &rec)
}

func (d *Generator) lookup(ptr uintptr) ([]*routeRecord, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	recs, ok := d.routes[ptr]
	return recs, ok
}

// walkAll walks every distinct chi router the generator saw, reporting full
// paths from whichever router can actually serve each route. chi composes
// Route/Mount prefixes invisibly, so as-passed patterns are not trustworthy;
// where several routers see the same handler (subrouter before and after
// Mount), the longest walk path wins — it carries the mount prefix.
// walkAll collects every full path each handler+method appears at. One
// handler may be registered on several routes; all walked paths are kept
// so each registration can claim one.
func (d *Generator) walkAll() (map[rkey][]string, error) {
	hits := make(map[rkey][]string)
	for _, root := range d.chiRoots {
		err := chi.Walk(root, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
			ptr := reflect.ValueOf(handler).Pointer()
			k := rkey{ptr, method}
			hits[k] = append(hits[k], route)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return hits, nil
}

type rkey struct {
	ptr    uintptr
	method string
}

// chi.Routes implementation: tells chi.Walk the generator has no subroutes,
// so mounting the spec on a chi router doesn't surface as stray handler
// entries when adopting the tree.
func (d *Generator) Routes() []chi.Route { return nil }

func (d *Generator) Middlewares() chi.Middlewares { return nil }

// Match and Find complete chi's Routes interface: the generator has no
// subroutes, so it matches nothing for traversal purposes. chi still serves
// /openapi.json through Mount's wildcard.
func (d *Generator) Match(rctx *chi.Context, method, path string) bool {
	return method == http.MethodGet
}

func (d *Generator) Find(rctx *chi.Context, method, path string) string {
	return ""
}
