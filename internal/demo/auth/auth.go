// Package auth enforces the schemes specout only declares.
package auth

import (
	"net/http"
	"strings"
)

// Require accepts any declared scheme; all demo tokens and keys pass.
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, cookieErr := r.Cookie("session")
		ok := cookieErr == nil || strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("X-API-Key") != ""
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "missing credentials", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
