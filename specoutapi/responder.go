package specoutapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Config wires the error mapper; no package globals.
type Config struct {
	ErrorMapper func(error) (int, Problem)
}

// Responder writes JSON responses. The mapper lives here, wired through the
// app's dependencies — never a package global.
type Responder struct {
	cfg Config
}

func New(cfg Config) *Responder { return &Responder{cfg: cfg} }

func (a *Responder) OK[T any](w http.ResponseWriter, v T) {
	a.Status(w, http.StatusOK, v)
}

func (a *Responder) Status[T any](w http.ResponseWriter, code int, v T) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (a *Responder) NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Err writes the error: DetailedError first, mapper + Problem second.
func (a *Responder) Err(w http.ResponseWriter, r *http.Request, err error) {
	var de DetailedError
	if errors.As(err, &de) {
		a.Status(w, de.HTTPStatus(), de.Payload())
		return
	}
	code := http.StatusInternalServerError
	var p Problem
	if a.cfg.ErrorMapper != nil {
		code, p = a.cfg.ErrorMapper(err)
	}
	p.Status = code
	if p.Title == "" {
		p.Title = http.StatusText(code)
	}
	a.Status(w, code, p)
}
