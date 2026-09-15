// Package webhooks declares the union: two variants plus the envelope.
// Kind is a plain string with an enum tag — no marker type.
package webhooks

// EmailConfig is the email variant, registered as "email".
type EmailConfig struct {
	Address string `json:"address" jsonschema:"format=email"`
}

// SlackConfig is the slack variant, registered as "slack".
type SlackConfig struct {
	Channel string   `json:"channel"`
	Events  []string `json:"events,omitempty"`
}

// Config is the envelope: Kind discriminates, Data is the variant payload.
type Config struct {
	Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
}
