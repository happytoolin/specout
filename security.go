package specout

// securitySchemeObj builds the named scheme entry from the doc-only
// AuthScheme declaration. A typo in Type, Key, In or URL would emit a
// securityScheme object the OpenAPI validator rejects (type must be one of
// apiKey/http/mutualTLS/oauth2/openIdConnect; apiKey needs a real in), so
// the declaration is checked here rather than shipped broken.
func securitySchemeObj(a AuthScheme) *obj {
	if a.Name == "" {
		panic("specout: AuthScheme.Name must not be empty (it is the securitySchemes key)")
	}
	o := newObj()
	switch a.Type {
	case "httpBearer":
		o.set("type", "http").set("scheme", "bearer")
	case "apiKey":
		if a.Key == "" {
			panic("specout: AuthScheme " + a.Name + " is apiKey but has no Key (the parameter name)")
		}
		switch a.In {
		case InHeader, InQuery, InCookie:
		default:
			panic("specout: AuthScheme " + a.Name + " has in=" + a.In + ", must be header, query or cookie")
		}
		o.set("type", "apiKey").set("name", a.Key).set("in", a.In)
	case "oauth2":
		if len(a.Flows) == 0 {
			panic("specout: AuthScheme " + a.Name + " is oauth2 but has no Flows")
		}
		o.set("type", "oauth2").set("flows", oauth2Flows(a))
	case "openIdConnect":
		if a.URL == "" {
			panic("specout: AuthScheme " + a.Name + " is openIdConnect but has no URL")
		}
		o.set("type", "openIdConnect").set("openIdConnectUrl", a.URL)
	default:
		panic("specout: AuthScheme " + a.Name + " has type " + a.Type +
			", must be httpBearer, apiKey, oauth2 or openIdConnect")
	}
	return o
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
