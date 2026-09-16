// Command msgraph rebuilds a slice of the published Microsoft Graph v1.0
// OpenAPI description (OData, 11546 paths) with specout types, and serves it
// with gorilla/mux. The slice is the user resource plus /me: eight of the
// document's operations, with the published paths, operationIds, parameter
// names, response codes and response descriptions. examples/README.md lists
// what the document has and specout cannot say.
//
// Descriptions are the published ones, shortened to their first sentence
// where the original is a paragraph: the demonstration is the shape, not the
// prose.
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
)

// ---- components.schemas ----

// ODataError is microsoft.graph.ODataErrors.ODataError: the envelope every
// 4XX and 5XX in the published document carries.
type ODataError struct {
	Error MainError `json:"error"`
}

// MainError is microsoft.graph.ODataErrors.MainError.
type MainError struct {
	Code       string        `json:"code"`
	Message    string        `json:"message"`
	Target     string        `json:"target,omitempty"`
	Details    []ErrorDetail `json:"details,omitempty"`
	InnerError *InnerError   `json:"innerError,omitempty"`
}

// ErrorDetail is microsoft.graph.ODataErrors.ErrorDetails.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Target  string `json:"target,omitempty"`
}

// InnerError is microsoft.graph.ODataErrors.InnerError; two of its published
// property names carry hyphens.
type InnerError struct {
	RequestID       string `json:"request-id,omitempty"`
	ClientRequestID string `json:"client-request-id,omitempty"`
	Date            string `json:"date,omitempty" jsonschema:"format=date-time,description=Date when the error occured."`
}

// User is microsoft.graph.user: the published schema inherits from
// directoryObject through allOf and carries about 400 properties. This is the
// common subset, with the published types and descriptions.
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

// UserCollectionResponse is microsoft.graph.userCollectionResponse: the
// pagination pair from BaseCollectionPaginationCountResponse, then value.
type UserCollectionResponse struct {
	ODataCount    int64  `json:"@odata.count,omitempty"`
	ODataNextLink string `json:"@odata.nextLink,omitempty"`
	Value         []User `json:"value"`
}

// Message is microsoft.graph.message, common subset.
type Message struct {
	Subject      string      `json:"subject,omitempty"`
	Body         ItemBody    `json:"body,omitempty"`
	ToRecipients []Recipient `json:"toRecipients,omitempty"`
}

// ItemBody is microsoft.graph.itemBody.
type ItemBody struct {
	Content     string `json:"content,omitempty" jsonschema:"description=The content of the item."`
	ContentType string `json:"contentType,omitempty" jsonschema:"description=The type of the content. Possible values are text and html.,enum=text|html"`
}

// Recipient is microsoft.graph.recipient.
type Recipient struct {
	EmailAddress EmailAddress `json:"emailAddress,omitempty"`
}

// EmailAddress is microsoft.graph.emailAddress.
type EmailAddress struct {
	Name    string `json:"name,omitempty" jsonschema:"description=The display name of the person or entity."`
	Address string `json:"address,omitempty" jsonschema:"description=The email address of the person or entity."`
}

// ---- Req types: path, query and header parameters ----

type (
	// getMeReq is the published parameter list of me.user.GetUser: the
	// ConsistencyLevel header plus the OData $select/$expand pair.
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
	// getUserReq is users.user.GetUser: no body, so the path parameter rides
	// the Req type and keeps its published description.
	getUserReq struct {
		UserID string   `path:"user-id" jsonschema:"description=The unique identifier of user"`
		Select []string `query:"$select,omitempty" jsonschema:"description=Select properties to be returned"`
		Expand []string `query:"$expand,omitempty" jsonschema:"description=Expand related entities"`
	}
	// deleteUserReq is users.user.DeleteUser.
	deleteUserReq struct {
		UserID  string `path:"user-id" jsonschema:"description=The unique identifier of user"`
		IfMatch string `header:"If-Match,omitempty" jsonschema:"description=ETag"`
	}
	// mediaReq is the profile-photo media operation: path parameter only.
	mediaReq struct {
		UserID string `path:"user-id" jsonschema:"description=The unique identifier of user"`
	}
	// sendMailReq is the sendMail action: the published path parameter plus
	// the sendMailRequestBody properties. Req is path+body together, so
	// specout splits the body out into its own component.
	sendMailReq struct {
		UserID          string  `path:"user-id" jsonschema:"description=The unique identifier of user"`
		Message         Message `json:"Message,omitempty" jsonschema:"description=The message to send."`
		SaveToSentItems bool    `json:"SaveToSentItems,omitempty" jsonschema:"description=Save the message in Sent Items."`
	}
)

// ---- responses: the published descriptions ----

