package specout

import (
	"iter"
	"slices"
	"strconv"
	"strings"
)

type webhook struct {
	name string
	rec  routeRecord
}

// Webhook documents a request that your API sends to its clients on its own,
// such as an order event. name identifies the event, and it is not a path. h
// describes the request as a route does: Req is what your API sends, and Res is
// what the client answers. h has no HandlerFunc, because your API does not
// serve this request.
//
// A webhook stays out of the route checks and the recorder. It does not take
// DefaultErrors, which describe the responses of your API. It needs no auth from
// Config.Auth unless h.Security names a scheme. Call before building the spec.
func (g *Generator) Webhook[Req, Res any](method, name string, h Handler[Req, Res]) {
	if name == "" {
		panic("specout: Webhook needs a name")
	}
	rec := outgoingRecord("webhook "+name, method, h)
	g.checkRoute(&rec)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.beginChange()
	if slices.ContainsFunc(g.webhooks, func(w webhook) bool { return w.name == name && w.rec.method == method }) {
		panic("specout: " + rec.label() + ": duplicate webhook; each name takes each method once")
	}
	g.webhooks = append(g.webhooks, webhook{name: name, rec: rec})
}

// Callback is a request that your API sends back to the client because of one
// operation, such as a job result. Make one with NewCallback.
type Callback struct {
	name string
	url  string
	rec  routeRecord
}

// NewCallback documents a request that your API sends after an operation. The
// method comes first, as in Webhook and Document. name identifies the
// callback. url is where the request goes, usually a runtime expression that
// reads the original request, such as "{$request.body#/callbackUrl}". h
// describes the request as Webhook does. One Callback value can go on several
// routes when h sets no OperationID: an operationId names one operation of the
// whole document.
func NewCallback[Req, Res any](method, name, url string, h Handler[Req, Res]) Callback {
	if name == "" || url == "" {
		panic("specout: NewCallback needs a name and a url")
	}
	return Callback{name: name, url: url, rec: outgoingRecord("callback "+name, method, h)}
}

// outgoingRecord is the record of a request that your API sends. Nothing serves
// it, so it has no handler and no path. Config.Auth describes how clients reach
// your API, so the request does not inherit that requirement.
func outgoingRecord[Req, Res any](where, method string, h Handler[Req, Res]) routeRecord {
	checkMethod(method, where)
	if h.HandlerFunc != nil {
		panic("specout: " + where + ": remove HandlerFunc; your API sends this request, so it has no handler")
	}
	rec := recOf(h)
	rec.method, rec.pattern = method, where
	rec.outgoing = true
	rec.public = h.Public || len(h.Security) == 0
	if name, ok := strayPathField(rec.req, ""); ok {
		panic("specout: " + where + `: remove the tag path:"` + name + `" from Req; this request has no path`)
	}
	return rec
}

// checkCallbacks panics on a route's callbacks when they would break the
// document. Each one is checked as a route is, and its messages name rec. The
// list is copied first, because one Callback value can go on several routes.
func (g *Generator) checkCallbacks(rec *routeRecord) {
	rec.callbacks = slices.Clone(rec.callbacks)
	seen := make(map[string]bool, len(rec.callbacks))
	for i := range rec.callbacks {
		cb := &rec.callbacks[i]
		if cb.name == "" {
			panic("specout: " + rec.label() + ": Callbacks holds an empty Callback; make it with NewCallback")
		}
		key := strings.Join([]string{cb.name, cb.url, cb.rec.method}, "\x00")
		if seen[key] {
			panic("specout: " + rec.label() + ": Callbacks repeats " + cb.rec.method + " callback " + strconv.Quote(cb.name))
		}
		seen[key] = true
		cb.rec.parent = rec
		g.checkRoute(&cb.rec)
	}
}

// callbackObjs is the callbacks object of one operation. Callbacks with one name
// and one url share a path item.
func (g *Generator) callbackObjs(callbacks []Callback, sr *schemaRegistry) *obj {
	out := newObj()
	for i := range callbacks {
		cb := &callbacks[i]
		out.child(cb.name).child(cb.url).set(strings.ToLower(cb.rec.method), g.operationFor(&cb.rec, sr))
	}
	return out
}

func (g *Generator) webhookObjs(sr *schemaRegistry) *obj {
	out := newObj()
	for i := range g.webhooks {
		w := &g.webhooks[i]
		out.child(w.name).set(strings.ToLower(w.rec.method), g.operationFor(&w.rec, sr))
	}
	return out
}

// operations yields each operation of the document: the routes, the webhooks,
// and the callbacks under them.
func (g *Generator) operations() iter.Seq[*routeRecord] {
	return func(yield func(*routeRecord) bool) {
		for _, rec := range g.records {
			if !rec.isCatchAll() && !yieldOperation(rec, yield) {
				return
			}
		}
		for i := range g.webhooks {
			if !yieldOperation(&g.webhooks[i].rec, yield) {
				return
			}
		}
	}
}

func yieldOperation(rec *routeRecord, yield func(*routeRecord) bool) bool {
	if !yield(rec) {
		return false
	}
	for i := range rec.callbacks {
		if !yieldOperation(&rec.callbacks[i].rec, yield) {
			return false
		}
	}
	return true
}
