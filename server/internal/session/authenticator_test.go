package session

import (
	"bytes"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewAuthenticatorRejectsNilStorage(t *testing.T) {
	if _, err := NewAuthenticator(nil); err == nil {
		t.Error("NewAuthenticator accepted a nil *storage.Storage")
	}
}

func requestWithCookie(value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	if value != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: value})
	}
	return r
}

func TestAuthenticateAcceptsAValidSession(t *testing.T) {
	f := newFixture(t)
	logged, err := f.Login(t.Context(), LoginRequest{Username: "alice", Password: []byte(testPassword)})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	authr, err := NewAuthenticator(f.Service.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	got, err := authr.Authenticate(requestWithCookie(logged.Token))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.Group != f.groupID {
		t.Errorf("Group = %q, want %q", got.Group, f.groupID)
	}
	if got.UserID != f.userID {
		t.Errorf("UserID = %q, want %q", got.UserID, f.userID)
	}
	if got.Role != "owner" {
		t.Errorf("Role = %q, want %q", got.Role, "owner")
	}
}

func TestAuthenticateRejectsEveryInvalidCredentialAsUnauthenticated(t *testing.T) {
	f := newFixture(t)
	logged, err := f.Login(t.Context(), LoginRequest{Username: "alice", Password: []byte(testPassword)})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	authr, err := NewAuthenticator(f.Service.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	t.Run("no cookie", func(t *testing.T) {
		if _, err := authr.Authenticate(requestWithCookie("")); !errors.Is(err, middleware.ErrUnauthenticated) {
			t.Fatalf("Authenticate(no cookie): error = %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("unknown token", func(t *testing.T) {
		if _, err := authr.Authenticate(requestWithCookie("not-a-real-token")); !errors.Is(err, middleware.ErrUnauthenticated) {
			t.Fatalf("Authenticate(unknown token): error = %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("revoked", func(t *testing.T) {
		f.exec(t, "UPDATE sessions SET revoked_at = ? WHERE user_id = ?", time.Now().UnixMilli(), f.userID)
		if _, err := authr.Authenticate(requestWithCookie(logged.Token)); !errors.Is(err, middleware.ErrUnauthenticated) {
			t.Fatalf("Authenticate(revoked): error = %v, want ErrUnauthenticated", err)
		}
	})
}

func TestAuthenticateReflectsARoleChangeOnTheVeryNextRequest(t *testing.T) {
	f := newFixture(t)
	logged, err := f.Login(t.Context(), LoginRequest{Username: "alice", Password: []byte(testPassword)})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	authr, err := NewAuthenticator(f.Service.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	for i := 0; i < 3; i++ {
		got, err := authr.Authenticate(requestWithCookie(logged.Token))
		if err != nil {
			t.Fatalf("Authenticate call %d before demotion: %v", i, err)
		}
		if got.Role != "owner" {
			t.Fatalf("Authenticate call %d before demotion: Role = %q, want %q", i, got.Role, "owner")
		}
	}

	f.exec(t, "UPDATE users SET role = 'member' WHERE id = ?", f.userID)

	got, err := authr.Authenticate(requestWithCookie(logged.Token))
	if err != nil {
		t.Fatalf("Authenticate immediately after demotion: %v", err)
	}
	if got.Role != "member" {
		t.Fatalf("Authenticate immediately after demotion: Role = %q, want %q -- a demoted owner must lose the role on the very next request, not on the next login", got.Role, "member")
	}
}

func TestAuthenticateRejectsAnExpiredSession(t *testing.T) {
	f := newFixture(t)
	logged, err := f.Login(t.Context(), LoginRequest{Username: "alice", Password: []byte(testPassword)})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	f.exec(t, "UPDATE sessions SET expires_at = ? WHERE user_id = ?", time.Now().Add(-time.Hour).UnixMilli(), f.userID)

	authr, err := NewAuthenticator(f.Service.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	if _, err := authr.Authenticate(requestWithCookie(logged.Token)); !errors.Is(err, middleware.ErrUnauthenticated) {
		t.Fatalf("Authenticate(expired): error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateErrorsNeverMentionTheToken(t *testing.T) {
	f := newFixture(t)
	authr, err := NewAuthenticator(f.Service.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	const secretToken = "super-secret-token-value-should-never-appear"
	_, authErr := authr.Authenticate(requestWithCookie(secretToken))
	if authErr == nil {
		t.Fatal("Authenticate(unknown secret token) succeeded, want an error")
	}
	if strings.Contains(authErr.Error(), secretToken) {
		t.Fatalf("Authenticate's error mentions the raw token: %v", authErr)
	}

	sum := bearertoken.Hash(secretToken)
	if bytes.Contains([]byte(authErr.Error()), []byte(sum)) {
		t.Fatalf("Authenticate's error mentions the token's digest: %v", authErr)
	}
}
