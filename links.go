package specout

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// Link tells a client how to use a response in a next request. Set OperationID
// or OperationRef, not both. The build fails when no operation has OperationID.
type Link struct {
	// OperationID names the next operation. Set Handler.OperationID on that
	// route to give it a name of your choice.
	OperationID string
	// OperationRef points at an operation with a URI, for an operation in
	// another document.
	OperationRef string
	// Parameters holds the parameters of the next request, by name: a constant
	// or a runtime expression, such as "$response.body#/id". The official
	// OpenAPI schema allows only strings here.
	Parameters map[string]string
	// RequestBody is the body of the next request: a constant or a runtime
	// expression.
	RequestBody any
	Description string
	// Server replaces the server of the next request.
	Server *Server
}

// linkObjs is the links object of one response. where names the response.
func linkObjs(where string, links map[string]Link) *obj {
	out := newObj()
	for _, name := range sortedKeys(links) {
		out.set(name, linkObj(where+": link "+strconv.Quote(name), links[name]))
	}
	return out
}

// linkObj builds one link. OpenAPI requires exactly one way to name the target.
func linkObj(where string, l Link) *obj {
	if (l.OperationID == "") == (l.OperationRef == "") {
		panic("specout: " + where + ": set one of OperationID and OperationRef")
	}
	o := newObj().setIf("operationRef", l.OperationRef).setIf("operationId", l.OperationID)
	if len(l.Parameters) > 0 {
		params := newObj()
		for _, key := range sortedKeys(l.Parameters) {
			params.set(key, l.Parameters[key])
		}
		o.set("parameters", params)
	}
	if l.RequestBody != nil {
		o.set("requestBody", exampleValue(where, l.RequestBody))
	}
	o.setIf("description", l.Description)
	if l.Server != nil {
		o.set("server", serverObj(where+": Server", *l.Server))
	}
	return o
}

// checkLinks fails when a link names an operationId that no operation has. It
// runs at build, because the target can register after the link.
func (d *Generator) checkLinks() error {
	var ids []string
	for rec := range d.operations() {
		if id := emittedOperationID(rec); id != "" {
			ids = append(ids, id)
		}
	}
	var errs []error
	for rec := range d.operations() {
		errs = append(errs, d.checkLinkTargets(rec, ids))
	}
	return errors.Join(errs...)
}

// checkLinkTargets checks the links of one operation's responses.
func (d *Generator) checkLinkTargets(rec *routeRecord, ids []string) error {
	var errs []error
	for _, e := range d.responsePlan(rec, false) {
		if e.resp == nil {
			continue
		}
		for _, name := range sortedKeys(e.resp.Links) {
			id := e.resp.Links[name].OperationID
			if id == "" || slices.Contains(ids, id) {
				continue
			}
			errs = append(errs, fmt.Errorf("specout: %s: response %s: link %q: no operation has operationId %q; "+
				"set OperationID on the target route%s", rec.label(), e.key, name, id, didYouMean(id, ids)))
		}
	}
	return errors.Join(errs...)
}
