package specoutapi

import (
	"fmt"
	"net/http"
	"time"
)

// SendFile writes a binary payload with download semantics.
func SendFile(w http.ResponseWriter, contentType, filename string, b []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(b)))
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.Write(b)
}
