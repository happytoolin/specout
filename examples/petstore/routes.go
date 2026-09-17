package main

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/happytoolin/specout"
)

// op builds one operation: its published metadata, the two fields the
// operations vary on, and its http body. Every published operation ends with
// the same "Unexpected error" default, so op adds it.
func op[Req, Res any](id, summary, description string, tags []string, public bool, requestTypes []string, fn http.HandlerFunc, responses ...specout.Response) specout.Handler[Req, Res] {
	return specout.Handler[Req, Res]{
		OperationID: id, Summary: summary, Description: description, Tags: tags, Public: public,
		RequestContentTypes: requestTypes, HandlerFunc: fn, Responses: append(responses, unexpected),
	}
}

// petRoutes registers the eight pet operations.
func petRoutes(b *specout.ChiRouter, s *store) {
	petCollectionRoutes(b, s)
	petByIDRoutes(b, s)
}

// petCollectionRoutes registers the four collection-level pet operations.
func petCollectionRoutes(b *specout.ChiRouter, s *store) {
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
}

// petByIDRoutes registers the four /pet/{petId} operations.
func petByIDRoutes(b *specout.ChiRouter, s *store) {
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
			w.WriteHeader(http.StatusOK)
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
			_ = f.Close()
			writeJSON(w, 200, ApiResponse{Code: 200, Type: "ok", Message: "file uploaded"})
		},
		okJSON("successful operation"), desc(400, "No file uploaded"), petNotFound))
}

// storeRoutes registers the four store operations.
func storeRoutes(b *specout.ChiRouter, s *store) {
	b.Get("/store/inventory", op[struct{}, map[string]int32]("getInventory", "Returns pet inventories by status.",
		"Returns a map of status codes to quantities.", tagStore, false, nil,
		func(w http.ResponseWriter, _ *http.Request) {
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
		specout.Response{
			Status: 200, ContentTypes: jsonOrXML, Raw: map[string]any{"description": "successful operation"},
			Headers: []specout.Header{
				{Name: "X-Rate-Limit", Type: int32(0)},
				{Name: "X-Expires-After", Type: time.Time{}},
			},
		},
		desc(400, "Invalid username/password supplied")))
	b.Get("/user/logout", op[struct{}, struct{}]("logoutUser", "Logs out current logged in user session.", "Log user out of the system.", tagUser, true, nil,
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
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
				w.WriteHeader(http.StatusOK)
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
