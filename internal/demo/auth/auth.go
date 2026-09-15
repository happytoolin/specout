// Package auth is the demo's own middleware. specout only declares the
// securitySchemes; enforcement is app code.
package auth

import (
	"net/http"
	"strings"
)

// Require accepts any of the three declared schemes: Bearer token,
// X-API-Key header, or session cookie. All demo tokens/keys are accepted.
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		has := func() bool {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				return true
			}
			if r.Header.Get("X-API-Key") != "" {
				return true
			}
			if _, err := r.Cookie("session"); err == nil {
				return true
			}
			return false
		}()
		if !has {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "missing credentials", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
