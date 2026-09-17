package handlers

import (
	"net/http"

	"github.com/happytoolin/specout"
)

// ImportRequest: the File field makes this multipart/form-data.
type ImportRequest struct {
	File specout.File `form:"file" jsonschema:"description=CSV of onboarding records"`
	Mode string       `form:"mode"  jsonschema:"enum=merge|replace,default=merge"`
}

// HandleImport: NoContent as Res means the 204 is the whole contract.
func HandleImport(d Deps) specout.Handler[ImportRequest, specout.NoContent] {
	return op[ImportRequest, specout.NoContent]("files", "Bulk import from CSV", false, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func HandleReport(d Deps) specout.Handler[struct{}, struct{}] {
	// overrides the struct{} 204 default: this route returns a PDF at 200
	return op[struct{}, struct{}]("files", "Download the activity report", true, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename=report.pdf")
		w.Write([]byte("%PDF-1.4 demo report"))
	}, specout.Response{Status: http.StatusOK, ContentType: "application/pdf"})
}
