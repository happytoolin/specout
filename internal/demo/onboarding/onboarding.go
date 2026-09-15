// Package onboarding holds the onboarding domain: types and the store.
package onboarding

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

// Onboarding serves both wire directions: readOnly fields are stripped from
// requests, writeOnly from responses.
type Onboarding struct {
	ID        string     `json:"id"        jsonschema:"readonly,example=onb_4f9x"`
	CreatedAt time.Time  `json:"createdAt" jsonschema:"readonly"`
	Owner     string     `json:"owner"     jsonschema:"format=email"`
	APIKey    string     `json:"apiKey,omitempty" jsonschema:"writeonly"`
	Stage     string     `json:"stage"     jsonschema:"enum=draft|active|archived,default=draft"`
	Note      string     `json:"note,omitempty" jsonschema:"maxLength=500"`
	Expiry    *time.Time `json:"expiry,omitempty" jsonschema:"description=Optional expiry"`
}

type UpsertRequest struct {
	Owner string `json:"owner" jsonschema:"format=email"`
	Stage string `json:"stage" jsonschema:"enum=draft|active|archived,default=draft"`
}

type ListRequest struct {
	Session string `cookie:"session"    jsonschema:"description=Session cookie from login"`
	Limit   int    `query:"limit"      jsonschema:"default=20,minimum=1,maximum=100"`
	Cursor  string `query:"cursor"     jsonschema:"description=Opaque cursor from a previous page"`
	Sort    string `query:"sort"       jsonschema:"enum=created|updated,default=created"`
	Trace   string `header:"X-Trace-Id" jsonschema:"description=Client trace id for debugging"`
}

type Page struct {
	Items []Onboarding `json:"items"`
	Next  string       `json:"next,omitempty" jsonschema:"description=Cursor for the next page, empty on last"`
}

// Empty marks a request with no body.
type Empty struct{}

// SyncConflict is the rich 409 body for the sync endpoint.
type SyncConflict struct {
	Resource   string `json:"resource"   jsonschema:"example=onboarding"`
	Expected   int    `json:"expected"   jsonschema:"description=Version the client sent"`
	Actual     int    `json:"actual"     jsonschema:"description=Version currently on the server"`
	ResolveURL string `json:"resolveUrl" jsonschema:"description=Fetch the current state here"`
}

type SyncRequest struct {
	Expected int `json:"expected" jsonschema:"description=Client's last seen version"`
}

type SyncResult struct {
	Count int `json:"count"`
}

var ErrNotFound = errors.New("onboarding: not found")

// ConflictError is a DetailedError: it knows its own status and wire shape,
// bypassing the generic mapper (api-reference §05).
type ConflictError struct {
	ID       string
	Expected int
	Actual   int
}

func (c *ConflictError) Error() string   { return "onboarding: version conflict on " + c.ID }
func (c *ConflictError) HTTPStatus() int { return http.StatusConflict }
func (c *ConflictError) Payload() any {
	return SyncConflict{Resource: "onboarding", Expected: c.Expected, Actual: c.Actual, ResolveURL: "/onboarding/" + c.ID}
}

// Store is a toy in-memory store; the point is typed errors, not SQL.
type Store struct {
	mu   sync.Mutex
	rows map[string]Onboarding
}

func NewStore() *Store {
	return &Store{rows: map[string]Onboarding{
		"onb_4f9x": {
			ID: "onb_4f9x", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Owner: "owner@example.com", Stage: "draft",
		},
	}}
}

func (s *Store) Get(id string) (Onboarding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ob, ok := s.rows[id]
	if !ok {
		return ob, ErrNotFound
	}
	return ob, nil
}

func (s *Store) List() []Onboarding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Onboarding, 0, len(s.rows))
	for _, ob := range s.rows {
		out = append(out, ob)
	}
	return out
}

// Upsert creates when the id is unknown (created=true) and updates otherwise.
func (s *Store) Upsert(id string, req UpsertRequest) (Onboarding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.rows[id]
	if !ok {
		ob := Onboarding{ID: id, CreatedAt: time.Now(), Owner: req.Owner, Stage: req.Stage, APIKey: "sk_demo"}
		s.rows[id] = ob
		return ob, true, nil
	}
	existing.Owner = req.Owner
	existing.Stage = req.Stage
	s.rows[id] = existing
	return existing, false, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return ErrNotFound
	}
	delete(s.rows, id)
	return nil
}

// Sync returns *ConflictError on version mismatch — the DetailedError path.
func (s *Store) Sync(id string, expected int) (SyncResult, error) {
	ob, err := s.Get(id)
	if err != nil {
		return SyncResult{}, err
	}
	actual := int(ob.CreatedAt.Unix() % 10)
	if expected != actual {
		return SyncResult{}, &ConflictError{ID: id, Expected: expected, Actual: actual}
	}
	return SyncResult{Count: 1}, nil
}
