package specout

import (
	"slices"
	"strconv"
	"strings"
)

// securitySchemeObj builds the named scheme entry from the doc-only
// AuthScheme declaration. A typo in Type, Key, In or URL would emit a
// securityScheme object the OpenAPI validator rejects (type must be one of
// apiKey/http/mutualTLS/oauth2/openIdConnect; apiKey needs a real in), so
// the declaration is checked here rather than shipped broken.
func securitySchemeObj(a AuthScheme) *obj {
	// the name is a securitySchemes key: the same rule as a component name
	if a.Name == "" || componentChars.MatchString(a.Name) {
		panic("specout: Config.Auth: scheme name " + strconv.Quote(a.Name) +
			" must contain only ASCII letters, digits, dots, hyphens or underscores")
	}
	return schemeOfType(a).setIf("description", a.Description)
}

// schemeOfType builds the fields that a scheme's Type selects.
func schemeOfType(a AuthScheme) *obj {
	switch a.Type {
	case authHTTP:
		return httpScheme(a)
	case authMutualTLS:
		return newObj().set("type", authMutualTLS)
	case authAPIKey:
		return apiKeyScheme(a)
	case authOAuth2:
		if len(a.Flows) == 0 {
			panic("specout: " + authWhere(a) + ": oauth2 needs Flows")
		}
		return newObj().set("type", authOAuth2).set("flows", oauth2Flows(a))
	case authOpenIDConnect:
		if a.URL == "" {
			panic("specout: " + authWhere(a) + ": openIdConnect needs a URL, the discovery document")
		}
		return newObj().set("type", authOpenIDConnect).set("openIdConnectUrl", a.URL)
	default:
		types := []string{authHTTP, authAPIKey, authOAuth2, authOpenIDConnect, authMutualTLS}
		panic("specout: " + authWhere(a) + ": Type " + strconv.Quote(a.Type) + " is not valid; use " +
			strings.Join(types, ", ") + didYouMean(a.Type, types))
	}
}

// authWhere names a scheme in a message.
func authWhere(a AuthScheme) string { return "Config.Auth " + strconv.Quote(a.Name) }

// httpScheme builds an http scheme.
func httpScheme(a AuthScheme) *obj {
	if a.Scheme == "" {
		panic("specout: " + authWhere(a) + `: http needs a Scheme, such as "bearer" or "basic"`)
	}
	if a.BearerFormat != "" && !strings.EqualFold(a.Scheme, "bearer") {
		panic("specout: " + authWhere(a) + ": BearerFormat needs the scheme \"bearer\", but the scheme is " +
			strconv.Quote(a.Scheme))
	}
	return newObj().set("type", authHTTP).set("scheme", a.Scheme).setIf("bearerFormat", a.BearerFormat)
}

// checkSecurity panics when a route's Security names a scheme that Config.Auth
// does not declare, or when a Public route also sets it.
func (d *Generator) checkSecurity(rec *routeRecord) {
	if rec.public && len(rec.security) > 0 {
		panic("specout: " + rec.label() + ": sets both Public and Security; set one")
	}
	names := make([]string, 0, len(d.cfg.Auth))
	for _, a := range d.cfg.Auth {
		names = append(names, a.Name)
	}
	for _, sec := range rec.security {
		if !slices.Contains(names, sec.Scheme) {
			panic("specout: " + rec.label() + ": Security names scheme " + strconv.Quote(sec.Scheme) +
				", but Config.Auth does not declare it" + didYouMean(sec.Scheme, names))
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
		panic("specout: " + authWhere(a) + ": apiKey needs a Key, the parameter name")
	}
	switch a.In {
	case InHeader, InQuery, InCookie:
	default:
		panic("specout: " + authWhere(a) + ": In " + strconv.Quote(a.In) + " is not valid; use header, query or cookie")
	}
	return newObj().set("type", authAPIKey).set("name", a.Key).set("in", a.In)
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
			names := []string{"implicit", "password", "clientCredentials", "authorizationCode"}
			panic("specout: " + authWhere(a) + ": flow " + strconv.Quote(name) + " is not valid; use " +
				strings.Join(names, ", ") + didYouMean(name, names))
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
		panic("specout: " + authWhere(a) + ": flow " + strconv.Quote(flow) + " needs " + field)
	}
}
