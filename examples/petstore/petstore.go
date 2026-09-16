// Command petstore rebuilds the published Swagger Petstore definition
// (OpenAPI 3.0.4, 19 operations, 13 paths) with specout types, and serves it
// with chi. Every shape in the published document is carried over in the
// types below; examples/README.md lists the handful that are not.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/happytoolin/specout"
)

// ---- components.schemas, field for field ----

// Category is components.schemas.Category.
type Category struct {
	ID   int64  `json:"id,omitempty" jsonschema:"format=int64,example=1"`
	Name string `json:"name,omitempty" jsonschema:"example=Dogs"`
}

// Tag is components.schemas.Tag.
type Tag struct {
	ID   int64  `json:"id,omitempty" jsonschema:"format=int64"`
	Name string `json:"name,omitempty"`
}

// Pet is components.schemas.Pet: required name + photoUrls, enum status.
type Pet struct {
	ID        int64    `json:"id,omitempty" jsonschema:"format=int64,example=10"`
	Name      string   `json:"name" jsonschema:"example=doggie"`
	Category  Category `json:"category,omitempty"`
	PhotoURLs []string `json:"photoUrls"`
	Tags      []Tag    `json:"tags,omitempty"`
	Status    string   `json:"status,omitempty" jsonschema:"description=pet status in the store,enum=available|pending|sold"`
}

// Order is components.schemas.Order.
type Order struct {
	ID       int64     `json:"id,omitempty" jsonschema:"format=int64,example=10"`
	PetID    int64     `json:"petId,omitempty" jsonschema:"format=int64,example=198772"`
	Quantity int32     `json:"quantity,omitempty" jsonschema:"format=int32,example=7"`
	ShipDate time.Time `json:"shipDate,omitempty"`
	Status   string    `json:"status,omitempty" jsonschema:"description=Order Status,example=approved,enum=placed|approved|delivered"`
	Complete bool      `json:"complete,omitempty"`
}

// User is components.schemas.User.
type User struct {
	ID         int64  `json:"id,omitempty" jsonschema:"format=int64,example=10"`
	Username   string `json:"username,omitempty" jsonschema:"example=theUser"`
	FirstName  string `json:"firstName,omitempty" jsonschema:"example=John"`
	LastName   string `json:"lastName,omitempty" jsonschema:"example=James"`
	Email      string `json:"email,omitempty" jsonschema:"example=john@email.com"`
	Password   string `json:"password,omitempty" jsonschema:"example=12345"`
	Phone      string `json:"phone,omitempty" jsonschema:"example=12345"`
	UserStatus int32  `json:"userStatus,omitempty" jsonschema:"description=User Status,format=int32,example=1"`
}

