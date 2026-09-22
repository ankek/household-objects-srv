package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeLoginService struct {
	result session.LoggedIn
	err    error
}

func (f fakeLoginService) Login(context.Context, session.LoginRequest) (session.LoggedIn, error) {
	return f.result, f.err
}

type fakeRateLimitClock struct{ now time.Time }

func (c *fakeRateLimitClock) Now() time.Time          { return c.now }
func (c *fakeRateLimitClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

type accountAwareLoginService struct {
	validUsername string
	validPassword string
	calls         atomic.Int64
}

func (f *accountAwareLoginService) Login(_ context.Context, req session.LoginRequest) (session.LoggedIn, error) {
	f.calls.Add(1)
	if req.Username == f.validUsername && string(req.Password) == f.validPassword {
		return session.LoggedIn{
			GroupID: "g1", UserID: "u1", Username: req.Username, Role: "owner",
			Token: "tok", ExpiresAt: time.Now().Add(time.Hour),
		}, nil
	}
	return session.LoggedIn{}, session.ErrInvalidCredentials
}

func loginBody(username, password string) []byte {
	body, err := json.Marshal(loginRequestBody{Username: username, Password: password})
	if err != nil {
		panic(err)
	}
	return body
}

func postJSONFromIP(t *testing.T, h http.Handler, path string, body []byte, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginRateLimitBlocksAfterFreeAttemptsAreExhausted(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	svc := &accountAwareLoginService{validUsername: "alice", validPassword: "correct horse battery staple"}
	cfg := testConfig()
	cfg.LoginService = svc
	cfg.LoginRateLimiter = ratelimit.New(ratelimit.Config{
		Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
	})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for i := 0; i < 2; i++ {
		rec := postJSON(t, h, "/api/v1/auth/login", loginBody("alice", "wrong password"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if got := svc.calls.Load(); got != 2 {
		t.Fatalf("LoginService.Login call count = %d, want 2 after two answered attempts", got)
	}

	rec := postJSON(t, h, "/api/v1/auth/login", loginBody("alice", "wrong password"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd attempt: status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if got := svc.calls.Load(); got != 2 {
		t.Errorf("LoginService.Login call count = %d after the blocked attempt, want still 2 -- a blocked attempt must never reach the hasher", got)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("429 response carries no Retry-After header")
	} else if seconds, err := strconv.Atoi(ra); err != nil || seconds < 1 {
		t.Errorf("Retry-After = %q, want a positive whole number of seconds", ra)
	}
	p := decodeProblem(t, rec)
	if p.Detail != "" {
		t.Errorf("429 carries a detail (%q); it must not reveal which bucket (account or IP) tripped", p.Detail)
	}

	clock.Advance(2 * time.Second)
	rec = postJSON(t, h, "/api/v1/auth/login", loginBody("alice", "correct horse battery staple"))
	if rec.Code != http.StatusOK {
		t.Fatalf("attempt after the backoff window with the correct password: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestLoginRateLimitBlocksRealAndUnknownUsernamesIdentically(t *testing.T) {
	for _, username := range []string{"alice", "this-username-was-never-registered"} {
		t.Run(username, func(t *testing.T) {
			clock := &fakeRateLimitClock{now: time.Now()}
			svc := &accountAwareLoginService{validUsername: "alice", validPassword: "correct horse battery staple"}
			cfg := testConfig()
			cfg.LoginService = svc
			cfg.LoginRateLimiter = ratelimit.New(ratelimit.Config{
				Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
			})
			h, err := NewRouter(cfg)
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}

			var codes []int
			for i := 0; i < 3; i++ {
				addr := fmt.Sprintf("203.0.113.%d:1234", i+1)
				rec := postJSONFromIP(t, h, "/api/v1/auth/login", loginBody(username, "wrong password"), addr)
				codes = append(codes, rec.Code)
			}
			want := []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests}
			for i := range want {
				if codes[i] != want[i] {
					t.Errorf("username=%q attempt %d (from a fresh source IP each time): status = %d, want %d (sequence: %v)", username, i+1, codes[i], want[i], codes)
				}
			}
		})
	}
}

func TestLoginRouteUnmountedWithoutALoginService(t *testing.T) {
	h := newTestRouter(t)
	rec := postJSON(t, h, "/api/v1/auth/login", []byte(`{}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; a nil LoginService must leave the route unmounted", rec.Code)
	}
}

func TestLoginRouteIsUnauthenticated(t *testing.T) {
	cfg := testConfig()
	cfg.LoginService = fakeLoginService{result: session.LoggedIn{
		GroupID: "g1", UserID: "u1", Username: "alice", Role: "owner",
		Token: "tok", ExpiresAt: time.Now().Add(time.Hour),
	}}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/login",
		[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no credential presented: %s", rec.Code, rec.Body.String())
	}
}

func TestLoginRouteHappyPath(t *testing.T) {
	expiresAt := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	cfg := testConfig()
	cfg.LoginService = fakeLoginService{result: session.LoggedIn{
		GroupID: "g1", UserID: "u1", Username: "alice", Role: "owner",
		Token: "the-raw-token", ExpiresAt: expiresAt,
	}}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/login",
		[]byte(`{"username":"alice","password":"correct horse battery staple"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var got loginResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	want := loginResponseBody{GroupID: "g1", UserID: "u1", Username: "alice", Role: "owner"}
	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("the-raw-token")) {
		t.Error("response body carries the raw token; it must travel only in Set-Cookie, HttpOnly")
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1: %v", len(cookies), rec.Header().Values("Set-Cookie"))
	}
	c := cookies[0]
	if c.Name != session.CookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, session.CookieName)
	}
	if c.Value != "the-raw-token" {
		t.Errorf("cookie value = %q, want the raw token", c.Value)
	}
	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly (NFR-013)")
	}
	if !c.Secure {
		t.Error("cookie is not Secure (NFR-013)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v, want Lax (NFR-013)", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("cookie Path = %q, want %q", c.Path, "/")
	}
	if !c.Expires.Equal(expiresAt) {
		t.Errorf("cookie Expires = %v, want %v", c.Expires, expiresAt)
	}
}

func TestLoginRouteMalformedJSON(t *testing.T) {
	cfg := testConfig()
	cfg.LoginService = fakeLoginService{}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/login", []byte(`{not json`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestLoginRouteInvalidCredentialsIsUnauthorized(t *testing.T) {
	cfg := testConfig()
	cfg.LoginService = fakeLoginService{err: session.ErrInvalidCredentials}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/login",
		[]byte(`{"username":"alice","password":"wrong"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Detail != "" {
		t.Errorf("Unauthorized carries a detail (%q); wrong-username and wrong-password must be indistinguishable", p.Detail)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a failed login set a cookie; it must not")
	}
}

func TestLoginRouteMalformedHashIsUnauthorizedButLogged(t *testing.T) {
	var logs bytes.Buffer
	cfg := testConfig()
	cfg.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	cfg.LoginService = fakeLoginService{err: errors.Join(session.ErrInvalidCredentials, auth.ErrMalformedHash)}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/login",
		[]byte(`{"username":"alice","password":"whatever"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "malformed") {
		t.Errorf("log output does not mention the malformed hash, want an operator-visible warning: %q", logs.String())
	}
}

func TestLoginRouteUnexpectedErrorIsInternal(t *testing.T) {
	cfg := testConfig()
	cfg.LoginService = fakeLoginService{err: errors.New("boom: something only an operator should see")}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/auth/login",
		[]byte(`{"username":"alice","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("boom")) {
		t.Error("the internal error's message reached the response body")
	}
}
