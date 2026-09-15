package handlers

import (
	"net/http"

	"github.com/happytoolin/specout"
	"github.com/happytoolin/specout/internal/demo/onboarding"
	"github.com/happytoolin/specout/specoutapi"
)

// ImportRequest shows the File convention: multipart/form-data + binary.
type ImportRequest struct {
	File specout.File `form:"file" jsonschema:"description=CSV of onboarding records"`
	Mode string       `form:"mode"  jsonschema:"enum=merge|replace,default=merge"`
}

func HandleImport(d Deps) specout.Handler[ImportRequest, specout.NoContent] {
	return specout.Handler[ImportRequest, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			d.API.NoContent(w)
		},
		Summary: "Bulk import from CSV",
		Tags:    []string{"files"},
	}
}

// Report shows Binary as Res: application/octet-stream, format binary.
func HandleReport(d Deps) specout.Handler[onboarding.Empty, specout.Binary] {
	return specout.Handler[onboarding.Empty, specout.Binary]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			pdf := []byte("%PDF-1.4 demo report")
			specoutapi.SendFile(w, "application/pdf", "report.pdf", pdf)
		},
		Summary: "Download the activity report",
		Tags:    []string{"files"},
	}
}
