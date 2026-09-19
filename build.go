package specout

import (
	"slices"
	"strings"
)

// build assembles the OpenAPI document: resolve the routes, reflect schemas,
// emit operations in path order.
func (d *Generator) build() (*obj, error) {
	records, err := d.buildRecords()
	if err != nil {
		return nil, err
	}

	spec := newObj().set("openapi", "3.1.0").set("info", d.infoObj())
	if servers := d.serverObjs(); len(servers) > 0 {
		spec.set("servers", servers)
	}

	sr := newSchemaRegistry(d.cfg)
	for name, t := range d.variants {
		sr.registerVariant(t, name)
	}
	for t, name := range d.nameOverrides {
		sr.overrideName(t, name)
	}
	// paths first: it is what fills sr, and components reads sr back.
	spec.set("paths", d.pathObj(records, sr))
	if tags := d.tagObjs(); len(tags) > 0 {
		spec.set("tags", tags)
	}

	components := newObj()
	if len(d.cfg.Auth) > 0 {
		// security: one named scheme per Auth entry, plus a top-level
		// requirement that any one of them satisfies (OR semantics).
		schemes, alts := d.securityObjs()
		components.set("securitySchemes", schemes)
		spec.set("security", alts)
	}
	if schemas := d.schemaObjs(sr); len(schemas.keys) > 0 {
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

// buildRecords resolves every route, checks operationId uniqueness, and drops
// catch-alls from the documented paths.
func (d *Generator) buildRecords() ([]*routeRecord, error) {
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
	return records, nil
}

// infoObj is the document's info object.
func (d *Generator) infoObj() *obj {
	info := newObj().set("title", d.cfg.Title).set("version", d.cfg.Version)
	info.setIf("description", d.cfg.Description)
	info.setIf("termsOfService", d.cfg.TermsOfService)
	if c := d.cfg.Contact; c != nil {
		info.set("contact", newObj().setIf("name", c.Name).setIf("url", c.URL).setIf("email", c.Email))
	}
	if l := d.cfg.License; l != nil {
		info.set("license", newObj().setIf("name", l.Name).setIf("url", l.URL))
	}
	return info
}

// serverObjs is the server list, empty when the config declares none.
func (d *Generator) serverObjs() []any {
	servers := make([]any, 0, len(d.cfg.Servers))
	for _, s := range d.cfg.Servers {
		servers = append(servers, newObj().set("url", s.URL).setIf("description", s.Description))
	}
	return servers
}

// tagObjs is the declared tag list.
func (d *Generator) tagObjs() []any {
	tags := make([]any, 0, len(d.cfg.Tags))
	for _, t := range d.cfg.Tags {
		e := newObj().set("name", t.Name).setIf("description", t.Description)
		if t.ExternalDocs != nil {
			e.set("externalDocs", externalDocsObj(t.ExternalDocs))
		}
		tags = append(tags, e)
	}
	return tags
}

// securityObjs splits the auth config into the components.securitySchemes
// object and the top-level security requirement list.
func (d *Generator) securityObjs() (*obj, []any) {
	schemes := newObj()
	alts := make([]any, 0, len(d.cfg.Auth))
	for _, a := range d.cfg.Auth {
		schemes.set(a.Name, securitySchemeObj(a))
		alts = append(alts, newObj().set(a.Name, []any{}))
	}
	return schemes, alts
}

// schemaObjs is the hoisted components.schemas object: the typed components
// in registration order, then the $defs with no Go type behind them. Empty
// when nothing was hoisted, so the caller can skip the key.
func (d *Generator) schemaObjs(sr *schemaRegistry) *obj {
	if len(sr.order) == 0 {
		return newObj()
	}
	schemas := newObj()
	for _, t := range sr.order {
		e := sr.byType[t]
		schemas.set(e.name, e.s)
	}
	// hoisted $defs with no Go type: emitted after the typed components
	for _, n := range sr.nameOrder {
		if schemas.get(n) == nil {
			schemas.set(n, sr.byName[n])
		}
	}
	return schemas
}

// pathObj reflects one operation per record and groups them by documented
// path. It is what fills sr.
func (d *Generator) pathObj(records []*routeRecord, sr *schemaRegistry) *obj {
	paths := newObj()
	for _, rec := range records {
		if rec.omit {
			continue
		}
		op := d.operationFor(rec, sr)
		docP := docPath(rec.full)
		pathItem, _ := paths.get(docP).(*obj)
		if pathItem == nil {
			pathItem = newObj()
			paths.set(docP, pathItem)
		}
		pathItem.set(strings.ToLower(rec.method), op)
	}
	return paths
}
