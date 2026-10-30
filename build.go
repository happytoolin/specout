package specout

import (
	"slices"
	"strconv"
	"strings"
)

// build assembles the OpenAPI document: resolve the routes, reflect schemas,
// emit operations in path order.
func (d *Generator) build() (*obj, error) {
	if err := d.resolveLocked(); err != nil {
		return nil, err
	}
	if err := d.checkChiMount(); err != nil {
		return nil, err
	}
	if err := checkOperationIDs(d.records); err != nil {
		return nil, err
	}

	spec := newObj().set("openapi", "3.1.0").set("info", d.infoObj())
	if servers := d.serverObjs(); len(servers) > 0 {
		spec.set("servers", servers)
	}

	paths, schemas := d.pathsAndSchemas()
	spec.set("paths", paths)
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
	if len(schemas.keys) > 0 {
		components.set("schemas", schemas)
	}
	if len(components.keys) > 0 {
		spec.set("components", components)
	}
	if d.cfg.ExternalDocs != nil {
		spec.set("externalDocs", externalDocsObj(d.cfg.ExternalDocs))
	}
	return mergeRaw(spec, d.cfg.Raw), nil
}

// checkConfig builds the parts of the document that come from Config alone
// and drops them, so a Config that would break the document panics in New.
func (d *Generator) checkConfig() {
	d.infoObj()
	d.serverObjs()
	d.tagObjs()
	checkRawKeys(d.cfg.Raw, rootFields)
	d.securityObjs()
	if d.cfg.ExternalDocs != nil {
		externalDocsObj(d.cfg.ExternalDocs)
	}
	d.checkDefaultErrors()
}

// pathsAndSchemas reflects the routes into the paths object and the schema
// components. An anonymous struct takes the next AnonymousN name that no
// named type uses. When a named type claims a name that an anonymous struct
// took first, the reflection runs again with that name reserved, so the names
// do not depend on the route order.
func (d *Generator) pathsAndSchemas() (*obj, *obj) {
	reserved := make(map[string]bool)
	for {
		if paths, schemas, ok := d.reflectRoutes(reserved); ok {
			return paths, schemas
		}
	}
}

// reflectRoutes is one run of pathsAndSchemas. It reports false, and adds the
// name to reserved, when a named type claims a name that an anonymous struct
// holds: the recovered run returns its zero results.
func (d *Generator) reflectRoutes(reserved map[string]bool) (*obj, *obj, bool) {
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
	// paths first: it is what fills sr, and components reads sr back.
	paths := d.pathObj(d.records, sr)
	return paths, sr.schemaObjs(), true
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
			panic("specout: License needs a Name")
		}
		if l.URL != "" && l.Identifier != "" {
			panic("specout: License sets both URL and Identifier; set one")
		}
		info.set("license", newObj().set("name", l.Name).setIf("identifier", l.Identifier).setIf("url", l.URL))
	}
	return info
}

// serverObjs is the server list, empty when the config declares none.
func (d *Generator) serverObjs() []any {
	servers := make([]any, 0, len(d.cfg.Servers))
	for _, s := range d.cfg.Servers {
		server := newObj().set("url", s.URL).setIf("description", s.Description)
		if len(s.Variables) > 0 {
			server.set("variables", serverVariableObjs(s))
		}
		servers = append(servers, server)
	}
	return servers
}

// serverVariableObjs is the variables object of one server. A Go map has no
// order, so the names are sorted.
func serverVariableObjs(s Server) *obj {
	vars := newObj()
	for _, name := range sortedKeys(s.Variables) {
		v := s.Variables[name]
		if v.Default == "" {
			panic("specout: server " + s.URL + " variable " + name + " needs a Default")
		}
		o := newObj().set("default", v.Default)
		if len(v.Enum) > 0 {
			if !slices.Contains(v.Enum, v.Default) {
				panic("specout: server " + s.URL + " variable " + name + " has a Default that Enum does not hold")
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
			panic("specout: empty or duplicate tag name " + strconv.Quote(t.Name))
		}
		seen[t.Name] = true
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
		// a second scheme with one name would replace the first
		if schemes.get(a.Name) != nil {
			panic("specout: duplicate AuthScheme name " + a.Name)
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
