package main

import "time"

type problem struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
}

type fieldProblem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type validationError struct {
	Problems []fieldProblem `json:"problems"`
}

type onboarding struct {
	ID        string    `json:"id"        jsonschema:"readonly,example=onb_4f9x"`
	CreatedAt time.Time `json:"createdAt" jsonschema:"readonly"`
	Owner     string    `json:"owner"     jsonschema:"format=email"`
	Stage     string    `json:"stage"     jsonschema:"enum=draft|active|archived,default=draft"`
}

type upsertRequest struct {
	Owner string `json:"owner" jsonschema:"format=email"`
	Stage string `json:"stage" jsonschema:"enum=draft|active|archived,default=draft"`
}

type listRequest struct {
	Limit int    `jsonschema:"default=20,minimum=1,maximum=100"     query:"limit"`
	Sort  string `jsonschema:"enum=created|updated,default=created" query:"sort"`
	Trace string `header:"X-Trace-Id"                               jsonschema:"description=Client trace id"`
}

type page struct {
	Items []onboarding `json:"items"`
}

type emailWebhook struct {
	Address string `json:"address" jsonschema:"format=email"`
}

type slackWebhook struct {
	Channel string `json:"channel"`
}

type webhook struct {
	Kind string `json:"kind" jsonschema:"enum=email|slack,description=Discriminator"`
	Data any    `json:"data" jsonschema:"oneof_type=email|slack"`
}
