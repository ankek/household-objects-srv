package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type fakeInviteRedeemer struct {
	result     invite.Redeemed
	err        error
	calls      atomic.Int64
	gotRequest invite.RedeemRequest
}

func (f *fakeInviteRedeemer) Redeem(_ context.Context, req invite.RedeemRequest) (invite.Redeemed, error) {
	f.calls.Add(1)
	f.gotRequest = req
	return f.result, f.err
}

var _ InviteRedeemer = (*fakeInviteRedeemer)(nil)

func TestInviteRedeemRouteUnmountedWithoutARedeemer(t *testing.T) {
	h := newTestRouter(t)
	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"u","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route not mounted)", rec.Code)
	}
}

func TestInviteRedeemRouteIsUnauthenticated(t *testing.T) {
	redeemer := &fakeInviteRedeemer{result: invite.Redeemed{GroupID: "g1", UserID: "u1", Username: "newmember"}}
	cfg := testConfig()
	cfg.InviteRedeemer = redeemer
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"newmember","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 with no credential presented at all: %s", rec.Code, rec.Body.String())
	}
}

func TestInviteRedeemHandlerHappyPath(t *testing.T) {
	redeemer := &fakeInviteRedeemer{result: invite.Redeemed{GroupID: "grp-1", UserID: "usr-1", Username: "newmember"}}
	cfg := testConfig()
	cfg.InviteRedeemer = redeemer
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"raw-token","username":"newmember","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var got inviteRedeemResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if got.GroupID != "grp-1" || got.UserID != "usr-1" || got.Username != "newmember" {
		t.Errorf("response = %+v, want {GroupID: grp-1, UserID: usr-1, Username: newmember}", got)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("raw-token")) || bytes.Contains(rec.Body.Bytes(), []byte("correct horse")) {
		t.Error("response body echoes the presented token or password")
	}
	if redeemer.gotRequest.Token != "raw-token" || redeemer.gotRequest.Username != "newmember" {
		t.Errorf("Redeem called with %+v, want the request body's own token/username", redeemer.gotRequest)
	}
}

func TestInviteRedeemHandlerMalformedJSON(t *testing.T) {
	cfg := testConfig()
	cfg.InviteRedeemer = &fakeInviteRedeemer{}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`not json`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestInviteRedeemHandlerValidationErrorIsBadRequest(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"empty token", invite.ErrTokenRequired},
		{"invalid username", invite.ErrUsernameInvalid},
		{"invalid password", invite.ErrPasswordInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.InviteRedeemer = &fakeInviteRedeemer{err: tc.err}
			h, err := NewRouter(cfg)
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"u","password":"correct horse battery staple"}`))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestInviteRedeemHandlerNotRedeemableIsNotFoundWithNoDetail(t *testing.T) {
	cfg := testConfig()
	cfg.InviteRedeemer = &fakeInviteRedeemer{err: invite.ErrNotRedeemable}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"u","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Detail != "" {
		t.Errorf("NotFound carries a detail (%q); an unredeemable token must not describe why", p.Detail)
	}
}

func TestInviteRedeemHandlerUsernameTakenIsConflict(t *testing.T) {
	cfg := testConfig()
	cfg.InviteRedeemer = &fakeInviteRedeemer{err: invite.ErrUsernameTaken}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"taken","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestInviteRedeemHandlerUnexpectedErrorIsInternal(t *testing.T) {
	cfg := testConfig()
	cfg.InviteRedeemer = &fakeInviteRedeemer{err: errors.New("boom: only an operator should see this")}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"u","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("boom")) {
		t.Error("the internal error's message reached the response body")
	}
}

func TestInviteRedeemSucceedsWithRegistrationClosed(t *testing.T) {
	cfg := testConfig()
	cfg.Registrar = fakeRegistrar{err: groups.ErrRegistrationClosed}
	cfg.InviteRedeemer = &fakeInviteRedeemer{result: invite.Redeemed{GroupID: "g1", UserID: "u1", Username: "newmember"}}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	regRec := postJSON(t, h, "/api/v1/auth/register", []byte(`{"username":"x","password":"correct horse battery staple"}`))
	if regRec.Code != http.StatusConflict {
		t.Fatalf("precondition: /auth/register status = %d, want 409 (closed)", regRec.Code)
	}

	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"newmember","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 -- invite redemption must not be gated by FR-005's registration-closed toggle: %s", rec.Code, rec.Body.String())
	}
}

func TestPublicInviteRedeemRouteDoesNotConflictWithOwnerOnlyInviteRoutes(t *testing.T) {
	cfg := invitesConfig(&fakeInviteIssuer{}, &fakeInviteAdmin{})
	cfg.InviteRedeemer = &fakeInviteRedeemer{result: invite.Redeemed{GroupID: "g1", UserID: "u1", Username: "newmember"}}
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	redeemRec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"newmember","password":"correct horse battery staple"}`))
	if redeemRec.Code != http.StatusCreated {
		t.Errorf("POST /invites/redeem: status = %d, want 201: %s", redeemRec.Code, redeemRec.Body.String())
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/invites/some-unknown-id", nil), "owner")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE /invites/{unknown}: status = %d, want 404", rec.Code)
	}
}

