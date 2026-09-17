// Package validators showcases every JSON Schema constraint keyword as struct tags.
package validators

import "time"

type CreateThingRequest struct {
	Slug        string            `json:"slug"                jsonschema:"pattern=^[a-z][a-z0-9-]*$,minLength=3,maxLength=40,example=acme-thing"`
	DisplayName string            `json:"displayName"         jsonschema:"minLength=1,maxLength=80"`
	Email       string            `json:"email"               jsonschema:"format=email,example=ops@example.com"`
	Site        string            `json:"site,omitempty"      jsonschema:"format=uri"`
	Priority    int               `json:"priority"            jsonschema:"minimum=1,maximum=5,default=3"`
	Weight      float64           `json:"weight,omitzero"     jsonschema:"minimum=0,maximum=1000,multipleOf=0.5"`
	Stock       int64             `json:"stock"               jsonschema:"minimum=0,maximum=1000000"`
	Tags        []string          `json:"tags"                jsonschema:"minItems=1,maxItems=10,uniqueItems=true,description=Free-form labels"`
	Scores      []int             `json:"scores,omitempty"    jsonschema:"minItems=1,maxItems=5,description=Judges scores 0-10"`
	Visibility  string            `json:"visibility"          jsonschema:"enum=public|internal|private,default=public"`
	ExpiresAt   *time.Time        `json:"expiresAt,omitempty" jsonschema:"description=Null or omit for never"`
	Metadata    map[string]string `json:"metadata,omitempty"  jsonschema:"description=Arbitrary string labels"`
	Limits      map[string]int    `json:"limits,omitempty"    jsonschema:"description=Per-region limits"`
}

type Thing struct {
	ID         string            `json:"id"                  jsonschema:"readonly,example=thg_01HX"`
	CreatedAt  time.Time         `json:"createdAt"           jsonschema:"readonly"`
	Slug       string            `json:"slug"                jsonschema:"pattern=^[a-z][a-z0-9-]*$"`
	Email      string            `json:"email"               jsonschema:"format=email"`
	Priority   int               `json:"priority"            jsonschema:"minimum=1,maximum=5"`
	Tags       []string          `json:"tags"                jsonschema:"minItems=1,maxItems=10,uniqueItems=true"`
	Visibility string            `json:"visibility"          jsonschema:"enum=public|internal|private"`
	ExpiresAt  *time.Time        `json:"expiresAt,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

func (r CreateThingRequest) Thing(id string, at time.Time) Thing {
	return Thing{
		ID: id, CreatedAt: at, Slug: r.Slug, Email: r.Email, Priority: r.Priority,
		Tags: r.Tags, Visibility: r.Visibility, ExpiresAt: r.ExpiresAt, Metadata: r.Metadata,
	}
}

type SearchThingsRequest struct {
	Q           string `jsonschema:"minLength=1,maxLength=100,description=Full-text query" query:"q"`
	Visibility  string `jsonschema:"enum=public|internal|private,default=public"           query:"visibility"`
	MinPriority int    `jsonschema:"minimum=1,maximum=5,default=1"                         query:"minPriority"`
	Limit       int    `jsonschema:"minimum=1,maximum=50,default=20"                       query:"limit"`
	Cursor      string `jsonschema:"description=Opaque cursor"                             query:"cursor"`
}

// EmailChannel and SlackChannel are the notify_email/notify_slack union variants.
type EmailChannel struct {
	Address string `json:"address" jsonschema:"format=email"`
}

type SlackChannel struct {
	Webhook string `json:"webhook" jsonschema:"format=uri"`
	Channel string `json:"channel"`
}

// ChannelConfig is a oneOf + discriminator demo via Register.
type ChannelConfig struct {
	Kind string `json:"kind" jsonschema:"enum=notify_email|notify_slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=notify_email|notify_slack"`
}

type ThingList struct {
	Items []Thing `json:"items"`
}

// EmailConfig, SlackConfig and Config are the webhook union: Kind discriminates,
// Data is the variant payload. Registered as "email" and "slack".
type EmailConfig struct {
	Address string `json:"address" jsonschema:"format=email"`
}

type SlackConfig struct {
	Channel string   `json:"channel"`
	Events  []string `json:"events,omitempty"`
}

type Config struct {
	Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
}
