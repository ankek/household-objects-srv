package httpapi

import (
	"encoding/json"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"net/http"
	"testing"
	"time"
)

func regBody(username, password string) []byte {
	body, err := json.Marshal(registerRequestBody{Username: username, Password: password})
	if err != nil {
		panic(err)
	}
	return body
}

func redeemBody(token, username, password string) []byte {
	body, err := json.Marshal(inviteRedeemRequestBody{Token: token, Username: username, Password: password})
	if err != nil {
		panic(err)
	}
	return body
}

func TestRateLimitersAreIndependentBucketsAcrossEndpoints(t *testing.T) {
	newTightConfig := func(clock *fakeRateLimitClock) Config {
		cfg := testConfig()
		cfg.Registrar = &countingRegistrar{result: groups.Registered{GroupID: "g1", UserID: "u1", Username: "newuser"}}
		cfg.LoginService = fakeLoginService{err: session.ErrInvalidCredentials}
		cfg.InviteRedeemer = &fakeInviteRedeemer{err: invite.ErrNotRedeemable}
		limCfg := ratelimit.Config{Clock: clock, FreeAttempts: 1, BaseDelay: time.Minute, MaxDelay: time.Minute}
		cfg.RegisterRateLimiter = ratelimit.New(limCfg)
		cfg.LoginRateLimiter = ratelimit.New(limCfg)
		cfg.InviteRedeemRateLimiter = ratelimit.New(limCfg)
		return cfg
	}

	const ip = "203.0.113.77"

	assertUnaffected := func(t *testing.T, h http.Handler, tripped string) {
		t.Helper()
		if tripped != "register" {
			if rec := postJSONFromIP(t, h, "/api/v1/auth/register", regBody("fresh-user", "correct horse battery staple"), ip+":1"); rec.Code == http.StatusTooManyRequests {
				t.Errorf("register is blocked after %s's bucket tripped -- the buckets are not independent", tripped)
			}
		}
		if tripped != "login" {
			if rec := postJSONFromIP(t, h, "/api/v1/auth/login", loginBody("whoever", "whatever"), ip+":1"); rec.Code == http.StatusTooManyRequests {
				t.Errorf("login is blocked after %s's bucket tripped -- the buckets are not independent", tripped)
			}
		}
		if tripped != "redeem" {
			if rec := postJSONFromIP(t, h, "/api/v1/invites/redeem", redeemBody("some-other-token", "u", "correct horse battery staple"), ip+":1"); rec.Code == http.StatusTooManyRequests {
				t.Errorf("invite redeem is blocked after %s's bucket tripped -- the buckets are not independent", tripped)
			}
		}
	}

	t.Run("exhausting register", func(t *testing.T) {
		clock := &fakeRateLimitClock{now: time.Now()}
		h, err := NewRouter(newTightConfig(clock))
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		for i := 0; i < 2; i++ {
			addr := fmt.Sprintf("%s:%d", ip, 100+i)
			rec := postJSONFromIP(t, h, "/api/v1/auth/register", regBody(fmt.Sprintf("user%d", i), "correct horse battery staple"), addr)
			if rec.Code != http.StatusCreated {
				t.Fatalf("register attempt %d: status = %d, want 201: %s", i+1, rec.Code, rec.Body.String())
			}
		}
		if rec := postJSONFromIP(t, h, "/api/v1/auth/register", regBody("user-blocked", "correct horse battery staple"), ip+":199"); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("register's own bucket did not trip: status = %d, want 429 (test setup is broken, not the property under test)", rec.Code)
		}
		assertUnaffected(t, h, "register")
	})

	t.Run("exhausting login", func(t *testing.T) {
		clock := &fakeRateLimitClock{now: time.Now()}
		h, err := NewRouter(newTightConfig(clock))
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		for i := 0; i < 2; i++ {
			addr := fmt.Sprintf("%s:%d", ip, 200+i)
			rec := postJSONFromIP(t, h, "/api/v1/auth/login", loginBody("alice", "wrong password"), addr)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("login attempt %d: status = %d, want 401: %s", i+1, rec.Code, rec.Body.String())
			}
		}
		if rec := postJSONFromIP(t, h, "/api/v1/auth/login", loginBody("alice", "wrong password"), ip+":299"); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("login's own bucket did not trip: status = %d, want 429 (test setup is broken, not the property under test)", rec.Code)
		}
		assertUnaffected(t, h, "login")
	})

	t.Run("exhausting invite redeem", func(t *testing.T) {
		clock := &fakeRateLimitClock{now: time.Now()}
		h, err := NewRouter(newTightConfig(clock))
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		for i := 0; i < 2; i++ {
			addr := fmt.Sprintf("%s:%d", ip, 300+i)
			rec := postJSONFromIP(t, h, "/api/v1/invites/redeem", redeemBody("same-token", "u", "correct horse battery staple"), addr)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("redeem attempt %d: status = %d, want 404 (ErrNotRedeemable): %s", i+1, rec.Code, rec.Body.String())
			}
		}
		if rec := postJSONFromIP(t, h, "/api/v1/invites/redeem", redeemBody("same-token", "u", "correct horse battery staple"), ip+":399"); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("invite redeem's own bucket did not trip: status = %d, want 429 (test setup is broken, not the property under test)", rec.Code)
		}
		assertUnaffected(t, h, "redeem")
	})
}

func TestTooManyRequestsBodyIsIdenticalAcrossEndpoints(t *testing.T) {
	clock := &fakeRateLimitClock{now: time.Now()}
	cfg := testConfig()
	cfg.Registrar = &countingRegistrar{result: groups.Registered{GroupID: "g1", UserID: "u1", Username: "newuser"}}
	cfg.LoginService = fakeLoginService{err: session.ErrInvalidCredentials}
	cfg.InviteRedeemer = &fakeInviteRedeemer{err: invite.ErrNotRedeemable}
	limCfg := ratelimit.Config{Clock: clock, FreeAttempts: 1, BaseDelay: time.Minute, MaxDelay: time.Minute}
	cfg.RegisterRateLimiter = ratelimit.New(limCfg)
	cfg.LoginRateLimiter = ratelimit.New(limCfg)
	cfg.InviteRedeemRateLimiter = ratelimit.New(limCfg)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	get429 := func(method, path string, body []byte, addr string) problem.Problem {
		_ = postJSONFromIP(t, h, path, body, addr+":1")
		_ = postJSONFromIP(t, h, path, body, addr+":2")
		rec := postJSONFromIP(t, h, path, body, addr+":3")
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s %s: third attempt status = %d, want 429: %s", method, path, rec.Code, rec.Body.String())
		}
		p := decodeProblem(t, rec)
		p.RequestID = ""
		return p
	}

	registerProblem := get429(http.MethodPost, "/api/v1/auth/register", regBody("u1", "correct horse battery staple"), "203.0.113.10")
	loginProblem := get429(http.MethodPost, "/api/v1/auth/login", loginBody("u2", "wrong"), "203.0.113.11")
	redeemProblem := get429(http.MethodPost, "/api/v1/invites/redeem", redeemBody("t", "u3", "correct horse battery staple"), "203.0.113.12")

	if registerProblem != loginProblem || loginProblem != redeemProblem {
		t.Errorf("429 bodies differ across endpoints once request_id is discounted:\n  register = %+v\n  login    = %+v\n  redeem   = %+v", registerProblem, loginProblem, redeemProblem)
	}
	if registerProblem.Detail != "" {
		t.Errorf("429 carries a Detail %q, want empty", registerProblem.Detail)
	}
}
