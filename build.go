package specout

import (
	"slices"
	"strings"
)

// build assembles the OpenAPI document: resolve the routes, reflect schemas,
// emit operations in path order.
func (d *Generator) build() (*obj, error) {
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

	spec := newObj()
	info := newObj().set("title", d.cfg.Title).set("version", d.cfg.Version)
	info.setIf("description", d.cfg.Description)
	info.setIf("termsOfService", d.cfg.TermsOfService)
	if c := d.cfg.Contact; c != nil {
		info.set("contact", newObj().setIf("name", c.Name).setIf("url", c.URL).setIf("email", c.Email))
	}
	if l := d.cfg.License; l != nil {
		info.set("license", newObj().setIf("name", l.Name).setIf("url", l.URL))
	}
	spec.set("openapi", "3.1.0").set("info", info)
	if len(d.cfg.Servers) > 0 {
		servers := make([]any, 0, len(d.cfg.Servers))
		for _, s := range d.cfg.Servers {
			servers = append(servers, newObj().set("url", s.URL).setIf("description", s.Description))
		}
		spec.set("servers", servers)
	}

	paths := newObj()
	sr := newSchemaRegistry(d.cfg)
	for name, t := range d.variants {
		sr.registerVariant(t, name)
	}
	for t, name := range d.nameOverrides {
		sr.overrideName(t, name)
	}
	for _, rec := range records {
		op := d.operationFor(rec, sr)
		if rec.omit {
			continue
		}
		docP := docPath(rec.full)
		pathItem, _ := paths.get(docP).(*obj)
		if pathItem == nil {
			pathItem = newObj()
			paths.set(docP, pathItem)
		}
		pathItem.set(strings.ToLower(rec.method), op)
	}
	spec.set("paths", paths)

	if len(d.cfg.Tags) > 0 {
		tags := make([]any, 0, len(d.cfg.Tags))
		for _, t := range d.cfg.Tags {
			e := newObj().set("name", t.Name).setIf("description", t.Description)
			if t.ExternalDocs != nil {
				e.set("externalDocs", externalDocsObj(t.ExternalDocs))
			}
			tags = append(tags, e)
		}
		spec.set("tags", tags)
	}

	// security: one named scheme, top-level requirement; per-op override
	// on public routes (security: []).
	components := newObj()
	if len(d.cfg.Auth) > 0 {
		schemes := newObj()
		alts := make([]any, 0, len(d.cfg.Auth))
		for _, a := range d.cfg.Auth {
			schemes.set(a.Name, securitySchemeObj(a))
			alts = append(alts, newObj().set(a.Name, []any{}))
		}
		components.set("securitySchemes", schemes)
		spec.set("security", alts)
	}
	if len(sr.order) > 0 {
		schemas := newObj()
		for _, t := range sr.order {
			e := sr.byType[t]
			schemas.set(e.name, e.s)
		}
		// hoisted $defs with no Go type: emitted after the typed components
		for _, n := range sr.nameOrder {
			if !schemas.has(n) {
				schemas.set(n, sr.byName[n])
			}
		}
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