// ok is one operation's published responses: the document keys every success
// 2XX, so the body the Res type derives is dropped from 200 and re-keyed, and
// every failure carries the document's own 4XX and 5XX range keys.
func ok(description string) []specout.Response {
	return append([]specout.Response{
		{Status: 200, Omit: true},
		{Status: 200, Key: "2XX", Raw: map[string]any{"description": description}},
	}, errs()...)
}

// noContent is the published 204: the concrete code Graph keeps for the
// operations that return no body, plus the two failure ranges.
func noContent() []specout.Response {
	return append([]specout.Response{desc(204, "Success")}, errs()...)
}

// errs is the published failure pair: components.responses.error, described
// "error", reached through the 4XX and 5XX range keys the document uses.
func errs() []specout.Response {
	return []specout.Response{
		{Key: "4XX", Type: ODataError{}, Raw: map[string]any{"description": "error"}},
		{Key: "5XX", Type: ODataError{}, Raw: map[string]any{"description": "error"}},
	}
}

// desc is a description-only response.
func desc(code int, description string) specout.Response {
	return specout.Response{Status: code, Type: struct{}{}, Raw: map[string]any{"description": description}}
}

// ---- the service ----

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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeError is the published error envelope, with the code the handler
// wrote.
func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, ODataError{Error: MainError{
		Code:    strings.ToLower(http.StatusText(code)),
		Message: message,
	}})
}

func pathUserID(r *http.Request) string { return mux.Vars(r)["user-id"] }

func getMe(s *store) specout.Handler[getMeReq, User] {
	return specout.Handler[getMeReq, User]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			var u User
			for _, one := range s.users {
				u = one
				break
			}
			s.mu.Unlock()
			writeJSON(w, 200, u)
		},
		Summary:     "Get a user",
		Description: "Retrieve the properties and relationships of user object.",
		OperationID: "me.user.GetUser",
		Tags:        []string{"me.user"},
		ExternalDocs: &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/user-get?view=graph-rest-1.0",
		},
		Responses: ok("Retrieved entity"),
	}
}

func listUsers(s *store) specout.Handler[listUsersReq, UserCollectionResponse] {
	return specout.Handler[listUsersReq, UserCollectionResponse]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			top, _ := strconv.Atoi(r.URL.Query().Get("$top"))
			s.mu.Lock()
			out := UserCollectionResponse{Value: []User{}}
			for id, u := range s.users {
				_ = id
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
		},
		Summary:     "List users",
		Description: "Retrieve a list of user objects.",
		OperationID: "users.user.ListUser",
		Tags:        []string{"users.user"},
		ExternalDocs: &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/user-list?view=graph-rest-1.0",
		},
		Responses: ok("Retrieved collection"),
	}
}

func createUser(s *store) specout.Handler[User, User] {
	return specout.Handler[User, User]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var u User
			if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
				writeError(w, 400, "The request body is malformed.")
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
		},
		Summary:     "Create User",
		Description: "Create a new user.",
		OperationID: "users.user.CreateUser",
		Tags:        []string{"users.user"},
		ExternalDocs: &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/user-post-users?view=graph-rest-1.0",
		},
		Responses: ok("Created entity"),
	}
}

func getUser(s *store) specout.Handler[getUserReq, User] {
	return specout.Handler[getUserReq, User]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			u, found := s.users[pathUserID(r)]
			s.mu.Unlock()
			if !found {
				writeError(w, 404, "Resource not found for the segment 'users'.")
				return
			}
			writeJSON(w, 200, u)
		},
		Summary:     "Get a user",
		Description: "Retrieve the properties and relationships of user object.",
		OperationID: "users.user.GetUser",
		Tags:        []string{"users.user"},
		ExternalDocs: &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/user-get?view=graph-rest-1.0",
		},
		Responses: ok("Retrieved entity"),
	}
}

// patchUser takes the user object itself as its request body: the published
// operation $refs microsoft.graph.user, and so does this one. The path
// parameter therefore has no Req field, so it carries no description.
func patchUser(s *store) specout.Handler[User, User] {
	return specout.Handler[User, User]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var patch User
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				writeError(w, 400, "The request body is malformed.")
				return
			}
			s.mu.Lock()
			u, found := s.users[pathUserID(r)]
			if found {
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
			}
			s.mu.Unlock()
			if !found {
				writeError(w, 404, "Resource not found for the segment 'users'.")
				return
			}
			writeJSON(w, 200, u)
		},
		Summary:     "Update user",
		Description: "Update the properties of a user object.",
		OperationID: "users.user.UpdateUser",
		Tags:        []string{"users.user"},
		ExternalDocs: &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/user-update?view=graph-rest-1.0",
		},
		Responses: ok("Success"),
	}
}

