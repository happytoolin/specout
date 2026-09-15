package handlers

import (
	"net/http"

	"github.com/happytoolin/specout"
)

// ImportRequest: the File field makes this multipart/form-data.
type ImportRequest struct {
	File specout.File `form:"file" jsonschema:"description=CSV of onboarding records"`
	Mode string `form:"mode"  jsonschema:"enum=merge|replace,default=merge"`
}

// struct{} as Res means "no default body"; the explicit 200 entry below
// carries the real declaration (PDF bytes).
func HandleImport(d Deps) specout.Handler[ImportRequest, specout.NoContent] {
	return specout.Handler[ImportRequest, specout.NoContent]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
		Summary: "Bulk import from CSV",
		Tags:    []string{"files"},
	}
}

func HandleReport(d Deps) specout.Handler[struct{}, struct{}] {
	return specout.Handler[struct{}, struct{}]{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", "attachment; filename=report.pdf")
			w.Write([]byte("%PDF-1.4 demo report"))
		},
		Summary: "Download the activity report",
		Tags:    []string{"files"},
		// overrides the struct{} 204 default: this route returns a PDF at 200
		Responses: []specout.Response{
			{Status: http.StatusOK, ContentType: "application/pdf"},
		},
	}
}
