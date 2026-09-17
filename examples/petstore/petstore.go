// Command petstore rebuilds the published Swagger Petstore definition
// (OpenAPI 3.0.4, 19 operations, 13 paths) with specout types, and serves it
// with chi. Every shape in the published document is carried over in the
// types below; examples/README.md lists the handful that are not.
package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/examplekit"
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

// ---- responses: the published media types and descriptions ----

// ok and okJSON override the description of the Res-derived 200: ok is the
// published JSON + XML pair, okJSON the operations that answer JSON only.
func ok(description string) specout.Response {
	return specout.Response{Status: 200, ContentTypes: jsonOrXML, Raw: map[string]any{"description": description}}
}

func okJSON(description string) specout.Response {
	return specout.Response{Status: 200, Raw: map[string]any{"description": description}}
}

// desc is a description-only response: the published document declares its
// errors and its few body-less 200s this way; def is its "default" catch-all.
func desc(code int, description string) specout.Response {
	return specout.Response{Status: code, Type: struct{}{}, Raw: map[string]any{"description": description}}
}

func def(description string) specout.Response { return desc(0, description) }

var (
	jsonOrXML   = []string{"application/json", "application/xml"}
	jsonXMLForm = []string{"application/json", "application/xml", "application/x-www-form-urlencoded"}

	// The published tag of each group, then the answers more than one operation
	// declares: one variable per published description.
	tagPet, tagStore, tagUser = []string{"pet"}, []string{"store"}, []string{"user"}

	unexpected      = def("Unexpected error")
	invalidInput    = desc(400, "Invalid input")
	invalidID       = desc(400, "Invalid ID supplied")
	invalidUsername = desc(400, "Invalid username supplied")
	validation      = desc(422, "Validation exception")
	petNotFound     = desc(404, "Pet not found")
	orderNotFound   = desc(404, "Order not found")
	userNotFound    = desc(404, "User not found")
)

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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

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