// ApiResponse is components.schemas.ApiResponse.
type ApiResponse struct {
	Code    int32  `json:"code,omitempty"`
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

// ---- Req types: parameters and bodies, one per operation shape ----

// A path:"name" field types the {name} placeholder: without one the
// placeholder is a plain string. Query and header fields become parameters.
type (
	findPetsByStatusReq struct {
		Status string `query:"status" jsonschema:"description=Status values that need to be considered for filter,default=available,enum=available|pending|sold"`
	}
	findPetsByTagsReq struct {
		Tags []string `query:"tags" jsonschema:"description=Tags to filter by"`
	}
	getPetReq struct {
		PetID int64 `path:"petId" jsonschema:"format=int64,description=ID of pet to return"`
	}
	updatePetFormReq struct {
		PetID  int64  `path:"petId" jsonschema:"format=int64,description=ID of pet that needs to be updated"`
		Name   string `query:"name,omitempty" jsonschema:"description=Name of pet that needs to be updated"`
		Status string `query:"status,omitempty" jsonschema:"description=Status of pet that needs to be updated"`
	}
	deletePetReq struct {
		PetID  int64  `path:"petId" jsonschema:"format=int64,description=Pet id to delete"`
		APIKey string `header:"api_key,omitempty"`
	}
	uploadImageReq struct {
		PetID              int64        `path:"petId" jsonschema:"format=int64,description=ID of pet to update"`
		AdditionalMetadata string       `query:"additionalMetadata,omitempty" jsonschema:"description=Additional Metadata"`
		File               specout.File `form:"file" jsonschema:"description=Upload image of the pet"`
	}
	getOrderReq struct {
		OrderID int64 `path:"orderId" jsonschema:"format=int64,description=ID of order that needs to be fetched"`
	}
	deleteOrderReq struct {
		OrderID int64 `path:"orderId" jsonschema:"format=int64,description=ID of the order that needs to be deleted"`
	}
	loginUserReq struct {
		Username string `query:"username,omitempty" jsonschema:"description=The user name for login"`
		Password string `query:"password,omitempty" jsonschema:"description=The password for login in clear text"`
	}
	getUserReq struct {
		Username string `path:"username" jsonschema:"description=The name that needs to be fetched. Use user1 for testing"`
	}
	deleteUserReq struct {
		Username string `path:"username" jsonschema:"description=The name that needs to be deleted"`
	}
)

// ---- responses: the published descriptions, one helper per shape ----

// ok overrides the description of the Res-derived 200 response.
func ok(description string) specout.Response {
	return specout.Response{Status: 200, Raw: map[string]any{"description": description}}
}

// desc is a description-only response: the published document declares its
// errors and its few body-less 200s this way.
func desc(code int, description string) specout.Response {
	return specout.Response{Status: code, Type: struct{}{}, Raw: map[string]any{"description": description}}
}

// def is the "default" catch-all response.
func def(description string) specout.Response {
	return desc(0, description)
}

// ---- the service ----

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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter, msg string) { writeJSON(w, 404, map[string]any{"message": msg}) }

func petID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "petId"), 10, 64)
	return id
}

func updatePet(s *store) specout.Handler[Pet, Pet] {
	return specout.Handler[Pet, Pet]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var p Pet
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				writeJSON(w, 400, map[string]any{"message": "Invalid ID supplied"})
				return
			}
			s.mu.Lock()
			if p.ID == 0 {
				p.ID = s.nextID()
			}
			s.pets[p.ID] = p
			s.mu.Unlock()
			writeJSON(w, 200, p)
		},
		Summary:     "Update an existing pet.",
		Description: "Update an existing pet by Id.",
		OperationID: "updatePet",
		Tags:        []string{"pet"},
		Responses: []specout.Response{
			ok("Successful operation"), desc(400, "Invalid ID supplied"),
			desc(404, "Pet not found"), desc(422, "Validation exception"), def("Unexpected error"),
		},
	}
}

func addPet(s *store) specout.Handler[Pet, Pet] {
	return specout.Handler[Pet, Pet]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var p Pet
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				writeJSON(w, 400, map[string]any{"message": "Invalid input"})
				return
			}
			s.mu.Lock()
			p.ID = s.nextID()
			s.pets[p.ID] = p
			s.mu.Unlock()
			writeJSON(w, 200, p)
		},
		Summary:     "Add a new pet to the store.",
		Description: "Add a new pet to the store.",
		OperationID: "addPet",
		Tags:        []string{"pet"},
		Responses: []specout.Response{
			ok("Successful operation"), desc(400, "Invalid input"),
			desc(422, "Validation exception"), def("Unexpected error"),
		},
	}
}

func findPetsByStatus(s *store) specout.Handler[findPetsByStatusReq, []Pet] {
	return specout.Handler[findPetsByStatusReq, []Pet]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			want := r.URL.Query().Get("status")
			s.mu.Lock()
			out := []Pet{}
			for _, p := range s.pets {
				if want == "" || p.Status == want {
					out = append(out, p)
				}
			}
			s.mu.Unlock()
			writeJSON(w, 200, out)
		},
		Summary:     "Finds Pets by status.",
		Description: "Multiple status values can be provided with comma separated strings.",
		OperationID: "findPetsByStatus",
		Tags:        []string{"pet"},
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "Invalid status value"), def("Unexpected error"),
		},
	}
}

