package demoapp

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/specoutapi"
	"github.com/invopop/jsonschema"
)

// ---- shared types ----

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type ValidationError struct {
	Problems []FieldProblem `json:"problems"`
}
type FieldProblem struct {
	Field   string `json:"field"   jsonschema:"example=owner"`
	Message string `json:"message" jsonschema:"example=must be a valid email"`
}

// Onboarding serves both read and write directions (readOnly/writeOnly).
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

type Empty struct{}

type ListOnboardingRequest struct {
	Limit  int    `query:"limit"  jsonschema:"default=20,minimum=1,maximum=100"`
	Cursor string `query:"cursor" jsonschema:"description=Opaque cursor from a previous page"`
	Sort   string `query:"sort"   jsonschema:"enum=created|updated,default=created"`
}

type Page struct {
	Items []Onboarding `json:"items"`
	Next  string       `json:"next,omitempty" jsonschema:"description=Cursor for next page, empty on last"`
}

type SyncConflict struct {
	Resource   string `json:"resource"   jsonschema:"example=onboarding"`
	Expected   int    `json:"expected"`
	Actual     int    `json:"actual"`
	ResolveURL string `json:"resolveUrl" jsonschema:"description=Fetch current state here"`
}
type SyncRequest struct{}
type SyncResult struct {
	Count int `json:"count"`
}

type ImportRequest struct {
	File specout.File `form:"file" jsonschema:"description=CSV of onboardings"`
	Mode string       `form:"mode"  jsonschema:"enum=merge|replace,default=merge"`
}

// Money is a self-describing custom scalar.
type Money struct{ Cents int64 }

func (Money) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "string",
		Pattern:  `^-?\d+\.\d{2}$`,
		Examples: []any{"19.99"},
	}
}

// ---- union variants ----

