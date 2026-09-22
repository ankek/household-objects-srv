package middleware

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDPublishesOneIDToBothSides(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestid.FromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("the handler saw no request id in its context")
	}
	if got := rec.Header().Get(requestid.HeaderName); got != seen {
		t.Errorf("%s = %q but the handler saw %q", requestid.HeaderName, got, seen)
	}
}

func TestRequestIDIsFreshPerRequest(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	seen := make(map[string]bool, 64)
	for range 64 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		id := rec.Header().Get(requestid.HeaderName)
		if seen[id] {
			t.Fatalf("id %q was issued twice", id)
		}
		seen[id] = true
	}
}

func TestRequestIDIgnoresTheInboundHeader(t *testing.T) {
	const supplied = "client-chosen-id\nfake log line"

	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestid.FromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestid.HeaderName, supplied)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen == supplied {
		t.Error("the client's X-Request-ID was adopted into the request context")
	}
	if got := rec.Header().Get(requestid.HeaderName); got == supplied {
		t.Error("the client's X-Request-ID was echoed back unchanged")
	}
}

func TestRequestIDSetsTheHeaderBeforeTheHandlerWrites(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("body"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get(requestid.HeaderName) == "" {
		t.Errorf("a handler that wrote its own status lost the %s header", requestid.HeaderName)
	}
}