func findPetsByTags(s *store) specout.Handler[findPetsByTagsReq, []Pet] {
	return specout.Handler[findPetsByTagsReq, []Pet]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			want := r.URL.Query()["tags"]
			s.mu.Lock()
			out := []Pet{}
			for _, p := range s.pets {
				for _, t := range p.Tags {
					for _, wt := range want {
						if t.Name == wt {
							out = append(out, p)
						}
					}
				}
			}
			s.mu.Unlock()
			writeJSON(w, 200, out)
		},
		Summary:     "Finds Pets by tags.",
		Description: "Multiple tags can be provided with comma separated strings. Use tag1, tag2, tag3 for testing.",
		OperationID: "findPetsByTags",
		Tags:        []string{"pet"},
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "Invalid tag value"), def("Unexpected error"),
		},
	}
}

func getPetByID(s *store) specout.Handler[getPetReq, Pet] {
	return specout.Handler[getPetReq, Pet]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			p, found := s.pets[petID(r)]
			s.mu.Unlock()
			if !found {
				notFound(w, "Pet not found")
				return
			}
			writeJSON(w, 200, p)
		},
		Summary:     "Find pet by ID.",
		Description: "Returns a single pet.",
		OperationID: "getPetById",
		Tags:        []string{"pet"},
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "Invalid ID supplied"),
			desc(404, "Pet not found"), def("Unexpected error"),
		},
	}
}

func updatePetWithForm(s *store) specout.Handler[updatePetFormReq, Pet] {
	return specout.Handler[updatePetFormReq, Pet]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			id := petID(r)
			s.mu.Lock()
			p, found := s.pets[id]
			if found {
				if v := r.URL.Query().Get("name"); v != "" {
					p.Name = v
				}
				if v := r.URL.Query().Get("status"); v != "" {
					p.Status = v
				}
				s.pets[id] = p
			}
			s.mu.Unlock()
			if !found {
				notFound(w, "Pet not found")
				return
			}
			writeJSON(w, 200, p)
		},
		Summary:     "Updates a pet in the store with form data.",
		Description: "Updates a pet resource based on the form data.",
		OperationID: "updatePetWithForm",
		Tags:        []string{"pet"},
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "Invalid input"), def("Unexpected error"),
		},
	}
}

func deletePet(s *store) specout.Handler[deletePetReq, struct{}] {
	return specout.Handler[deletePetReq, struct{}]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			delete(s.pets, petID(r))
			s.mu.Unlock()
			w.WriteHeader(200)
		},
		Summary:     "Deletes a pet.",
		Description: "Delete a pet.",
		OperationID: "deletePet",
		Tags:        []string{"pet"},
		Responses: []specout.Response{
			desc(200, "Pet deleted"), desc(400, "Invalid pet value"), def("Unexpected error"),
		},
	}
}

func uploadFile(s *store) specout.Handler[uploadImageReq, ApiResponse] {
	return specout.Handler[uploadImageReq, ApiResponse]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			_, found := s.pets[petID(r)]
			s.mu.Unlock()
			if !found {
				notFound(w, "Pet not found")
				return
			}
			// declaration only: the multipart body is yours to read.
			f, _, err := r.FormFile("file")
			if err != nil {
				writeJSON(w, 400, map[string]any{"message": "No file uploaded"})
				return
			}
			f.Close()
			writeJSON(w, 200, ApiResponse{Code: 200, Type: "ok", Message: "file uploaded"})
		},
		Summary:     "Uploads an image.",
		Description: "Upload image of the pet.",
		OperationID: "uploadFile",
		Tags:        []string{"pet"},
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "No file uploaded"),
			desc(404, "Pet not found"), def("Unexpected error"),
		},
	}
}

