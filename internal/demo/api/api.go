// Package api is the application's own api layer: error shapes and the
// error mapper, wired once in dependencies. The specoutapi.Responder lives
// on Dependencies so tests can build multiple apps with different mappers.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/happytoolin/specout/internal/demo/onboarding"
	"github.com/happytoolin/specout/specoutapi"
)

// Problem is the project-wide error envelope. It IS specoutapi.Problem:
// the mapper's signature requires that exact type (the responder writes it).
type Problem = specoutapi.Problem

// ValidationError is the 422 body: field-level problems.
type ValidationError struct {
	Problems []FieldProblem `json:"problems"`
}

type FieldProblem struct {
	Field   string `json:"field"   jsonschema:"example=owner"`
	Message string `json:"message" jsonschema:"example=must be a valid email"`
}

// New builds the responder with the default error mapper. Routes that need
// their own shape use DetailedError and bypass this mapper entirely.
func New() *specoutapi.Responder {
	return specoutapi.New(specoutapi.Config{
		ErrorMapper: func(err error) (int, Problem) {
			switch {
			case errors.Is(err, onboarding.ErrNotFound):
				return http.StatusNotFound, Problem{Type: "not-found", Title: "Not found"}
			case isDecodeError(err):
				return http.StatusBadRequest, Problem{Type: "validation", Title: "Invalid request"}
			default:
				return http.StatusInternalServerError, Problem{Type: "internal", Title: "Internal error"}
			}
		},
	})
}

func isDecodeError(err error) bool {
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	return errors.As(err, &se) || errors.As(err, &te)
}
