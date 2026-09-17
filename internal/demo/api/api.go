// Package api is the app's own helper layer — plain std, no specout dependency.
package api

import (
	"cmp"
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

type ValidationError struct {
	Problems []FieldProblem `json:"problems"`
}

type FieldProblem struct {
	Field   string `json:"field"   jsonschema:"example=owner"`
	Message string `json:"message" jsonschema:"example=must be a valid email"`
}

func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func Invalid(w http.ResponseWriter, field, message string) {
	JSON(w, http.StatusUnprocessableEntity, ValidationError{[]FieldProblem{{Field: field, Message: message}}})
}

// wireError carries its own status and payload, bypassing the mapper.
type wireError interface {
	error
	HTTPStatus() int
	Payload() any
}

type Mapper func(error) (int, Problem)

// DefaultMapper maps store errors to Problems; apps wire their own.
var DefaultMapper Mapper = func(err error) (int, Problem) {
	if errors.Is(err, onboarding.ErrNotFound) {
		return http.StatusNotFound, Problem{Type: "not-found", Title: "Not found"}
	}
	return http.StatusInternalServerError, Problem{Type: "internal", Title: "Internal error"}
}

// Error writes the mapped Problem; a wireError bypasses the mapper.
func Error(w http.ResponseWriter, mapper Mapper, err error) {
	if we, ok := err.(wireError); ok {
		JSON(w, we.HTTPStatus(), we.Payload())
		return
	}
	code, p := mapper(err)
	p.Status, p.Title = code, cmp.Or(p.Title, http.StatusText(code))
	JSON(w, code, p)
}