func getInventory(s *store) specout.Handler[struct{}, map[string]int32] {
	return specout.Handler[struct{}, map[string]int32]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			out := map[string]int32{}
			for _, p := range s.pets {
				out[p.Status]++
			}
			s.mu.Unlock()
			writeJSON(w, 200, out)
		},
		Summary:     "Returns pet inventories by status.",
		Description: "Returns a map of status codes to quantities.",
		OperationID: "getInventory",
		Tags:        []string{"store"},
		Responses: []specout.Response{
			ok("successful operation"), def("Unexpected error"),
		},
	}
}

func placeOrder(s *store) specout.Handler[Order, Order] {
	return specout.Handler[Order, Order]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var o Order
			if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
				writeJSON(w, 400, map[string]any{"message": "Invalid input"})
				return
			}
			s.mu.Lock()
			o.ID = s.nextID()
			s.orders[o.ID] = o
			s.mu.Unlock()
			writeJSON(w, 200, o)
		},
		Summary:     "Place an order for a pet.",
		Description: "Place a new order in the store.",
		OperationID: "placeOrder",
		Tags:        []string{"store"},
		Public:      true,
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "Invalid input"),
			desc(422, "Validation exception"), def("Unexpected error"),
		},
	}
}

func getOrderByID(s *store) specout.Handler[getOrderReq, Order] {
	return specout.Handler[getOrderReq, Order]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.ParseInt(chi.URLParam(r, "orderId"), 10, 64)
			s.mu.Lock()
			o, found := s.orders[id]
			s.mu.Unlock()
			if !found {
				notFound(w, "Order not found")
				return
			}
			writeJSON(w, 200, o)
		},
		Summary:     "Find purchase order by ID.",
		Description: "For valid response try integer IDs with value <= 5 or > 10. Other values will generate exceptions.",
		OperationID: "getOrderById",
		Tags:        []string{"store"},
		Public:      true,
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "Invalid ID supplied"),
			desc(404, "Order not found"), def("Unexpected error"),
		},
	}
}

func deleteOrder(s *store) specout.Handler[deleteOrderReq, struct{}] {
	return specout.Handler[deleteOrderReq, struct{}]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.ParseInt(chi.URLParam(r, "orderId"), 10, 64)
			s.mu.Lock()
			_, found := s.orders[id]
			delete(s.orders, id)
			s.mu.Unlock()
			if !found {
				notFound(w, "Order not found")
				return
			}
			w.WriteHeader(200)
		},
		Summary:     "Delete purchase order by identifier.",
		Description: "For valid response try integer IDs with value < 1000. Anything above 1000 or non-integers will generate API errors.",
		OperationID: "deleteOrder",
		Tags:        []string{"store"},
		Public:      true,
		Responses: []specout.Response{
			desc(200, "order deleted"), desc(400, "Invalid ID supplied"),
			desc(404, "Order not found"), def("Unexpected error"),
		},
	}
}

func createUser(s *store) specout.Handler[User, User] {
	return specout.Handler[User, User]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var u User
			if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
				writeJSON(w, 400, map[string]any{"message": "Invalid input"})
				return
			}
			s.mu.Lock()
			s.users[u.Username] = u
			s.mu.Unlock()
			writeJSON(w, 200, u)
		},
		Summary:     "Create user.",
		Description: "This can only be done by the logged in user.",
		OperationID: "createUser",
		Tags:        []string{"user"},
		Public:      true,
		Responses: []specout.Response{
			ok("successful operation"), def("Unexpected error"),
		},
	}
}

func createUsersWithListInput(s *store) specout.Handler[[]User, User] {
	return specout.Handler[[]User, User]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var u []User
			if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
				writeJSON(w, 400, map[string]any{"message": "Invalid input"})
				return
			}
			s.mu.Lock()
			for _, one := range u {
				s.users[one.Username] = one
			}
			s.mu.Unlock()
			if len(u) == 0 {
				writeJSON(w, 200, User{})
				return
			}
			writeJSON(w, 200, u[0])
		},
		Summary:     "Creates list of users with given input array.",
		Description: "Creates list of users with given input array.",
		OperationID: "createUsersWithListInput",
		Tags:        []string{"user"},
		Public:      true,
		Responses: []specout.Response{
			ok("Successful operation"), def("Unexpected error"),
		},
	}
}

