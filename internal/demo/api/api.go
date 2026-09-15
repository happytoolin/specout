// Package api is the app's own helper layer — plain std, no specout
// dependency. The spec never reads this; handlers may use it or not.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/happytoolin/specout/internal/demo/onboarding"
)

// Problem is the project-wide error envelope (RFC 9457 style).
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ValidationError is the 422 body: field-level problems.
type ValidationError struct {
	Problems []FieldProblem `json:"problems"`
}

type FieldProblem struct {
	Field   string `json:"field"   jsonschema:"example=owner"`
	Message string `json:"message" jsonschema:"example=must be a valid email"`
}

// JSON writes code + v as JSON. Handlers call this directly.
func JSON[T any](w http.ResponseWriter, code int, v T) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// Error writes the mapped Problem. mapper decides code + shape; the domain's
// *ConflictError (a wireError) bypasses it with its own status and payload.
type wireError interface {
	error
	HTTPStatus() int
	Payload() any
}

type Mapper func(error) (int, Problem)

// DefaultMapper maps store errors to Problems; apps wire their own.
var DefaultMapper Mapper = func(err error) (int, Problem) {
	switch {
	case errors.Is(err, onboarding.ErrNotFound):
		return http.StatusNotFound, Problem{Type: "not-found", Title: "Not found"}
	default:
		return http.StatusInternalServerError, Problem{Type: "internal", Title: "Internal error"}
	}
}

func Error(w http.ResponseWriter, mapper Mapper, err error) {
	if we, ok := err.(wireError); ok {
		JSON(w, we.HTTPStatus(), we.Payload())
		return
	}
	code, p := mapper(err)
	p.Status = code
	if p.Title == "" {
		p.Title = http.StatusText(code)
	}
	JSON(w, code, p)
}
