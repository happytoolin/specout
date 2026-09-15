package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/api"
	"github.com/happytoolin/specout/internal/demo/webhooks"
)

// HandleRegisterWebhook shows unions: oneOf + discriminator, variants
// registered at setup via d.Register[T](name).
func HandleRegisterWebhook(d Deps) specout.Handler[webhooks.Config, webhooks.Config] {
	return specout.Handler[webhooks.Config, webhooks.Config]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var cfg webhooks.Config
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				writeErr(w, d, err)
				return
			}
			api.JSON(w, http.StatusOK, cfg)
		},
		Summary: "Register a webhook (email or slack)",
		Tags:    []string{"webhooks"},
	}
}