func TestInviteRedeemRateLimitBlocksAfterFreeAttemptsAreExhausted(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	redeemer := &fakeInviteRedeemer{result: invite.Redeemed{GroupID: "g1", UserID: "u1", Username: "newmember"}}
	cfg := testConfig()
	cfg.InviteRedeemer = redeemer
	cfg.InviteRedeemRateLimiter = ratelimit.New(ratelimit.Config{
		Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
	})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for i := 0; i < 2; i++ {
		rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"newmember","password":"correct horse battery staple"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("attempt %d: status = %d, want 201: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if got := redeemer.calls.Load(); got != 2 {
		t.Fatalf("Redeem call count = %d, want 2", got)
	}

	rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"t","username":"newmember","password":"correct horse battery staple"}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd attempt: status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if got := redeemer.calls.Load(); got != 2 {
		t.Errorf("Redeem call count = %d after the blocked attempt, want still 2 -- a blocked attempt must never reach the hasher", got)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("429 response carries no Retry-After header")
	} else if seconds, err := strconv.Atoi(ra); err != nil || seconds < 1 {
		t.Errorf("Retry-After = %q, want a positive whole number of seconds", ra)
	}
}

func TestInviteRedeemRateLimitBlocksByTokenEvenFromDifferentIPs(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	redeemer := &fakeInviteRedeemer{err: invite.ErrNotRedeemable}
	cfg := testConfig()
	cfg.InviteRedeemer = redeemer
	cfg.InviteRedeemRateLimiter = ratelimit.New(ratelimit.Config{
		Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
	})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	postFrom := func(ip string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/invites/redeem",
			bytes.NewReader([]byte(`{"token":"same-token-every-time","username":"u","password":"correct horse battery staple"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":12345"
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := postFrom("203.0.113.1"); rec.Code != http.StatusNotFound {
		t.Fatalf("attempt 1 (ip 1): status = %d, want 404", rec.Code)
	}
	if rec := postFrom("203.0.113.2"); rec.Code != http.StatusNotFound {
		t.Fatalf("attempt 2 (ip 2): status = %d, want 404", rec.Code)
	}
	rec := postFrom("203.0.113.3")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 3 (ip 3, same token): status = %d, want 429 (per-token bucket must have tripped even though every IP is fresh)", rec.Code)
	}
}

func TestInviteRedeemRateLimitDoesNotEscalateOnValidationFailure(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	cfg := testConfig()
	cfg.InviteRedeemer = &fakeInviteRedeemer{err: invite.ErrTokenRequired}
	cfg.InviteRedeemRateLimiter = ratelimit.New(ratelimit.Config{
		Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
	})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for i := 0; i < 5; i++ {
		rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"","username":"u","password":"correct horse battery staple"}`))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: status = %d, want 400 every time (validation failures must never be throttled): %s", i+1, rec.Code, rec.Body.String())
		}
	}
}

func TestInviteRedeemRateLimitEscalatesOnNotRedeemableToo(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	cfg := testConfig()
	cfg.InviteRedeemer = &fakeInviteRedeemer{err: invite.ErrNotRedeemable}
	cfg.InviteRedeemRateLimiter = ratelimit.New(ratelimit.Config{
		Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute,
	})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	var codes []int
	for i := 0; i < 3; i++ {
		rec := postJSON(t, h, "/api/v1/invites/redeem", []byte(`{"token":"guess","username":"u","password":"correct horse battery staple"}`))
		codes = append(codes, rec.Code)
	}
	want := []int{http.StatusNotFound, http.StatusNotFound, http.StatusTooManyRequests}
	for i := range want {
		if codes[i] != want[i] {
			t.Errorf("attempt %d: status = %d, want %d (sequence: %v)", i+1, codes[i], want[i], codes)
		}
	}
}
