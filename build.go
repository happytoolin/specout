package specout

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

// buildDoc assembles the OpenAPI document: resolve the routes, reflect schemas,
// emit operations in path order.
func (d *Generator) buildDoc() (*obj, error) {
	err := errors.Join(d.resolveLocked(), d.checkChiMount(), checkOperationIDs(d.operations()), d.checkLinks())
	if err != nil {
		return nil, err
	}

	spec := newObj().set("openapi", "3.1.0").set("info", d.infoObj())
	spec.setIf("jsonSchemaDialect", d.cfg.JSONSchemaDialect)
	if servers := serverObjs("Config.Servers", d.cfg.Servers); len(servers) > 0 {
		spec.set("servers", servers)
	}

	objs := d.pathsAndSchemas()
	spec.set("paths", objs.paths)
	if len(objs.webhooks.keys) > 0 {
		spec.set("webhooks", objs.webhooks)
	}
	if tags := d.tagObjs(); len(tags) > 0 {
		spec.set("tags", tags)
	}
	d.setComponents(spec, objs.schemas)
	if d.cfg.ExternalDocs != nil {
		spec.set("externalDocs", externalDocsObj("Config", d.cfg.ExternalDocs))
	}
	return mergeRaw(spec, d.cfg.Raw), nil
}

// setComponents adds the components object and the document's security
// requirement to spec.
func (d *Generator) setComponents(spec, schemas *obj) {
	components := newObj()
	if len(d.cfg.Auth) > 0 {
		// security: one named scheme per Auth entry, plus a top-level
		// requirement that any one of them satisfies (OR semantics).
		schemes, alts := d.securityObjs()
		components.set("securitySchemes", schemes)
		spec.set("security", alts)
	}
	if len(schemas.keys) > 0 {
		components.set("schemas", schemas)
	}
	if len(components.keys) > 0 {
		spec.set("components", components)
	}
}

// checkConfig builds the parts of the document that come from Config alone
// and drops them, so a Config that would break the document panics in New.
func (d *Generator) checkConfig() {
	if d.cfg.Title == "" || d.cfg.Version == "" {
		panic("specout: Config: set Title and Version; OpenAPI requires both")
	}
	d.infoObj()
	serverObjs("Config.Servers", d.cfg.Servers)
	d.tagObjs()
	checkRawKeys("Config", d.cfg.Raw, nil)
	d.securityObjs()
	if d.cfg.ExternalDocs != nil {
		externalDocsObj("Config", d.cfg.ExternalDocs)
	}
	d.checkDefaultErrors()
}

// reflectedObjs are the parts of the document that fill the schema registry.
type reflectedObjs struct {
	paths, webhooks, schemas *obj
}

// pathsAndSchemas reflects the routes and webhooks into their objects and the
// schema components. An anonymous struct takes the next AnonymousN name that no
// named type uses. When a named type claims a name that an anonymous struct
// took first, the reflection runs again with that name reserved, so the names
// do not depend on the route order.
func (d *Generator) pathsAndSchemas() reflectedObjs {
	reserved := make(map[string]bool)
	for {
		if objs, ok := d.reflectRoutes(reserved); ok {
			return objs
		}
	}
}

// reflectRoutes is one run of pathsAndSchemas. It reports false, and adds the
// name to reserved, when a named type claims a name that an anonymous struct
// holds: the recovered run returns its zero results.
func (d *Generator) reflectRoutes(reserved map[string]bool) (reflectedObjs, bool) {
	defer func() {
		r := recover()
		clash, isClash := r.(nameClash)
		switch {
		case isClash:
			reserved[string(clash)] = true
		case r != nil:
			panic(r)
		}
	}()
	sr := d.newSchemaRegistry()
	sr.reserved = reserved
	// paths and webhooks first: they fill sr, and components reads sr back.
	paths := d.pathObj(d.records, sr)
	webhooks := d.webhookObjs(sr)
	return reflectedObjs{paths: paths, webhooks: webhooks, schemas: sr.schemaObjs()}, true
}

