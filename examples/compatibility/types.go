package main

import "github.com/invopop/jsonschema"

// The fixtures use ordinary Go types. A named constrained scalar uses the
// reflector's existing JSONSchema hook for constraints on collection items.
type stripeText string

func (stripeText) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", MaxLength: new(uint64(5000))}
}

type githubPagination struct {
	Page    int `json:"-" jsonschema:"default=1"  query:"page,omitempty"`
	PerPage int `json:"-" jsonschema:"default=30" query:"per_page,omitempty"`
}

type githubLicenseQuery struct {
	githubPagination

	Featured bool `json:"-" query:"featured,omitempty"`
}

type githubLicense struct {
	Key     string  `json:"key"`
	Name    string  `json:"name"`
	URL     *string `json:"url"                jsonschema:"format=uri"`
	SPDXID  *string `json:"spdx_id"`
	NodeID  string  `json:"node_id"`
	HTMLURL string  `json:"html_url,omitempty" jsonschema:"format=uri"`
}

type githubRepository struct {
	Owner string `json:"-" path:"owner"`
	Repo  string `json:"-" path:"repo"`
}

type (
	githubTopicQuery struct {
		githubRepository
		githubPagination
	}
	githubTopicUpdate struct {
		githubRepository

		Names []string `json:"names"`
	}
)

type (
	githubTopics struct {
		Names []string `json:"names"`
	}
	githubError struct {
		Message          string `json:"message,omitempty"`
		DocumentationURL string `json:"documentation_url,omitempty"`
		URL              string `json:"url,omitempty"`
		Status           string `json:"status,omitempty"`
	}
)

type githubValidationError struct {
	Message          string   `json:"message"`
	DocumentationURL string   `json:"documentation_url"`
	Errors           []string `json:"errors,omitempty"`
}

type stripeCountryQuery struct {
	EndingBefore  stripeText   `jsonschema:"style=form"                    query:"ending_before,omitempty"`
	Expand        []stripeText `jsonschema:"style=deepObject,explode=true" query:"expand,omitempty"`
	Limit         int          `jsonschema:"style=form"                    query:"limit,omitempty"`
	StartingAfter stripeText   `jsonschema:"style=form"                    query:"starting_after,omitempty"`
}
type stripeCountryPath struct {
	Country stripeText   `jsonschema:"style=simple"                  path:"country"`
	Expand  []stripeText `jsonschema:"style=deepObject,explode=true" query:"expand,omitempty"`
}
type stripeCountry struct {
	DefaultCurrency                stripeText              `json:"default_currency"`
	ID                             stripeText              `json:"id"`
	Object                         string                  `json:"object"                            jsonschema:"enum=country_spec"`
	SupportedBankAccountCurrencies map[string][]stripeText `json:"supported_bank_account_currencies"`
	SupportedPaymentCurrencies     []stripeText            `json:"supported_payment_currencies"`
	SupportedPaymentMethods        []stripeText            `json:"supported_payment_methods"`
	SupportedTransferCountries     []stripeText            `json:"supported_transfer_countries"`
	VerificationFields             stripeVerification      `json:"verification_fields"`
}
type stripeVerification struct {
	Company    stripeVerificationFields `json:"company"`
	Individual stripeVerificationFields `json:"individual"`
}
type stripeVerificationFields struct {
	Additional []stripeText `json:"additional"`
	Minimum    []stripeText `json:"minimum"`
}
type stripeList[T any] struct {
	Data    []T    `json:"data"`
	HasMore bool   `json:"has_more"`
	Object  string `json:"object"   jsonschema:"enum=list"`
	URL     string `json:"url"      jsonschema:"maxLength=5000,pattern=^/v1/country_specs"`
}

type (
	cloudflareSource struct {
		Pointer string `json:"pointer,omitempty"`
	}
	cloudflareMessage struct {
		Code             int              `json:"code"                        jsonschema:"minimum=1000"`
		Message          string           `json:"message"`
		DocumentationURL string           `json:"documentation_url,omitempty"`
		Source           cloudflareSource `json:"source,omitzero"`
	}
)

// The source spec attaches uniqueItems to the item object, rather than its
// containing array. Retain the keyword for exact structural comparison.
func (cloudflareMessage) JSONSchemaExtend(schema *jsonschema.Schema) { schema.UniqueItems = true }

type cloudflareCommon struct {
	Errors   []cloudflareMessage `json:"errors"`
	Messages []cloudflareMessage `json:"messages"`
	Success  bool                `json:"success"  jsonschema:"enum=true"`
}
type cloudflareToken struct {
	ExpiresOn string `json:"expires_on,omitempty" jsonschema:"format=date-time"`
	ID        string `json:"id"                   jsonschema:"maxLength=32,readonly"`
	NotBefore string `json:"not_before,omitempty" jsonschema:"format=date-time"`
	Status    string `json:"status"               jsonschema:"enum=active|disabled|expired"`
}
type cloudflareVerify struct {
	cloudflareCommon

	Result cloudflareToken `json:"result,omitzero"`
}