// op builds one operation: its published metadata, the two fields the
// operations vary on, and its http body. Every published operation ends with
// the same "Unexpected error" default, so op adds it.
func op[Req, Res any](id, summary, description string, tags []string, public bool, requestTypes []string, fn http.HandlerFunc, responses ...specout.Response) specout.Handler[Req, Res] {
	return specout.Handler[Req, Res]{
		OperationID: id, Summary: summary, Description: description, Tags: tags, Public: public,
		RequestContentTypes: requestTypes, HandlerFunc: fn, Responses: append(responses, unexpected),
	}
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

// petRoutes registers the eight pet operations.
func petRoutes(b *specout.ChiRouter, s *store) {
	b.Put("/pet", op[Pet, Pet]("updatePet", "Update an existing pet.", "Update an existing pet by Id.", tagPet, false, jsonXMLForm,
		func(w http.ResponseWriter, r *http.Request) {
			storeBody(w, r, s, "Invalid ID supplied", s.pets, func(p *Pet) int64 {
				if p.ID == 0 {
					p.ID = s.nextID()
				}
				return p.ID
			})
		},
		ok("Successful operation"), invalidID, petNotFound, validation))
	b.Post("/pet", op[Pet, Pet]("addPet", "Add a new pet to the store.", "Add a new pet to the store.", tagPet, false, jsonXMLForm,
		func(w http.ResponseWriter, r *http.Request) {
			storeBody(w, r, s, "Invalid input", s.pets, func(p *Pet) int64 { p.ID = s.nextID(); return p.ID })
		},
		ok("Successful operation"), invalidInput, validation))
	b.Get("/pet/findByStatus", op[findPetsByStatusReq, []Pet]("findPetsByStatus", "Finds Pets by status.",
		"Multiple status values can be provided with comma separated strings.", tagPet, false, nil,
		func(w http.ResponseWriter, r *http.Request) {
			want := r.URL.Query().Get("status")
			out := []Pet{}
			for _, p := range s.petsList() {
				if want == "" || p.Status == want {
					out = append(out, p)
				}
			}
			writeJSON(w, 200, out)
		},
		ok("successful operation"), desc(400, "Invalid status value")))
	b.Get("/pet/findByTags", op[findPetsByTagsReq, []Pet]("findPetsByTags", "Finds Pets by tags.",
		"Multiple tags can be provided with comma separated strings. Use tag1, tag2, tag3 for testing.", tagPet, false, nil,
		func(w http.ResponseWriter, r *http.Request) {
			want := r.URL.Query()["tags"]
			out := []Pet{}
			for _, p := range s.petsList() {
				for _, t := range p.Tags {
					for _, wt := range want {
						if t.Name == wt {
							out = append(out, p)
						}
					}
				}
			}
			writeJSON(w, 200, out)
		},
		ok("successful operation"), desc(400, "Invalid tag value")))
	b.Get("/pet/{petId}", op[getPetReq, Pet]("getPetById", "Find pet by ID.", "Returns a single pet.", tagPet, false, nil,
		func(w http.ResponseWriter, r *http.Request) { show(w, s, s.pets, pathID(r, "petId"), "Pet not found") },
		ok("successful operation"), invalidID, petNotFound))
	b.Post("/pet/{petId}", op[updatePetFormReq, Pet]("updatePetWithForm", "Updates a pet in the store with form data.",
		"Updates a pet resource based on the form data.", tagPet, false, nil,
		func(w http.ResponseWriter, r *http.Request) {
			id := pathID(r, "petId")
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
		ok("successful operation"), invalidInput))
	b.Delete("/pet/{petId}", op[deletePetReq, struct{}]("deletePet", "Deletes a pet.", "Delete a pet.", tagPet, false, nil,
		func(w http.ResponseWriter, r *http.Request) {
			take(s, s.pets, pathID(r, "petId"))
			w.WriteHeader(200)
		},
		desc(200, "Pet deleted"), desc(400, "Invalid pet value")))
	b.Post("/pet/{petId}/uploadImage", op[uploadImageReq, ApiResponse]("uploadFile", "Uploads an image.", "Upload image of the pet.", tagPet, false, nil,
		func(w http.ResponseWriter, r *http.Request) {
			if _, found := get(s, s.pets, pathID(r, "petId")); !found {
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
		okJSON("successful operation"), desc(400, "No file uploaded"), petNotFound))
}

// storeRoutes registers the four store operations.
func storeRoutes(b *specout.ChiRouter, s *store) {
	b.Get("/store/inventory", op[struct{}, map[string]int32]("getInventory", "Returns pet inventories by status.",
		"Returns a map of status codes to quantities.", tagStore, false, nil,
		func(w http.ResponseWriter, r *http.Request) {
			out := map[string]int32{}
			for _, p := range s.petsList() {
				out[p.Status]++
			}
			writeJSON(w, 200, out)
		},
		okJSON("successful operation")))
	b.Post("/store/order", op[Order, Order]("placeOrder", "Place an order for a pet.", "Place a new order in the store.", tagStore, true, jsonXMLForm,
		func(w http.ResponseWriter, r *http.Request) {
			storeBody(w, r, s, "Invalid input", s.orders, func(o *Order) int64 { o.ID = s.nextID(); return o.ID })
		},
		okJSON("successful operation"), invalidInput, validation))
	b.Get("/store/order/{orderId}", op[getOrderReq, Order]("getOrderById", "Find purchase order by ID.",
		"For valid response try integer IDs with value <= 5 or > 10. Other values will generate exceptions.",
		tagStore, true, nil,
		func(w http.ResponseWriter, r *http.Request) {
			show(w, s, s.orders, pathID(r, "orderId"), "Order not found")
		},
		ok("successful operation"), invalidID, orderNotFound))
	b.Delete("/store/order/{orderId}", op[deleteOrderReq, struct{}]("deleteOrder", "Delete purchase order by identifier.",
		"For valid response try integer IDs with value < 1000. Anything above 1000 or non-integers will generate API errors.",
		tagStore, true, nil,
		func(w http.ResponseWriter, r *http.Request) {
			deleteOr404(w, s, s.orders, pathID(r, "orderId"), "Order not found")
		},
		desc(200, "order deleted"), invalidID, orderNotFound))
}

// userRoutes registers the seven user operations.
func userRoutes(b *specout.ChiRouter, s *store) {
	b.Post("/user", op[User, User]("createUser", "Create user.", "This can only be done by the logged in user.", tagUser, true, jsonXMLForm,
		func(w http.ResponseWriter, r *http.Request) {
			storeBody(w, r, s, "Invalid input", s.users, func(u *User) string { return u.Username })
		},
		ok("successful operation")))
	b.Post("/user/createWithList", op[[]User, User]("createUsersWithListInput", "Creates list of users with given input array.",
		"Creates list of users with given input array.", tagUser, true, nil,
		func(w http.ResponseWriter, r *http.Request) {
			withBody(w, r, "Invalid input", func(us []User) {
				s.mu.Lock()
				for _, u := range us {
					s.users[u.Username] = u
				}
				s.mu.Unlock()
				if len(us) == 0 {
					writeJSON(w, 200, User{})
					return
				}
				writeJSON(w, 200, us[0])
			})
		},
		ok("Successful operation")))
	b.Get("/user/login", op[loginUserReq, string]("loginUser", "Logs user into the system.", "Log into the system.", tagUser, true, nil,
		func(w http.ResponseWriter, r *http.Request) {
			name := r.URL.Query().Get("username")
			if _, found := get(s, s.users, name); !found {
				writeJSON(w, 400, map[string]any{"message": "Invalid username/password supplied"})
				return
			}
			w.Header().Set("X-Rate-Limit", "5000")
			w.Header().Set("X-Expires-After", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
			writeJSON(w, 200, "logged in user session: "+name)
		},
		specout.Response{Status: 200, ContentTypes: jsonOrXML, Raw: map[string]any{"description": "successful operation"},
			Headers: []specout.Header{
				{Name: "X-Rate-Limit", Type: int32(0)},
				{Name: "X-Expires-After", Type: time.Time{}},
			}},
		desc(400, "Invalid username/password supplied")))
	b.Get("/user/logout", op[struct{}, struct{}]("logoutUser", "Logs out current logged in user session.", "Log user out of the system.", tagUser, true, nil,
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
		desc(200, "successful operation")))
	b.Get("/user/{username}", op[getUserReq, User]("getUserByName", "Get user by user name.", "Get user detail based on username.", tagUser, true, nil,
		func(w http.ResponseWriter, r *http.Request) {
			show(w, s, s.users, chi.URLParam(r, "username"), "User not found")
		},
		ok("successful operation"), invalidUsername, userNotFound))
	// updateUser takes the body as Req and no path tag: the {username}
	// placeholder stays a plain string, as in the published document.
	b.Put("/user/{username}", op[User, struct{}]("updateUser", "Update user resource.", "This can only be done by the logged in user.",
		tagUser, true, jsonXMLForm,
		func(w http.ResponseWriter, r *http.Request) {
			withBody(w, r, "bad request", func(u User) {
				name := chi.URLParam(r, "username")
				s.mu.Lock()
				if u.Username == "" {
					u.Username = name
				}
				s.users[name] = u
				s.mu.Unlock()
				w.WriteHeader(200)
			})
		},
		desc(200, "successful operation"), desc(400, "bad request"), desc(404, "user not found")))
	b.Delete("/user/{username}", op[deleteUserReq, struct{}]("deleteUser", "Delete user resource.",
		"This can only be done by the logged in user.", tagUser, true, nil,
		func(w http.ResponseWriter, r *http.Request) {
			deleteOr404(w, s, s.users, chi.URLParam(r, "username"), "User not found")
		},
		desc(200, "User deleted"), invalidUsername, userNotFound))
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
		// the published pair: an api key in the header, and the petstore's
		// own oauth2 scheme with its two pet scopes.
		Auth: []specout.AuthScheme{
			specout.APIKey("api_key", "api_key", specout.InHeader),
			specout.OAuth2("petstore_auth", map[string]specout.OAuth2Flow{
				"implicit": {
					AuthorizationURL: "https://petstore3.swagger.io/oauth/authorize",
					Scopes: map[string]string{
						"write:pets": "modify pets in your account",
						"read:pets":  "read your pets",
					},
				},
			}),
		},
		ExternalDocs:   &specout.ExternalDocs{URL: "https://swagger.io", Description: "Find out more about Swagger"},
		TermsOfService: "https://swagger.io/terms/",
		Contact:        &specout.Contact{Email: "apiteam@swagger.io"},
		License: &specout.License{
			Name: "Apache 2.0",
			URL:  "https://www.apache.org/licenses/LICENSE-2.0.html",
		},
		Tags: []specout.Tag{
			{Name: "pet", Description: "Everything about your Pets",
				ExternalDocs: &specout.ExternalDocs{URL: "https://swagger.io", Description: "Find out more"}},
			{Name: "store", Description: "Access to Petstore orders",
				ExternalDocs: &specout.ExternalDocs{URL: "https://swagger.io", Description: "Find out more about our store"}},
			{Name: "user", Description: "Operations about user"},
		},
	})

	s := newStore()
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)

	// One binder on the root router with the published absolute patterns: a
	// group root would document "/pet/" where the published document has "/pet".
	b := specout.Chi(d, r)
	petRoutes(b, s)
	storeRoutes(b, s)
	userRoutes(b, s)

	r.Mount("/openapi.json", d)
	// Adopt registers the build-time walk and fails on undocumented routes.
	if err := b.Adopt(); err != nil {
		panic(err)
	}
	return d, r
}

// Handler wraps the chi router with the demo page and the published server
// prefix: the document declares servers: [/api/v3], so the API is served
// there and the Swagger UI "try it out" button hits real routes.
func Handler(d *specout.Generator, r http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v3/", http.StripPrefix("/api/v3", r))
	page := examplekit.SwaggerPage("specout example — Swagger Petstore", "/api/v3/openapi.json")
	mux.HandleFunc("/", examplekit.Page(page, http.NotFoundHandler()))
	return mux
}

func main() {
	d, r := New()
	if examplekit.EmitSpec(d) {
		return
	}
	fmt.Println("petstore: http://localhost:8081/   spec: http://localhost:8081/api/v3/openapi.json")
	if err := http.ListenAndServe(":8081", Handler(d, r)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