type EmailConfig struct {
	Address string `json:"address" jsonschema:"format=email"`
}
type SlackConfig struct {
	Channel string `json:"channel"`
}
type WebhookConfig struct {
	Kind specout.Variant `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any             `json:"data" jsonschema:"oneof_type=email|slack"`
}

// New wires the full demo: every route shape in the api-reference.
func New() (*specout.Generator, http.Handler, *specoutapi.Responder) {
	api := specoutapi.New(specoutapi.Config{
		ErrorMapper: func(err error) (int, specoutapi.Problem) {
			return 500, specoutapi.Problem{Type: "internal", Title: "Internal error"}
		},
	})

	d := specout.New(specout.Config{
		Title:         "specout demo",
		Version:       "1.0.0",
		Description:   "Every route shape the library supports, live.",
		ErrorType:     Problem{},
		DefaultErrors: []int{400, 401, 403, 404, 409, 500},
		Tags: []specout.Tag{
			{Name: "onboarding", Description: "Onboarding lifecycle"},
			{Name: "sync", Description: "Version sync"},
			{Name: "files", Description: "Upload and download"},
		},
		Servers: []specout.Server{{URL: "http://localhost:8080", Description: "local"}},
	})

	d.Register[EmailConfig]("email")
	d.Register[SlackConfig]("slack")

	r := chi.NewRouter()
	r.Use(middleware.Logger)

	r.Route("/onboarding", func(r chi.Router) {
		d.Get(r, "/", specout.Handler[ListOnboardingRequest, Page]{
			HandlerFunc: handleList(api), Summary: "List onboarding", Tags: []string{"onboarding"},
		})
		d.Post(r, "/", specout.Handler[UpsertRequest, Onboarding]{
			HandlerFunc: handleCreate(api), Summary: "Create onboarding", Tags: []string{"onboarding"},
			Responses: []specout.Response{{Status: http.StatusCreated, Headers: []specout.Header{{Name: "Location"}}}},
		})
		r.Route("/{id}", func(r chi.Router) {
			d.Get(r, "/", specout.Handler[Empty, Onboarding]{
				HandlerFunc: handleGet(api), Summary: "Get one", Tags: []string{"onboarding"},
			})
			d.Put(r, "/", specout.Handler[UpsertRequest, Onboarding]{
				HandlerFunc: handleUpsert(api), Summary: "Upsert", Tags: []string{"onboarding"},
				Responses: []specout.Response{
					{Status: http.StatusCreated},
					{Status: http.StatusUnprocessableEntity, Type: ValidationError{}},
				},
			})
			d.Delete(r, "/", specout.Handler[Empty, specout.NoContent]{
				HandlerFunc: handleDelete(api), Summary: "Delete", Tags: []string{"onboarding"},
			})
			d.Post(r, "/sync", specout.Handler[SyncRequest, SyncResult]{
				HandlerFunc: handleSync(api), Summary: "Sync with conflict detail", Tags: []string{"sync"},
				Responses: []specout.Response{
					{Status: 409, Type: SyncConflict{}},
					{Status: 422, Type: ValidationError{}},
					{Status: 401, Omit: true},
				},
			})
		})
	})

	d.Post(r, "/files/import", specout.Handler[ImportRequest, specout.NoContent]{
		HandlerFunc: handleImport(api), Summary: "Import CSV", Tags: []string{"files"},
	})
	d.Get(r, "/files/report", specout.Handler[Empty, specout.Binary]{
		HandlerFunc: handleReport(api), Summary: "Download report", Tags: []string{"files"},
	})
	d.Get(r, "/legacy", specout.Handler[Empty, Onboarding]{
		HandlerFunc: handleGet(api), Summary: "Replaced by PUT /onboarding/{id}", Deprecated: true, Tags: []string{"onboarding"},
	})
	d.Post(r, "/webhooks", specout.Handler[WebhookConfig, WebhookConfig]{
		HandlerFunc: handleWebhook(api), Summary: "Register webhook (union body)", Tags: []string{"sync"},
	})

	r.Mount("/openapi.json", d)
	if err := d.Adopt(r); err != nil {
		panic(err)
	}
	return d, r, api
}

// handlers are thin: real behavior is not the point of the demo.
func handleList(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := specoutapi.Decode[ListOnboardingRequest](r)
		if err != nil {
			api.Err(w, r, err)
			return
		}
		api.OK(w, Page{Items: []Onboarding{sampleOnboarding()}, Next: ""})
	}
}

func handleCreate(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := specoutapi.Decode[UpsertRequest](r)
		_ = req
		if err != nil {
			api.Err(w, r, err)
			return
		}
		if r.URL.Query().Get("existing") != "" {
			api.OK(w, sampleOnboarding())
			return
		}
		w.Header().Set("Location", "/onboarding/onb_new")
		api.Status(w, http.StatusCreated, sampleOnboarding())
	}
}

func handleGet(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { api.OK(w, sampleOnboarding()) }
}

func handleUpsert(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := specoutapi.Decode[UpsertRequest](r)
		_ = req
		if err != nil {
			api.Err(w, r, err)
			return
		}
		if req.Owner == "invalid" || r.URL.Query().Get("invalid") != "" {
			api.Status(w, http.StatusUnprocessableEntity, ValidationError{Problems: []FieldProblem{{Field: "owner", Message: "must be a valid email"}}})
			return
		}
		if r.URL.Query().Get("created") == "1" {
			w.Header().Set("Location", "/onboarding/onb_new")
			api.Status(w, http.StatusCreated, sampleOnboarding())
			return
		}
		api.OK(w, sampleOnboarding())
	}
}

func handleDelete(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { api.NoContent(w) }
}

func handleSync(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "conflict":
			api.Status(w, 409, SyncConflict{Resource: "onboarding", Expected: 3, Actual: 4, ResolveURL: "/onboarding/onb_1"})
		case "invalid":
			api.Status(w, http.StatusUnprocessableEntity, ValidationError{Problems: []FieldProblem{{Field: "version", Message: "out of date"}}})
		default:
			api.OK(w, SyncResult{Count: 1})
		}
	}
}

func handleImport(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { api.NoContent(w) }
}

func handleReport(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specoutapi.SendFile(w, "application/pdf", "report.pdf", []byte("%PDF-demo"))
	}
}

func handleWebhook(api *specoutapi.Responder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		api.OK(w, WebhookConfig{Kind: "email", Data: EmailConfig{Address: "ops@example.com"}})
	}
}

func sampleOnboarding() Onboarding {
	return Onboarding{
		ID: "onb_4f9x", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Owner: "owner@example.com", Stage: "draft",
	}
}
