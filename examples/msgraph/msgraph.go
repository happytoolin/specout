// Command msgraph serves eight operations of the published Microsoft Graph
// v1.0 OpenAPI description — the user resource plus /me — with specout types
// and gorilla/mux. Summaries, descriptions, operationIds, parameter names and
// response codes are the published ones; examples/README.md lists deviations.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/examplekit"
)

// ODataError is microsoft.graph.ODataErrors.ODataError: the 4XX/5XX envelope.
type ODataError struct {
	Error MainError `json:"error"`
}

type MainError struct {
	Code       string        `json:"code"`
	Message    string        `json:"message"`
	Target     string        `json:"target,omitempty"`
	Details    []ErrorDetail `json:"details,omitempty"`
	InnerError *InnerError   `json:"innerError,omitempty"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Target  string `json:"target,omitempty"`
}

// InnerError is microsoft.graph.ODataErrors.InnerError; two names carry hyphens.
type InnerError struct {
	RequestID       string `json:"request-id,omitempty"`
	ClientRequestID string `json:"client-request-id,omitempty"`
	Date            string `json:"date,omitempty" jsonschema:"format=date-time,description=Date when the error occured."`
}

// User is the common subset of microsoft.graph.user, which inherits directoryObject.
type User struct {
	ID                string `json:"id,omitempty" jsonschema:"description=The unique identifier for the user."`
	DisplayName       string `json:"displayName,omitempty" jsonschema:"description=The name displayed in the address book for the user."`
	Mail              string `json:"mail,omitempty" jsonschema:"description=The SMTP address for the user."`
	UserPrincipalName string `json:"userPrincipalName,omitempty" jsonschema:"description=The user principal name (UPN) of the user."`
	GivenName         string `json:"givenName,omitempty" jsonschema:"description=The given name (first name) of the user."`
	Surname           string `json:"surname,omitempty" jsonschema:"description=The user's surname (family name or last name)."`
	JobTitle          string `json:"jobTitle,omitempty" jsonschema:"description=The user's job title."`
	AccountEnabled    bool   `json:"accountEnabled,omitempty" jsonschema:"description=true if the account is enabled; otherwise, false."`
	CreatedDateTime   string `json:"createdDateTime,omitempty" jsonschema:"format=date-time,description=The date and time the user was created."`
}

// UserCollectionResponse is microsoft.graph.userCollectionResponse: the page
// links from BaseCollectionPaginationCountResponse, then value.
type UserCollectionResponse struct {
	ODataCount    int64  `json:"@odata.count,omitempty"`
	ODataNextLink string `json:"@odata.nextLink,omitempty"`
	Value         []User `json:"value"`
}

type Message struct {
	Subject      string      `json:"subject,omitempty"`
	Body         ItemBody    `json:"body,omitempty"`
	ToRecipients []Recipient `json:"toRecipients,omitempty"`
}

type ItemBody struct {
	Content     string `json:"content,omitempty" jsonschema:"description=The content of the item."`
	ContentType string `json:"contentType,omitempty" jsonschema:"description=The type of the content. Possible values are text and html.,enum=text|html"`
}

type Recipient struct {
	EmailAddress EmailAddress `json:"emailAddress,omitempty"`
}

type EmailAddress struct {
	Name    string `json:"name,omitempty" jsonschema:"description=The display name of the person or entity."`
	Address string `json:"address,omitempty" jsonschema:"description=The email address of the person or entity."`
}

type (
	// getMeReq is me.user.GetUser: ConsistencyLevel plus $select/$expand.
	getMeReq struct {
		ConsistencyLevel string   `header:"ConsistencyLevel,omitempty" jsonschema:"description=Indicates the requested consistency level."`
		Select           []string `query:"$select,omitempty" jsonschema:"description=Select properties to be returned"`
		Expand           []string `query:"$expand,omitempty" jsonschema:"description=Expand related entities"`
	}
	// listUsersReq is users.user.ListUser, in published order.
	listUsersReq struct {
		ConsistencyLevel string   `header:"ConsistencyLevel,omitempty" jsonschema:"description=Indicates the requested consistency level."`
		Top              int32    `query:"$top,omitempty" jsonschema:"description=Show only the first n items,minimum=0,example=50"`
		Search           string   `query:"$search,omitempty" jsonschema:"description=Search items by search phrases"`
		Filter           string   `query:"$filter,omitempty" jsonschema:"description=Filter items by property values"`
		Count            bool     `query:"$count,omitempty" jsonschema:"description=Include count of items"`
		OrderBy          []string `query:"$orderby,omitempty" jsonschema:"description=Order items by property values"`
		Select           []string `query:"$select,omitempty" jsonschema:"description=Select properties to be returned"`
		Expand           []string `query:"$expand,omitempty" jsonschema:"description=Expand related entities"`
	}
	// getUserReq is users.user.GetUser: no body, so user-id rides the Req type.
	getUserReq struct {
		UserID string   `path:"user-id" jsonschema:"description=The unique identifier of user"`
		Select []string `query:"$select,omitempty" jsonschema:"description=Select properties to be returned"`
		Expand []string `query:"$expand,omitempty" jsonschema:"description=Expand related entities"`
	}
	deleteUserReq struct {
		UserID  string `path:"user-id" jsonschema:"description=The unique identifier of user"`
		IfMatch string `header:"If-Match,omitempty" jsonschema:"description=ETag"`
	}
	// mediaReq is the profile-photo media operation: path parameter only.
	mediaReq struct {
		UserID string `path:"user-id" jsonschema:"description=The unique identifier of user"`
	}
	// sendMailReq is sendMail: the path parameter plus the body properties,
	// which specout splits out into their own component.
	sendMailReq struct {
		UserID          string  `path:"user-id" jsonschema:"description=The unique identifier of user"`
		Message         Message `json:"Message,omitempty" jsonschema:"description=The message to send."`
		SaveToSentItems bool    `json:"SaveToSentItems,omitempty" jsonschema:"description=Save the message in Sent Items."`
	}
)

// errs is the published failure pair: the 4XX/5XX ranges, described "error".
var errs = []specout.Response{
	{Key: "4XX", Type: ODataError{}, Raw: map[string]any{"description": "error"}},
	{Key: "5XX", Type: ODataError{}, Raw: map[string]any{"description": "error"}},
}

// withErrs joins one operation's own responses with the shared failure pair.
func withErrs(rs ...specout.Response) []specout.Response { return append(rs, errs...) }

// ok drops the Res-derived 200 body and re-keys the success under 2XX.
func ok(description string) []specout.Response {
	return withErrs(
		specout.Response{Status: 200, Omit: true},
		specout.Response{Status: 200, Key: "2XX", Raw: map[string]any{"description": description}},
	)
}

// noContent is the published 204 — the concrete code Graph keeps, with no body.
func noContent() []specout.Response {
	return withErrs(specout.Response{Status: 204, Type: struct{}{}, Raw: map[string]any{"description": "Success"}})
}

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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

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

// op assembles one operation from its published metadata, the
// learn.microsoft.com page slug ("" when the document has none) and the body.
func op[Req, Res any](summary, description, id, tag, docs string, responses []specout.Response, h http.HandlerFunc) specout.Handler[Req, Res] {
	handler := specout.Handler[Req, Res]{HandlerFunc: h, Summary: summary, Description: description, OperationID: id, Tags: []string{tag}, Responses: responses}
	if docs != "" {
		handler.ExternalDocs = &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/" + docs + "?view=graph-rest-1.0",
		}
	}
	return handler
}

func (s *store) getMe(w http.ResponseWriter, r *http.Request) {
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
	out := UserCollectionResponse{Value: []User{}}
	for _, u := range s.users {
		out.Value = append(out.Value, u)
	}
	s.mu.Unlock()
	sort.Slice(out.Value, func(i, j int) bool { return out.Value[i].ID < out.Value[j].ID })
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
		w.WriteHeader(204)
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
	w.WriteHeader(200)
	w.Write(photo)
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
		w.WriteHeader(204)
	}
}

// graphDescription is info.description of the published document.
const graphDescription = "This OData service is located at https://graph.microsoft.com/v1.0"

// New builds the generator and the router. The published document declares no
// securitySchemes, though Graph takes a bearer token.
func New() (*specout.Generator, *mux.Router) {
	d := specout.New(specout.Config{
		Title:       "OData Service for namespace microsoft.graph",
		Version:     "v1.0",
		Description: graphDescription,
		Servers:     []specout.Server{{URL: "https://graph.microsoft.com/v1.0"}},
		Tags:        []specout.Tag{{Name: "me.user"}, {Name: "users.user"}, {Name: "users.user.Actions"}, {Name: "users.profilePhoto"}},
	})

	s := newStore()
	r := mux.NewRouter()
	b := specout.Gorilla(d, r)
	b.Get("/me", op[getMeReq, User]("Get a user", "Retrieve the properties and relationships of user object.",
		"me.user.GetUser", "me.user", "user-get", ok("Retrieved entity"), s.getMe))
	b.Get("/users", op[listUsersReq, UserCollectionResponse]("List users", "Retrieve a list of user objects.",
		"users.user.ListUser", "users.user", "user-list", ok("Retrieved collection"), s.listUsers))
	b.Post("/users", op[User, User]("Create User", "Create a new user.",
		"users.user.CreateUser", "users.user", "user-post-users", ok("Created entity"), s.createUser))
	b.Get("/users/{user-id}", op[getUserReq, User]("Get a user", "Retrieve the properties and relationships of user object.",
		"users.user.GetUser", "users.user", "user-get", ok("Retrieved entity"), s.getUser))
	b.Patch("/users/{user-id}", op[User, User]("Update user", "Update the properties of a user object.",
		"users.user.UpdateUser", "users.user", "user-update", ok("Success"), s.patchUser))
	b.Delete("/users/{user-id}", op[deleteUserReq, specout.NoContent]("Delete a user", "Delete a user object.",
		"users.user.DeleteUser", "users.user", "user-delete", noContent(), s.deleteUser))
	b.Get("/users/{user-id}/photo/$value", op[mediaReq, struct{}]("Get media content for the navigation property photo from users",
		"The user's profile photo. Read-only.", "users.GetPhotoContent", "users.profilePhoto", "",
		withErrs(specout.Response{Status: 200, Key: "2XX", ContentType: "application/octet-stream", Raw: map[string]any{"description": "Retrieved media content"}}), s.getPhoto))
	b.Post("/users/{user-id}/sendMail", op[sendMailReq, specout.NoContent]("Invoke action sendMail",
		"Send the message specified in the request body using either JSON or MIME format.", "users.user.sendMail", "users.user.Actions", "user-sendmail", noContent(), s.sendMail))
	if err := b.Adopt(); err != nil {
		panic(err)
	}
	return d, r
}

// Handler serves the demo: Swagger UI at / and the spec at /openapi.json. The
// published server URL is absolute, so "try it out" aims at real Graph.
func Handler(d *specout.Generator, r http.Handler) http.Handler {
	std := http.NewServeMux()
	std.Handle("/openapi.json", d)
	std.Handle("/", examplekit.Page(examplekit.SwaggerPage("specout example — Microsoft Graph subset", "/openapi.json"), r))
	return std
}

func main() {
	d, r := New()
	if examplekit.EmitSpec(d) {
		return
	}
	fmt.Println("msgraph: http://localhost:8082/   spec: http://localhost:8082/openapi.json")
	if err := http.ListenAndServe(":8082", Handler(d, r)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
