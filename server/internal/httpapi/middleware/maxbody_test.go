package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBodyRefusesADeclaredOversizeBodyBeforeReadingIt(t *testing.T) {
	const limit = 64

	reached := false
	h := MaxBody(limit)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", limit+1)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if reached {
		t.Error("the handler ran; the point of the cap is that an oversize body never reaches one")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %q", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("body status = %d, want 413", p.Status)
	}
	if !strings.HasSuffix(p.Type, "content-too-large") {
		t.Errorf("problem type = %q; a client must be able to tell this from any other refusal", p.Type)
	}
}

func TestMaxBodyAdmitsABodyAtTheLimit(t *testing.T) {
	const limit = 64
	body := strings.Repeat("x", limit)

	var got string
	h := MaxBody(limit)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading a body exactly at the limit failed: %v", err)
		}
		got = string(b)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))

	if got != body {
		t.Errorf("the handler read %d bytes, want %d", len(got), len(body))
	}
}

func TestMaxBodyCapsAnUndeclaredBody(t *testing.T) {
	const limit = 64

	var readErr error
	h := MaxBody(limit)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader(strings.Repeat("x", limit*4))))
	if req.ContentLength != -1 {
		t.Fatalf("ContentLength = %d, want -1; this test is not exercising the undeclared-length path", req.ContentLength)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)

	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("reading an oversize undeclared body returned %v, want *http.MaxBytesError", readErr)
	}
	if maxErr.Limit != limit {
		t.Errorf("MaxBytesError.Limit = %d, want %d", maxErr.Limit, limit)
	}
}

func TestMaxBodyDefaultsWhenUnconfigured(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		reached := false
		h := MaxBody(limit)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

		body := strings.Repeat("x", int(DefaultMaxBodyBytes)/2)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))

		if !reached || rec.Code != http.StatusOK {
			t.Errorf("MaxBody(%d) refused a body of %d bytes with status %d; a non-positive limit means the default, not zero", limit, len(body), rec.Code)
		}
	}
}

func TestMaxBodyIgnoresAMissingBody(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h := MaxBody(1)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))

		if rec.Code != http.StatusNoContent {
			t.Errorf("%s with no body was answered %d under a 1-byte cap", method, rec.Code)
		}
	}
}
