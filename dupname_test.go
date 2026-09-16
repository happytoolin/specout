package specout_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/happytoolin/specout"
)

type dupA struct{ A string }
type dupB struct{ B int }

func TestDuplicateComponentNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate-name panic")
		}
	}()
	d := specout.New(specout.Config{Title: "t", Version: "1"})
	d.SchemaName[dupA]("Clash")
	d.SchemaName[dupB]("Clash")
	r := chi.NewRouter()
	specout.Chi(d, r).Get("/a", specout.Handler[struct{}, dupA]{HandlerFunc: func(http.ResponseWriter, *http.Request) {}})
	specout.Chi(d, r).Get("/b", specout.Handler[struct{}, dupB]{HandlerFunc: func(http.ResponseWriter, *http.Request) {}})
	serveDoc(t, d, r)
}
