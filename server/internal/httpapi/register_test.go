package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRegistrar struct {
	result groups.Registered
	err    error
}

func (f fakeRegistrar) Register(context.Context, groups.RegisterRequest) (groups.Registered, error) {
	return f.result, f.err
}

type countingRegistrar struct {
	result groups.Registered
	err    error
	calls  atomic.Int64
}

func (f *countingRegistrar) Register(context.Context, groups.RegisterRequest) (groups.Registered, error) {
	f.calls.Add(1)
	return f.result, f.err
}

func postJSON(t *testing.T, h http.Handler, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	return rec
}

func TestRegisterRouteUnmountedWithoutARegistrar(t *testing.T) {
	h := newTestRouter(t)
	rec := postJSON(t, h, "/api/v1/auth/register", []byte(`{}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; a nil Registrar must leave the route unmounted", rec.Code)
	}
}

func TestRegisterRouteIsUnauthenticated(t *testing.T) {
	cfg := testConfig()
	cfg.Registrar = fakeRegistrar{result: groups.Registered{GroupID: "g1", UserID: "u1", Username: "alice"}}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/register",
		[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 with no credential presented: %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterRouteHappyPath(t *testing.T) {
	cfg := testConfig()
	cfg.Registrar = fakeRegistrar{result: groups.Registered{GroupID: "g1", UserID: "u1", Username: "alice"}}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/register",
		[]byte(`{"username":"alice","password":"correct horse battery staple"}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var got registerResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	want := registerResponseBody{GroupID: "g1", UserID: "u1", Username: "alice"}
	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}

	if bytes.Contains(rec.Body.Bytes(), []byte("password")) {
		t.Error("response body mentions \"password\"; the response must never carry a hash or the submitted password")
	}
}

func TestRegisterRouteMalformedJSON(t *testing.T) {
	cfg := testConfig()
	cfg.Registrar = fakeRegistrar{result: groups.Registered{GroupID: "g1", UserID: "u1", Username: "alice"}}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/register", []byte(`{not json`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Detail == "" {
		t.Error("a malformed body the caller itself sent should carry a detail; problem.BadRequest exists for exactly this")
	}
}

func TestRegisterRouteValidationErrorIsBadRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"invalid username", groups.ErrUsernameInvalid},
		{"invalid password", groups.ErrPasswordInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Registrar = fakeRegistrar{err: tc.err}
			h, err := NewRouter(cfg)
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}

			rec := postJSON(t, h, "/api/v1/auth/register",
				[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRegisterRouteClosedIsConflict(t *testing.T) {
	cfg := testConfig()
	cfg.Registrar = fakeRegistrar{err: groups.ErrRegistrationClosed}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/register",
		[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Detail != "" {
		t.Errorf("Conflict carries a detail (%q); registration-closed must not describe server-global state to an unauthenticated caller", p.Detail)
	}
}

func TestRegisterRouteUnexpectedErrorIsInternal(t *testing.T) {
	cfg := testConfig()
	cfg.Registrar = fakeRegistrar{err: errors.New("boom: something only an operator should see")}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/register",
		[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("boom")) {
		t.Error("the internal error's message reached the response body")
	}
}

func TestRegisterRateLimitBlocksAfterFreeAttemptsAreExhausted(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	registrar := &countingRegistrar{result: groups.Registered{GroupID: "g1", UserID: "u1", Username: "alice"}}
	cfg := testConfig()
	cfg.Registrar = registrar
	cfg.RegisterRateLimiter = ratelimit.New(ratelimit.Config{
		Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
	})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for i := 0; i < 2; i++ {
		rec := postJSON(t, h, "/api/v1/auth/register",
			[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("attempt %d: status = %d, want 201: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if got := registrar.calls.Load(); got != 2 {
		t.Fatalf("Registrar.Register call count = %d, want 2", got)
	}

	rec := postJSON(t, h, "/api/v1/auth/register",
		[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd attempt: status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if got := registrar.calls.Load(); got != 2 {
		t.Errorf("Registrar.Register call count = %d after the blocked attempt, want still 2 -- a blocked attempt must never reach the hasher", got)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("429 response carries no Retry-After header")
	} else if seconds, err := strconv.Atoi(ra); err != nil || seconds < 1 {
		t.Errorf("Retry-After = %q, want a positive whole number of seconds", ra)
	}
}

func TestRegisterRateLimitEscalatesOnSuccessToo(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	registrar := &countingRegistrar{result: groups.Registered{GroupID: "g1", UserID: "u1", Username: "alice"}}
	cfg := testConfig()
	cfg.Registrar = registrar
	cfg.RegisterRateLimiter = ratelimit.New(ratelimit.Config{
		Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
	})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	var codes []int
	for i := 0; i < 3; i++ {
		rec := postJSON(t, h, "/api/v1/auth/register",
			[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
		codes = append(codes, rec.Code)
	}
	want := []int{http.StatusCreated, http.StatusCreated, http.StatusTooManyRequests}
	for i := range want {
		if codes[i] != want[i] {
			t.Errorf("attempt %d: status = %d, want %d (sequence: %v)", i+1, codes[i], want[i], codes)
		}
	}
}

func TestRegisterRateLimitDoesNotEscalateOnValidationFailure(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	cfg := testConfig()
	cfg.Registrar = fakeRegistrar{err: groups.ErrPasswordInvalid}
	cfg.RegisterRateLimiter = ratelimit.New(ratelimit.Config{
		Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
	})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for i := 0; i < 5; i++ {
		rec := postJSON(t, h, "/api/v1/auth/register", []byte(`{"username":"alice","password":"short"}`))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: status = %d, want 400 every time (validation failures must never be throttled): %s", i+1, rec.Code, rec.Body.String())
		}
	}
}