func loginUser(s *store) specout.Handler[loginUserReq, string] {
	return specout.Handler[loginUserReq, string]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			name := r.URL.Query().Get("username")
			s.mu.Lock()
			_, found := s.users[name]
			s.mu.Unlock()
			if !found {
				writeJSON(w, 400, map[string]any{"message": "Invalid username/password supplied"})
				return
			}
			w.Header().Set("X-Rate-Limit", "5000")
			w.Header().Set("X-Expires-After", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
			writeJSON(w, 200, "logged in user session: "+name)
		},
		Summary:     "Logs user into the system.",
		Description: "Log into the system.",
		OperationID: "loginUser",
		Tags:        []string{"user"},
		Public:      true,
		Responses: []specout.Response{
			{Status: 200, Raw: map[string]any{"description": "successful operation"}, Headers: []specout.Header{
				{Name: "X-Rate-Limit", Type: int32(0)},
				{Name: "X-Expires-After", Type: time.Time{}},
			}},
			desc(400, "Invalid username/password supplied"), def("Unexpected error"),
		},
	}
}

func logoutUser() specout.Handler[struct{}, struct{}] {
	return specout.Handler[struct{}, struct{}]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		},
		Summary:     "Logs out current logged in user session.",
		Description: "Log user out of the system.",
		OperationID: "logoutUser",
		Tags:        []string{"user"},
		Public:      true,
		Responses: []specout.Response{
			desc(200, "successful operation"), def("Unexpected error"),
		},
	}
}

func getUserByName(s *store) specout.Handler[getUserReq, User] {
	return specout.Handler[getUserReq, User]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			u, found := s.users[chi.URLParam(r, "username")]
			s.mu.Unlock()
			if !found {
				notFound(w, "User not found")
				return
			}
			writeJSON(w, 200, u)
		},
		Summary:     "Get user by user name.",
		Description: "Get user detail based on username.",
		OperationID: "getUserByName",
		Tags:        []string{"user"},
		Public:      true,
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "Invalid username supplied"),
			desc(404, "User not found"), def("Unexpected error"),
		},
	}
}

// updateUser takes the body as Req and no path tag: the {username} placeholder
// stays a plain string, as in the published document.
func updateUser(s *store) specout.Handler[User, struct{}] {
	return specout.Handler[User, struct{}]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			var u User
			if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
				writeJSON(w, 400, map[string]any{"message": "bad request"})
				return
			}
			name := chi.URLParam(r, "username")
			s.mu.Lock()
			if u.Username == "" {
				u.Username = name
			}
			s.users[name] = u
			s.mu.Unlock()
			w.WriteHeader(200)
		},
		Summary:     "Update user resource.",
		Description: "This can only be done by the logged in user.",
		OperationID: "updateUser",
		Tags:        []string{"user"},
		Public:      true,
		Responses: []specout.Response{
			ok("successful operation"), desc(400, "bad request"),
			desc(404, "user not found"), def("Unexpected error"),
		},
	}
}

func deleteUser(s *store) specout.Handler[deleteUserReq, struct{}] {
	return specout.Handler[deleteUserReq, struct{}]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			name := chi.URLParam(r, "username")
			s.mu.Lock()
			_, found := s.users[name]
			delete(s.users, name)
			s.mu.Unlock()
			if !found {
				notFound(w, "User not found")
				return
			}
			w.WriteHeader(200)
		},
		Summary:     "Delete user resource.",
		Description: "This can only be done by the logged in user.",
		OperationID: "deleteUser",
		Tags:        []string{"user"},
		Public:      true,
		Responses: []specout.Response{
			desc(200, "User deleted"), desc(400, "Invalid username supplied"),
			desc(404, "User not found"), def("Unexpected error"),
		},
	}
}

