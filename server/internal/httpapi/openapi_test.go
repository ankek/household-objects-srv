package httpapi

import (
	"bytes"
	"github.com/ankek/Household-Objects-Dev/server/api"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIRouteServesEmbeddedBytesVerbatim(t *testing.T) {
	h := newTestRouter(t)

	rec := do(t, h, http.MethodGet, "/api/v1/openapi.yaml")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != openapiContentType {
		t.Errorf("Content-Type = %q, want %q", got, openapiContentType)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if !bytes.Equal(rec.Body.Bytes(), api.OpenAPIYAML) {
		t.Fatalf("served body (%d bytes) is not byte-identical to api.OpenAPIYAML (%d bytes)", rec.Body.Len(), len(api.OpenAPIYAML))
	}
}

func TestOpenAPIRouteRejectsUnsupportedMethods(t *testing.T) {
	h := newTestRouter(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			rec := do(t, h, method, "/api/v1/openapi.yaml")

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405: %q", rec.Code, rec.Body.String())
			}
			_ = decodeProblem(t, rec)
		})
	}
}

func TestOpenAPIRouteRevalidatesOnMatchingETag(t *testing.T) {
	h := newTestRouter(t)

	first := do(t, h, http.MethodGet, "/api/v1/openapi.yaml")
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("first response carries no ETag; nothing to revalidate against: %q", first.Header())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304: %q", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 response carries a body (%d bytes); it must be empty", rec.Body.Len())
	}
}
