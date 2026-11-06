package specout

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// buildDoc assembles the OpenAPI document: resolve the routes, reflect schemas,
// emit operations in path order.
func (g *Generator) buildDoc() (*obj, error) {
	resolveErr := g.resolveLocked()
	ids, idErr := checkOperationIDs(g.operations())
	if err := errors.Join(resolveErr, idErr, g.checkLinks(ids)); err != nil {
		return nil, err
	}

	spec := newObj().set("openapi", "3.1.0").set("info", g.infoObj())
	if servers := serverObjs("Config.Servers", g.cfg.Servers); len(servers) > 0 {
		spec.set("servers", servers)
	}

	objs := g.pathsAndSchemas()
	spec.set("paths", objs.paths)
	if len(objs.webhooks.keys) > 0 {
		spec.set("webhooks", objs.webhooks)
	}
	if tags := g.tagObjs(); len(tags) > 0 {
		spec.set("tags", tags)
	}
	g.setComponents(spec, objs.schemas)
	if g.cfg.ExternalDocs != nil {
		spec.set("externalDocs", externalDocsObj("Config", g.cfg.ExternalDocs))
	}
	return mergeRaw(spec, g.cfg.Raw), nil
}

// setComponents adds the components object and the document's security
// requirement to spec.
func (g *Generator) setComponents(spec, schemas *obj) {
	components := newObj()
	if len(g.cfg.Auth) > 0 {
		// security: one named scheme per Auth entry, plus a top-level
		// requirement that any one of them satisfies (OR semantics).
		schemes, alts := g.securityObjs()
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
func (g *Generator) checkConfig() {
	if g.cfg.Title == "" || g.cfg.Version == "" {
		panic("specout: Config: set Title and Version; OpenAPI requires both")
	}
	g.infoObj()
	serverObjs("Config.Servers", g.cfg.Servers)
	g.tagObjs()
	checkRawKeys("Config", g.cfg.Raw, nil)
	g.securityObjs()
	if g.cfg.ExternalDocs != nil {
		externalDocsObj("Config", g.cfg.ExternalDocs)
	}
	g.checkDefaultErrors()
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
func (g *Generator) pathsAndSchemas() reflectedObjs {
	reserved := make(map[string]bool)
	for {
		if objs, ok := g.reflectRoutes(reserved); ok {
			return objs
		}
	}
}

// reflectRoutes is one run of pathsAndSchemas. It reports false, and adds the
// name to reserved, when a named type claims a name that an anonymous struct
// holds: the recovered run returns its zero results.
func (g *Generator) reflectRoutes(reserved map[string]bool) (reflectedObjs, bool) {
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
	sr := g.newSchemaRegistry()
	sr.reserved = reserved
	// paths and webhooks first: they fill sr, and components reads sr back.
	paths := g.pathObj(g.records, sr)
	webhooks := g.webhookObjs(sr)
	return reflectedObjs{paths: paths, webhooks: webhooks, schemas: sr.schemaObjs()}, true
}

func (g *Generator) infoObj() *obj {
	info := newObj().set("title", g.cfg.Title).set("version", g.cfg.Version)
	info.setIf("summary", g.cfg.Summary)
	info.setIf("description", g.cfg.Description)
	info.setIf("termsOfService", g.cfg.TermsOfService)
	checkURL("Config", "TermsOfService", g.cfg.TermsOfService)
	if c := g.cfg.Contact; c != nil {
		checkURL("Config.Contact", "URL", c.URL)
		checkEmail("Config.Contact", c.Email)
		info.set("contact", newObj().setIf("name", c.Name).setIf("url", c.URL).setIf("email", c.Email))
	}
	if l := g.cfg.License; l != nil {
		// OpenAPI requires the license name; a URL alone is invalid.
		if l.Name == "" {
			panic("specout: Config.License: set Name; OpenAPI requires it")
		}
		if l.URL != "" && l.Identifier != "" {
			panic("specout: Config.License: sets both URL and Identifier; set one")
		}
		checkURL("Config.License", "URL", l.URL)
		info.set("license", newObj().set("name", l.Name).setIf("identifier", l.Identifier).setIf("url", l.URL))
	}
	return info
}

func serverObjs(where string, servers []Server) []any {
	out := make([]any, 0, len(servers))
	for _, s := range servers {
		out = append(out, serverObj(where, s))
	}
	return out
}

func serverObj(where string, s Server) *obj {
	if s.URL == "" {
		panic("specout: " + where + ": a server needs a URL")
	}
	server := newObj().set("url", s.URL).setIf("description", s.Description)
	if vars := serverVariableObjs(where+" "+strconv.Quote(s.URL), s); len(vars.keys) > 0 {
		server.set("variables", vars)
	}
	return server
}

// serverVarRe matches one {name} placeholder of a server URL.
var serverVarRe = regexp.MustCompile(`{([^{}]*)}`)

// serverVariableObjs is the variables object of one server. A Go map has no
// order, so the names are sorted. Each variable fills a {name} placeholder of
// the URL, and each placeholder needs a variable, or no tool can resolve the
// URL.
func serverVariableObjs(where string, s Server) *obj {
	matches := serverVarRe.FindAllStringSubmatch(s.URL, -1)
	placeholders := make([]string, 0, len(matches))
	for _, m := range matches {
		placeholders = append(placeholders, m[1])
		if _, ok := s.Variables[m[1]]; !ok {
			panic("specout: " + where + ": the URL has {" + m[1] + "}, but Variables does not declare it; " +
				"add it with its Default")
		}
	}
	vars := newObj()
	for _, name := range sortedKeys(s.Variables) {
		v := s.Variables[name]
		if !slices.Contains(placeholders, name) {
			panic("specout: " + where + ": variable " + strconv.Quote(name) + " has no {" + name + "} in the URL" +
				didYouMean(name, placeholders))
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
func (g *Generator) tagObjs() []any {
	tags := make([]any, 0, len(g.cfg.Tags))
	seen := make(map[string]bool, len(g.cfg.Tags))
	for _, t := range g.cfg.Tags {
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
func (g *Generator) securityObjs() (*obj, []any) {
	schemes := newObj()
	alts := make([]any, 0, len(g.cfg.Auth))
	for _, a := range g.cfg.Auth {
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
func (g *Generator) pathObj(records []*routeRecord, sr *schemaRegistry) *obj {
	paths := newObj()
	for _, rec := range records {
		if rec.isCatchAll() {
			continue
		}
		op := g.operationFor(rec, sr)
		paths.child(docPath(rec.full)).set(strings.ToLower(rec.method), op)
	}
	return paths
}
