// Package webhooks declares the union: two variants plus the envelope with
// the Variant discriminator and a oneof_type field.
package webhooks

import "github.com/happytoolin/specout"

// EmailConfig is the email variant, registered as "email".
type EmailConfig struct {
	Address string `json:"address" jsonschema:"format=email"`
}

// SlackConfig is the slack variant, registered as "slack".
type SlackConfig struct {
	Channel string   `json:"channel"`
	Events  []string `json:"events,omitempty" jsonschema:"example=pinged,example=completed"`
}

// Config is the envelope: Kind discriminates, Data is the variant payload.
type Config struct {
	Kind specout.Variant `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any             `json:"data" jsonschema:"oneof_type=email|slack"`
}