func deleteUser(s *store) specout.Handler[deleteUserReq, specout.NoContent] {
	return specout.Handler[deleteUserReq, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			id := pathUserID(r)
			s.mu.Lock()
			_, found := s.users[id]
			delete(s.users, id)
			delete(s.photos, id)
			s.mu.Unlock()
			if !found {
				writeError(w, 404, "Resource not found for the segment 'users'.")
				return
			}
			w.WriteHeader(204)
		},
		Summary:     "Delete a user",
		Description: "Delete a user object.",
		OperationID: "users.user.DeleteUser",
		Tags:        []string{"users.user"},
		ExternalDocs: &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/user-delete?view=graph-rest-1.0",
		},
		Responses: noContent(),
	}
}

// getPhoto is the media download: the published response is
// application/octet-stream, which Response.ContentType carries.
func getPhoto(s *store) specout.Handler[mediaReq, struct{}] {
	return specout.Handler[mediaReq, struct{}]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			photo, found := s.photos[pathUserID(r)]
			s.mu.Unlock()
			if !found {
				writeError(w, 404, "Resource not found for the segment 'photo'.")
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(200)
			w.Write(photo)
		},
		Summary:     "Get media content for the navigation property photo from users",
		Description: "The user's profile photo. Read-only.",
		OperationID: "users.GetPhotoContent",
		Tags:        []string{"users.profilePhoto"},
		// struct{} Res declares no default of its own, so the 2XX binary body
		// is the whole success; the failure ranges follow it.
		Responses: append([]specout.Response{{
			Status:      200,
			Key:         "2XX",
			ContentType: "application/octet-stream",
			Raw:         map[string]any{"description": "Retrieved media content"},
		}}, errs()...),
	}
}

func sendMail(s *store) specout.Handler[sendMailReq, specout.NoContent] {
	return specout.Handler[sendMailReq, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var req sendMailReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, 400, "The request body is malformed.")
				return
			}
			s.mu.Lock()
			_, found := s.users[pathUserID(r)]
			if found {
				s.sent = append(s.sent, req.Message)
			}
			s.mu.Unlock()
			if !found {
				writeError(w, 404, "Resource not found for the segment 'users'.")
				return
			}
			w.WriteHeader(204)
		},
		Summary:     "Invoke action sendMail",
		Description: "Send the message specified in the request body using either JSON or MIME format.",
		OperationID: "users.user.sendMail",
		Tags:        []string{"users.user.Actions"},
		ExternalDocs: &specout.ExternalDocs{
			Description: "Find more info here",
			URL:         "https://learn.microsoft.com/graph/api/user-sendmail?view=graph-rest-1.0",
		},
		Responses: noContent(),
	}
}

// graphDescription is info.description of the published document.
const graphDescription = "This OData service is located at https://graph.microsoft.com/v1.0"

// New builds the service: the generator, and the gorilla router that serves
// it. The published document declares no securitySchemes; Graph in practice
// takes a bearer token, which specout.Bearer would declare.
func New() (*specout.Generator, *mux.Router) {
	d := specout.New(specout.Config{
		Title:       "OData Service for namespace microsoft.graph",
		Version:     "v1.0",
		Description: graphDescription,
		Servers:     []specout.Server{{URL: "https://graph.microsoft.com/v1.0"}},
		Tags: []specout.Tag{
			{Name: "me.user"},
			{Name: "users.user"},
			{Name: "users.user.Actions"},
			{Name: "users.profilePhoto"},
		},
	})

	s := newStore()
	r := mux.NewRouter()
	b := specout.Gorilla(d, r)
	b.Get("/me", getMe(s))
	b.Get("/users", listUsers(s))
	b.Post("/users", createUser(s))
	b.Get("/users/{user-id}", getUser(s))
	b.Patch("/users/{user-id}", patchUser(s))
	b.Delete("/users/{user-id}", deleteUser(s))
	b.Get("/users/{user-id}/photo/$value", getPhoto(s))
	b.Post("/users/{user-id}/sendMail", sendMail(s))
	if err := b.Adopt(); err != nil {
		panic(err)
	}
	return d, r
}

// Swagger UI over the generated spec.
const swaggerPage = `<!DOCTYPE html>
<html>
<head>
  <title>specout example — Microsoft Graph subset</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>window.ui = SwaggerUIBundle({ url: '/openapi.json', dom_id: '#swagger-ui' })</script>
</body>
</html>`

// Handler serves the demo: Swagger UI at /, the spec at /openapi.json, and
// the API at the published paths. The published server URL is absolute
// (https://graph.microsoft.com/v1.0), so a local demo serves the same paths
// without a prefix, and Swagger UI "try it out" aims at the real Graph.
func Handler(d *specout.Generator, r http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/openapi.json", d)
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" {
			r.ServeHTTP(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, swaggerPage)
	})
	return mux
}

func main() {
	d, r := New()
	if os.Getenv("GO_SPEC_ONLY") != "" {
		if err := d.WriteJSON(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	fmt.Println("msgraph: http://localhost:8082/   spec: http://localhost:8082/openapi.json")
	if err := http.ListenAndServe(":8082", Handler(d, r)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
