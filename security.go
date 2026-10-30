package specout

import (
	"slices"
	"strconv"
	"strings"
)

// typeHTTPBearer is the AuthScheme.Type of http with the bearer scheme.
const typeHTTPBearer = "httpBearer"

// securitySchemeObj builds the named scheme entry from the doc-only
// AuthScheme declaration. A typo in Type, Key, In or URL would emit a
// securityScheme object the OpenAPI validator rejects (type must be one of
// apiKey/http/mutualTLS/oauth2/openIdConnect; apiKey needs a real in), so
// the declaration is checked here rather than shipped broken.
func securitySchemeObj(a AuthScheme) *obj {
	// the name is a securitySchemes key: the same rule as a component name
	if a.Name == "" || componentChars.MatchString(a.Name) {
		panic("specout: AuthScheme.Name " + strconv.Quote(a.Name) +
			" must contain only ASCII letters, digits, dots, hyphens or underscores")
	}
	return schemeOfType(a).setIf("description", a.Description)
}

// schemeOfType builds the fields that a scheme's Type selects.
func schemeOfType(a AuthScheme) *obj {
	switch a.Type {
	case typeHTTPBearer, "http":
		return httpScheme(a)
	case "mutualTLS":
		return newObj().set("type", "mutualTLS")
	case "apiKey":
		return apiKeyScheme(a)
	case "oauth2":
		if len(a.Flows) == 0 {
			panic("specout: AuthScheme " + a.Name + " is oauth2 but has no Flows")
		}
		return newObj().set("type", "oauth2").set("flows", oauth2Flows(a))
	case "openIdConnect":
		if a.URL == "" {
			panic("specout: AuthScheme " + a.Name + " is openIdConnect but has no URL")
		}
		return newObj().set("type", "openIdConnect").set("openIdConnectUrl", a.URL)
	default:
		panic("specout: AuthScheme " + a.Name + " has type " + a.Type +
			", must be httpBearer, http, apiKey, oauth2, openIdConnect or mutualTLS")
	}
}

// httpScheme builds an http scheme. httpBearer is http with the bearer scheme.
func httpScheme(a AuthScheme) *obj {
	scheme := a.Scheme
	if a.Type == typeHTTPBearer {
		scheme = "bearer"
	}
	if scheme == "" {
		panic("specout: AuthScheme " + a.Name + " is http but has no Scheme")
	}
	if a.BearerFormat != "" && !strings.EqualFold(scheme, "bearer") {
		panic("specout: AuthScheme " + a.Name + " sets BearerFormat, but its scheme is " + scheme)
	}
	return newObj().set("type", "http").set("scheme", scheme).setIf("bearerFormat", a.BearerFormat)
}

// checkSecurity panics when a route's Security names a scheme that Config.Auth
// does not declare, or when a Public route also sets it.
func (d *Generator) checkSecurity(rec *routeRecord) {
	if rec.public && len(rec.security) > 0 {
		panic("specout: " + rec.method + " " + rec.pattern + " sets both Public and Security; set one")
	}
	for _, sec := range rec.security {
		if !slices.ContainsFunc(d.cfg.Auth, func(a AuthScheme) bool { return a.Name == sec.Scheme }) {
			panic("specout: " + rec.method + " " + rec.pattern + " names security scheme " +
				strconv.Quote(sec.Scheme) + ", which Config.Auth does not declare")
		}
	}
}

// routeSecurity is the operation's security field, or nil when the operation
// follows the document's. Each entry is one alternative.
func (d *Generator) routeSecurity(rec *routeRecord) []any {
	if rec.public && len(d.cfg.Auth) > 0 {
		return []any{}
	}
	if len(rec.security) == 0 {
		return nil
	}
	alts := make([]any, 0, len(rec.security))
	for _, sec := range rec.security {
		alts = append(alts, newObj().set(sec.Scheme, append([]string{}, sec.Scopes...)))
	}
	return alts
}

// apiKeyScheme builds an apiKey scheme. It needs the parameter name, and a
// location that OpenAPI allows.
func apiKeyScheme(a AuthScheme) *obj {
	if a.Key == "" {
		panic("specout: AuthScheme " + a.Name + " is apiKey but has no Key (the parameter name)")
	}
	switch a.In {
	case InHeader, InQuery, InCookie:
	default:
		panic("specout: AuthScheme " + a.Name + " has in=" + a.In + ", must be header, query or cookie")
	}
	return newObj().set("type", "apiKey").set("name", a.Key).set("in", a.In)
}

// oauth2Flows emits an oauth2 scheme's flows object. Flow and scope names are
// sorted: a Go map has no order and the document must stay byte-stable.
func oauth2Flows(a AuthScheme) *obj {
	flows := newObj()
	for _, name := range sortedKeys(a.Flows) {
		f := a.Flows[name]
		fo := newObj()
		// OpenAPI requires a URL per flow: implicit and authorizationCode
		// authorize in the browser, the other two take credentials directly.
		switch name {
		case "implicit":
			requireURL(a, name, "AuthorizationURL", f.AuthorizationURL)
			fo.set("authorizationUrl", f.AuthorizationURL)
		case "password", "clientCredentials":
			requireURL(a, name, "TokenURL", f.TokenURL)
			fo.set("tokenUrl", f.TokenURL)
		case "authorizationCode":
			requireURL(a, name, "AuthorizationURL", f.AuthorizationURL)
			requireURL(a, name, "TokenURL", f.TokenURL)
			fo.set("authorizationUrl", f.AuthorizationURL).set("tokenUrl", f.TokenURL)
		default:
			panic("specout: AuthScheme " + a.Name + " has flow " + name +
				", must be implicit, password, clientCredentials or authorizationCode")
		}
		if f.RefreshURL != "" {
			fo.set("refreshUrl", f.RefreshURL)
		}
		scopes := newObj()
		for _, s := range sortedKeys(f.Scopes) {
			scopes.set(s, f.Scopes[s])
		}
		flows.set(name, fo.set("scopes", scopes))
	}
	return flows
}

func requireURL(a AuthScheme, flow, field, url string) {
	if url == "" {
		panic("specout: AuthScheme " + a.Name + " flow " + flow + " has no " + field)
	}
}
