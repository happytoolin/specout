package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchErrorBodiesMatchContract(t *testing.T) {
	_, api := New()
	for _, tc := range []struct {
		name, method, path, body, token string
		code                            int
	}{
		{"missing record", http.MethodGet, "/onboarding/missing", "", "", http.StatusNotFound},
		{"missing credentials", http.MethodDelete, "/onboarding/onb_4f9x", "", "", http.StatusUnauthorized},
		{"invalid JSON", http.MethodPost, "/onboarding", "{", "Bearer test", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", tc.token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			assert.Equal(t, tc.code, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
			var body problem
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.code, body.Status)
			assert.NotEmpty(t, body.Title)
		})
	}
}

func TestLaunchOnboardingRejectsInvalidStage(t *testing.T) {
	_, api := New()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/onboarding",
		strings.NewReader(`{"owner":"owner@example.com","stage":"not-a-declared-stage"}`))
	r.Header.Set("Authorization", "Bearer test")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "stage must satisfy the published enum")
}

func TestLaunchWeirdTrailingJSONDoesNotMutateStore(t *testing.T) {
	const valid = `{"owner":"changed@example.com","stage":"active"}`
	for _, suffix := range []string{`{}`, `null`, `true`, ` garbage`, "\x00", ` [1,2,3]`} {
		t.Run(suffix, func(t *testing.T) {
			_, api := New()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/onboarding/onb_4f9x", strings.NewReader(valid+suffix))
			r.Header.Set("Authorization", "Bearer test")
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			read := httptest.NewRecorder()
			api.ServeHTTP(read, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/onboarding/onb_4f9x", nil))
			var item onboarding
			require.NoError(t, json.Unmarshal(read.Body.Bytes(), &item))
			assert.Equal(t, "owner@example.com", item.Owner, "rejected input must not change stored data")
			assert.Equal(t, "draft", item.Stage)
		})
	}
}

func TestLaunchWeirdOwnerAddresses(t *testing.T) {
	for _, owner := range []string{"@", "a@", "a@@example.com", "Alice <alice@example.com>", "a@example.com\nBcc: b@example.com"} {
		t.Run(owner, func(t *testing.T) {
			_, api := New()
			body, err := json.Marshal(upsertRequest{Owner: owner, Stage: "draft"})
			require.NoError(t, err)
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/onboarding", strings.NewReader(string(body)))
			r.Header.Set("Authorization", "Bearer test")
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "owner must be one plain email address")
		})
	}
}

func TestLaunchWeirdEmptyCredentials(t *testing.T) {
	for _, credentials := range []struct{ bearer, apiKey string }{
		{bearer: "Bearer "}, {bearer: "Bearer \t "}, {apiKey: "   "},
	} {
		_, api := New()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/onboarding/onb_4f9x", nil)
		r.Header.Set("Authorization", credentials.bearer)
		r.Header.Set("X-API-Key", credentials.apiKey)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "whitespace is not a credential")
	}
}