// infoObj is the document's info object.
func (d *Generator) infoObj() *obj {
	info := newObj().set("title", d.cfg.Title).set("version", d.cfg.Version)
	info.setIf("summary", d.cfg.Summary)
	info.setIf("description", d.cfg.Description)
	info.setIf("termsOfService", d.cfg.TermsOfService)
	if c := d.cfg.Contact; c != nil {
		info.set("contact", newObj().setIf("name", c.Name).setIf("url", c.URL).setIf("email", c.Email))
	}
	if l := d.cfg.License; l != nil {
		// OpenAPI requires the license name; a URL alone is invalid.
		if l.Name == "" {
			panic("specout: Config.License: set Name; OpenAPI requires it")
		}
		if l.URL != "" && l.Identifier != "" {
			panic("specout: Config.License: sets both URL and Identifier; set one")
		}
		info.set("license", newObj().set("name", l.Name).setIf("identifier", l.Identifier).setIf("url", l.URL))
	}
	return info
}

// serverObjs is a server list. where names the list.
func serverObjs(where string, servers []Server) []any {
	out := make([]any, 0, len(servers))
	for _, s := range servers {
		out = append(out, serverObj(where, s))
	}
	return out
}

// serverObj is one server object.
func serverObj(where string, s Server) *obj {
	if s.URL == "" {
		panic("specout: " + where + ": a server needs a URL")
	}
	server := newObj().set("url", s.URL).setIf("description", s.Description)
	if len(s.Variables) > 0 {
		server.set("variables", serverVariableObjs(where+" "+strconv.Quote(s.URL), s))
	}
	return server
}

// serverVariableObjs is the variables object of one server. A Go map has no
// order, so the names are sorted.
func serverVariableObjs(where string, s Server) *obj {
	vars := newObj()
	for _, name := range sortedKeys(s.Variables) {
		v := s.Variables[name]
		if v.Default == "" {
			panic("specout: " + where + ": variable " + strconv.Quote(name) + " needs a Default")
		}
		o := newObj().set("default", v.Default)
		if len(v.Enum) > 0 {
			if !slices.Contains(v.Enum, v.Default) {
				panic("specout: " + where + ": variable " + strconv.Quote(name) + ": Enum does not hold the Default " +
					strconv.Quote(v.Default) + "; add it to Enum")
			}
			o.set("enum", v.Enum)
		}
		vars.set(name, o.setIf("description", v.Description))
	}
	return vars
}

// tagObjs is the declared tag list. OpenAPI requires each tag name, and
// requires the names to be unique.
func (d *Generator) tagObjs() []any {
	tags := make([]any, 0, len(d.cfg.Tags))
	seen := make(map[string]bool, len(d.cfg.Tags))
	for _, t := range d.cfg.Tags {
		if t.Name == "" || seen[t.Name] {
			panic("specout: Config.Tags: tag name " + strconv.Quote(t.Name) + " is empty or repeated; give each tag one name")
		}
		seen[t.Name] = true
		e := newObj().set("name", t.Name).setIf("description", t.Description)
		if t.ExternalDocs != nil {
			e.set("externalDocs", externalDocsObj("Config.Tags "+strconv.Quote(t.Name), t.ExternalDocs))
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
		// a second scheme with one name would replace the first
		if schemes.get(a.Name) != nil {
			panic("specout: Config.Auth " + strconv.Quote(a.Name) + ": the name is repeated; give each scheme one name")
		}
		schemes.set(a.Name, securitySchemeObj(a))
		alts = append(alts, newObj().set(a.Name, []any{}))
	}
	return schemes, alts
}

// pathObj reflects operations in registration order and groups them by
// documented path. It is what fills sr.
func (d *Generator) pathObj(records []*routeRecord, sr *schemaRegistry) *obj {
	paths := newObj()
	for _, rec := range records {
		if rec.isCatchAll() {
			continue
		}
		op := d.operationFor(rec, sr)
		paths.child(docPath(rec.full)).set(strings.ToLower(rec.method), op)
	}
	return paths
}
