// Package validators holds the validation-showcase domain: every JSON
// Schema constraint keyword, expressed through struct tags, in one place.
package validators

import "time"

// CreateThingRequest exercises every string/number/array/map keyword.
type CreateThingRequest struct {
	// strings
	Slug        string `json:"slug"           jsonschema:"pattern=^[a-z][a-z0-9-]*$,minLength=3,maxLength=40,example=acme-thing"`
	DisplayName string `json:"displayName"    jsonschema:"minLength=1,maxLength=80"`
	Email       string `json:"email"          jsonschema:"format=email,example=ops@example.com"`
	Site        string `json:"site,omitempty" jsonschema:"format=uri"`
	// numbers
	Priority int     `json:"priority"       jsonschema:"minimum=1,maximum=5,default=3"`
	Weight   float64 `json:"weight,omitempty" jsonschema:"minimum=0,maximum=1000,multipleOf=0.5"`
	Stock    int64   `json:"stock"          jsonschema:"minimum=0,maximum=1000000"`
	// arrays
	Tags   []string `json:"tags"           jsonschema:"minItems=1,maxItems=10,uniqueItems=true,description=Free-form labels"`
	Scores []int    `json:"scores,omitempty" jsonschema:"minItems=1,maxItems=5,description=Judges scores 0-10"`
	// enum + default
	Visibility string `json:"visibility"     jsonschema:"enum=public|internal|private,default=public"`
	// nullable optional time
	ExpiresAt *time.Time `json:"expiresAt,omitempty" jsonschema:"description=Null or omit for never"`
	// maps with typed values
	Metadata map[string]string `json:"metadata,omitempty" jsonschema:"description=Arbitrary string labels"`
	Limits   map[string]int    `json:"limits,omitempty"   jsonschema:"description=Per-region limits"`
}

// Thing is the response shape: readOnly id and timestamps.
type Thing struct {
	ID         string            `json:"id"          jsonschema:"readonly,example=thg_01HX"`
	CreatedAt  time.Time         `json:"createdAt"   jsonschema:"readonly"`
	Slug       string            `json:"slug"        jsonschema:"pattern=^[a-z][a-z0-9-]*$"`
	Email      string            `json:"email"       jsonschema:"format=email"`
	Priority   int               `json:"priority"    jsonschema:"minimum=1,maximum=5"`
	Tags       []string          `json:"tags"        jsonschema:"minItems=1,maxItems=10,uniqueItems=true"`
	Visibility string            `json:"visibility"  jsonschema:"enum=public|internal|private"`
	ExpiresAt  *time.Time        `json:"expiresAt,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// SearchThingsRequest shows query-param validation: enum, bounds, defaults.
type SearchThingsRequest struct {
	Q           string `query:"q"           jsonschema:"minLength=1,maxLength=100,description=Full-text query"`
	Visibility  string `query:"visibility"  jsonschema:"enum=public|internal|private,default=public"`
	MinPriority int    `query:"minPriority" jsonschema:"minimum=1,maximum=5,default=1"`
	Limit       int    `query:"limit"       jsonschema:"minimum=1,maximum=50,default=20"`
	Cursor      string `query:"cursor"      jsonschema:"description=Opaque cursor"`
}

// EmailChannel is the email variant, registered as "email".
type EmailChannel struct {
	Address string `json:"address" jsonschema:"format=email"`
}

// SlackChannel is the slack variant, registered as "slack".
type SlackChannel struct {
	Webhook string `json:"webhook" jsonschema:"format=uri"`
	Channel string `json:"channel"`
}

// ChannelConfig is a oneOf + discriminator demo via Register.
type ChannelConfig struct {
	Kind string `json:"kind" jsonschema:"enum=notify_email|notify_slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=notify_email|notify_slack"`
}

// ThingList is the search response wrapper.
type ThingList struct {
	Items []Thing `json:"items"`
}
