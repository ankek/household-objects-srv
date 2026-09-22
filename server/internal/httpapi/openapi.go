package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/ankek/Household-Objects-Dev/server/api"
	"net/http"
)

const openapiContentType = "application/yaml; charset=utf-8"

var openapiETag = func() string {
	sum := sha256.Sum256(api.OpenAPIYAML)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}()

func openapiHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", openapiContentType)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("ETag", openapiETag)
		h.Set("Cache-Control", "public, max-age=300, must-revalidate")

		if r.Header.Get("If-None-Match") == openapiETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(api.OpenAPIYAML)
	}
}
