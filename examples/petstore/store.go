package main

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/happytoolin/specout/internal/examplekit"
)

type store struct {
	mu     sync.Mutex
	seq    int64
	pets   map[int64]Pet
	orders map[int64]Order
	users  map[string]User
}

func newStore() *store {
	return &store{pets: map[int64]Pet{}, orders: map[int64]Order{}, users: map[string]User{}}
}

func (s *store) nextID() int64 { s.seq++; return s.seq }

// petsList snapshots the pet map, so the filter operations iterate a stable
// slice without holding the lock.
func (s *store) petsList() []Pet {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Collect(maps.Values(s.pets))
}

// get reads m[k] and take removes it, both under the store lock; the bool says
// whether the key was there. A handler keeps its own lock only when it mutates
// a value in place.
func get[K comparable, V any](s *store, m map[K]V, k K) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := m[k]
	return v, ok
}

func take[K comparable, V any](s *store, m map[K]V, k K) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := m[k]
	delete(m, k)
	return ok
}

// writeJSON is the shared JSON writer under the short name the handlers use.
func writeJSON(w http.ResponseWriter, code int, v any) { examplekit.WriteJSON(w, code, v) }

func notFound(w http.ResponseWriter, msg string) { writeJSON(w, 404, map[string]any{"message": msg}) }

// withBody decodes the JSON request body into T, or answers the published 400
// with msg; act runs only on a decoded body.
func withBody[T any](w http.ResponseWriter, r *http.Request, msg string, act func(T)) {
	var v T
	if json.NewDecoder(r.Body).Decode(&v) != nil {
		writeJSON(w, 400, map[string]any{"message": msg})
		return
	}
	act(v)
}

// storeBody decodes the request body, lets key assign the identity the store
// keys on, and answers the stored value; msg is the published 400.
func storeBody[K comparable, V any](w http.ResponseWriter, r *http.Request, s *store, msg string, m map[K]V, key func(*V) K) {
	withBody(w, r, msg, func(v V) {
		s.mu.Lock()
		m[key(&v)] = v
		s.mu.Unlock()
		writeJSON(w, 200, v)
	})
}

// deleteOr404 removes m[k] and answers the published 200, or msg as 404.
func deleteOr404[K comparable, V any](w http.ResponseWriter, s *store, m map[K]V, k K, msg string) {
	if !take(s, m, k) {
		notFound(w, msg)
		return
	}
	w.WriteHeader(200)
}

// pathID is the int64 value of one URL placeholder. A value that is not a
// number reads as 0, which no route stores under: the published 404 answer.
func pathID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id
}

// show answers the stored value as JSON 200, or msg as the published 404 when
// the key is absent.
func show[K comparable, V any](w http.ResponseWriter, s *store, m map[K]V, k K, msg string) {
	if v, ok := get(s, m, k); ok {
		writeJSON(w, 200, v)
		return
	}
	notFound(w, msg)
}
