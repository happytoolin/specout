package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/happytoolin/specout/internal/examplekit"
)

type store struct {
	mu     sync.Mutex
	users  map[string]User
	photos map[string][]byte
	sent   []Message
}

func newStore() *store {
	alice := User{ID: "48d31887-5fad-4d73-a9f5-3c356e68a038", DisplayName: "Alice", Mail: "alice@contoso.com", UserPrincipalName: "alice@contoso.com", GivenName: "Alice", Surname: "Wong", JobTitle: "Engineer", AccountEnabled: true, CreatedDateTime: "2024-01-01T00:00:00Z"}
	bob := User{ID: "87d349ed-44d7-43e1-9a83-5f2406dee5bd", DisplayName: "Bob", Mail: "bob@contoso.com", UserPrincipalName: "bob@contoso.com", GivenName: "Bob", Surname: "Brown", JobTitle: "Designer", AccountEnabled: true, CreatedDateTime: "2024-02-02T00:00:00Z"}
	return &store{
		users:  map[string]User{alice.ID: alice, bob.ID: bob},
		photos: map[string][]byte{alice.ID: []byte("\x89PNG fake profile photo")},
	}
}

// user resolves user-id, answering the published 404 when the user is absent.
func (s *store) user(w http.ResponseWriter, r *http.Request) (User, bool) {
	s.mu.Lock()
	u, found := s.users[pathUserID(r)]
	s.mu.Unlock()
	if !found {
		notFound(w, "users")
	}
	return u, found
}

// writeJSON is the shared JSON writer under the short name the handlers use.
func writeJSON(w http.ResponseWriter, code int, v any) { examplekit.WriteJSON(w, code, v) }

// writeError is the published error envelope, with the handler's status code.
func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, ODataError{Error: MainError{
		Code:    strings.ToLower(http.StatusText(code)),
		Message: message,
	}})
}

// notFound is the published 404; segment names the missing resource.
func notFound(w http.ResponseWriter, segment string) {
	writeError(w, 404, "Resource not found for the segment '"+segment+"'.")
}

// decode reads the JSON body, answering the published 400 when it is malformed.
func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	err := json.NewDecoder(r.Body).Decode(&v)
	if err != nil {
		writeError(w, 400, "The request body is malformed.")
	}
	return v, err == nil
}

func pathUserID(r *http.Request) string { return mux.Vars(r)["user-id"] }

func (s *store) getMe(w http.ResponseWriter, _ *http.Request) {
	// no token here, so any user answers /me: Graph resolves one for the caller
	s.mu.Lock()
	var u User
	for _, u = range s.users {
		break
	}
	s.mu.Unlock()
	writeJSON(w, 200, u)
}

func (s *store) listUsers(w http.ResponseWriter, r *http.Request) {
	top, _ := strconv.Atoi(r.URL.Query().Get("$top"))
	s.mu.Lock()
	value := slices.SortedFunc(maps.Values(s.users), func(a, b User) int { return strings.Compare(a.ID, b.ID) })
	s.mu.Unlock()
	// keep Value non-nil so an empty store still answers "value": []
	out := UserCollectionResponse{Value: append([]User{}, value...)}
	if top > 0 && top < len(out.Value) {
		out.ODataNextLink = "/users?$top=" + strconv.Itoa(top) + "&$skiptoken=" + out.Value[top-1].ID
		out.Value = out.Value[:top]
	}
	out.ODataCount = int64(len(out.Value))
	writeJSON(w, 200, out)
}

func (s *store) createUser(w http.ResponseWriter, r *http.Request) {
	u, valid := decode[User](w, r)
	if !valid {
		return
	}
	if u.ID == "" {
		u.ID = fmt.Sprintf("user-%d", time.Now().UnixNano())
	}
	if u.CreatedDateTime == "" {
		u.CreatedDateTime = time.Now().UTC().Format(time.RFC3339)
	}
	s.mu.Lock()
	s.users[u.ID] = u
	s.mu.Unlock()
	writeJSON(w, 200, u)
}

func (s *store) getUser(w http.ResponseWriter, r *http.Request) {
	if u, found := s.user(w, r); found {
		writeJSON(w, 200, u)
	}
}

// patchUser's body is the user object itself: the published operation $refs
// microsoft.graph.user, and so this Req has no path field.
func (s *store) patchUser(w http.ResponseWriter, r *http.Request) {
	patch, valid := decode[User](w, r)
	if !valid {
		return
	}
	u, found := s.user(w, r)
	if !found {
		return
	}
	s.mu.Lock()
	if patch.DisplayName != "" {
		u.DisplayName = patch.DisplayName
	}
	if patch.JobTitle != "" {
		u.JobTitle = patch.JobTitle
	}
	if patch.Mail != "" {
		u.Mail = patch.Mail
	}
	s.users[u.ID] = u
	s.mu.Unlock()
	writeJSON(w, 200, u)
}

func (s *store) deleteUser(w http.ResponseWriter, r *http.Request) {
	if u, found := s.user(w, r); found {
		s.mu.Lock()
		delete(s.users, u.ID)
		delete(s.photos, u.ID)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}
}

// getPhoto is the media download: its 2XX body is application/octet-stream,
// carried by Response.ContentType, and Res declares no body of its own.
func (s *store) getPhoto(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	photo, found := s.photos[pathUserID(r)]
	s.mu.Unlock()
	if !found {
		notFound(w, "photo")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(photo)
}

func (s *store) sendMail(w http.ResponseWriter, r *http.Request) {
	req, valid := decode[sendMailReq](w, r)
	if !valid {
		return
	}
	if _, found := s.user(w, r); found {
		s.mu.Lock()
		s.sent = append(s.sent, req.Message)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}
}
