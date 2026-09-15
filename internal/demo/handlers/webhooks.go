package handlers

import (
	"net/http"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/webhooks"
	"github.com/happytoolin/specout/specoutapi"
)

// HandleRegisterWebhook shows unions: oneOf + discriminator, variants
// registered at setup via d.Register[T](name).
func HandleRegisterWebhook(d Deps) specout.Handler[webhooks.Config, webhooks.Config] {
	return specout.Handler[webhooks.Config, webhooks.Config]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			cfg, err := specoutapi.Decode[webhooks.Config](r)
			if err != nil {
				d.API.Err(w, r, err)
				return
			}
			d.API.OK(w, cfg)
		},
		Summary: "Register a webhook (email or slack)",
		Tags:    []string{"webhooks"},
	}
}
