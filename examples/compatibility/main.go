// Command compatibility exports small typed reconstructions of public APIs.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/happytoolin/specout"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: compatibility github|stripe|cloudflare")
	}
	d := document(os.Args[1])
	if d == nil {
		log.Fatal("unknown API; choose github, stripe, or cloudflare")
	}
	if err := d.WriteJSON(os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func noop(http.ResponseWriter, *http.Request) {}

func document(name string) *specout.Generator {
	switch name {
	case "github":
		return githubDocument()
	case "stripe":
		return stripeDocument()
	case "cloudflare":
		return cloudflareDocument()
	default:
		return nil
	}
}

func githubDocument() *specout.Generator {
	d := specout.New(specout.Config{Title: "GitHub compatibility sample", Version: "1", Servers: []specout.Server{{URL: "https://api.github.com"}}})
	specout.Document(d, "GET", "/licenses", specout.Handler[githubLicenseQuery, []githubLicense]{HandlerFunc: noop, OperationID: "licenses/get-all-commonly-used", Responses: []specout.Response{{Status: 304}}})
	specout.Document(d, "GET", "/repos/{owner}/{repo}/topics", specout.Handler[githubTopicQuery, githubTopics]{HandlerFunc: noop, OperationID: "repos/get-all-topics", Responses: []specout.Response{{Status: 404, Type: githubError{}}}})
	specout.Document(d, "PUT", "/repos/{owner}/{repo}/topics", specout.Handler[githubTopicUpdate, githubTopics]{HandlerFunc: noop, OperationID: "repos/replace-all-topics", Responses: []specout.Response{{Status: 404, Type: githubError{}}, {Status: 422, Type: githubValidationError{}}}})
	return d
}

func stripeDocument() *specout.Generator {
	d := specout.New(specout.Config{Title: "Stripe compatibility sample", Version: "1", Servers: []specout.Server{{URL: "https://api.stripe.com"}}})
	// Stripe's default error links to hundreds of expandable resources. This
	// sample compares success payloads and the declared default status only.
	defaults := []specout.Response{{Key: "default", Type: map[string]any{}}}
	// Stripe declares an optional empty form body even on these GET endpoints.
	// Raw is the existing escape hatch for operation fields outside the typed API.
	raw := map[string]any{"requestBody": map[string]any{"required": false, "content": map[string]any{"application/x-www-form-urlencoded": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}}}}
	specout.Document(d, "GET", "/v1/country_specs", specout.Handler[stripeCountryQuery, stripeList[stripeCountry]]{HandlerFunc: noop, OperationID: "GetCountrySpecs", Responses: defaults, Raw: raw})
	specout.Document(d, "GET", "/v1/country_specs/{country}", specout.Handler[stripeCountryPath, stripeCountry]{HandlerFunc: noop, OperationID: "GetCountrySpecsCountry", Responses: defaults, Raw: raw})
	return d
}

func cloudflareDocument() *specout.Generator {
	d := specout.New(specout.Config{Title: "Cloudflare compatibility sample", Version: "1", Servers: []specout.Server{{URL: "https://api.cloudflare.com/client/v4"}}, Auth: []specout.AuthScheme{{Name: "api_token", Type: "httpBearer"}}})
	specout.Document(d, "GET", "/user/tokens/verify", specout.Get[cloudflareVerify]{HandlerFunc: noop, OperationID: "user-api-tokens-verify-token", Responses: []specout.Response{{Key: "4XX", Type: map[string]any{}}}})
	return d
}