// petstoreDescription is info.description of the published document.
const petstoreDescription = `This is a sample Pet Store Server based on the OpenAPI 3.0 specification. You can find out more about Swagger at [https://swagger.io](https://swagger.io). In the third iteration of the pet store, we've switched to the design first approach!
You can now help us improve the API whether it's by making changes to the definition itself or to the code.

Some useful links:
- [The Pet Store repository](https://github.com/swagger-api/swagger-petstore)
- [The source API definition for the Pet Store](https://github.com/swagger-api/swagger-petstore/blob/master/src/main/resources/openapi.yaml)`

// New builds the service: the generator, and the chi router that serves it.
func New() (*specout.Generator, http.Handler) {
	d := specout.New(specout.Config{
		Title:       "Swagger Petstore - OpenAPI 3.0",
		Version:     "1.0.27",
		Description: petstoreDescription,
		Servers:     []specout.Server{{URL: "/api/v3"}},
		// the published document also declares petstore_auth (oauth2 implicit
		// with write:pets / read:pets scopes): specout has no oauth2 scheme.
		Auth:         []specout.AuthScheme{specout.APIKey("api_key", "api_key", specout.InHeader)},
		ExternalDocs: &specout.ExternalDocs{URL: "https://swagger.io", Description: "Find out more about Swagger"},
		Tags: []specout.Tag{
			{Name: "pet", Description: "Everything about your Pets"},
			{Name: "store", Description: "Access to Petstore orders"},
			{Name: "user", Description: "Operations about user"},
		},
	})

	s := newStore()
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)

	// One binder on the root router with the published absolute patterns: a
	// group root would document "/pet/" where the published document has "/pet".
	b := specout.Chi(d, r)
	b.Put("/pet", updatePet(s))
	b.Post("/pet", addPet(s))
	b.Get("/pet/findByStatus", findPetsByStatus(s))
	b.Get("/pet/findByTags", findPetsByTags(s))
	b.Get("/pet/{petId}", getPetByID(s))
	b.Post("/pet/{petId}", updatePetWithForm(s))
	b.Delete("/pet/{petId}", deletePet(s))
	b.Post("/pet/{petId}/uploadImage", uploadFile(s))
	b.Get("/store/inventory", getInventory(s))
	b.Post("/store/order", placeOrder(s))
	b.Get("/store/order/{orderId}", getOrderByID(s))
	b.Delete("/store/order/{orderId}", deleteOrder(s))
	b.Post("/user", createUser(s))
	b.Post("/user/createWithList", createUsersWithListInput(s))
	b.Get("/user/login", loginUser(s))
	b.Get("/user/logout", logoutUser())
	b.Get("/user/{username}", getUserByName(s))
	b.Put("/user/{username}", updateUser(s))
	b.Delete("/user/{username}", deleteUser(s))

	r.Mount("/openapi.json", d)
	// Adopt registers the build-time walk and fails on undocumented routes.
	if err := b.Adopt(); err != nil {
		panic(err)
	}
	return d, r
}

// Swagger UI over the generated spec.
const swaggerPage = `<!DOCTYPE html>
<html>
<head>
  <title>specout example — Swagger Petstore</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>window.ui = SwaggerUIBundle({ url: '/api/v3/openapi.json', dom_id: '#swagger-ui' })</script>
</body>
</html>`

// Handler wraps the chi router with the demo page and the published server
// prefix: the document declares servers: [/api/v3], so the API is served
// there and the Swagger UI "try it out" button hits real routes.
func Handler(d *specout.Generator, r http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v3/", http.StripPrefix("/api/v3", r))
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" {
			http.NotFound(w, req)
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
	fmt.Println("petstore: http://localhost:8081/   spec: http://localhost:8081/api/v3/openapi.json")
	if err := http.ListenAndServe(":8081", Handler(d, r)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
