package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const authMePath = "/api/v1/auth/me"

func authMeTestConfig(repo storage.MemberRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	cfg.Scopes = membersFakeScopes{repo: repo}
	return cfg
}

func authMeRoster() storage.MemberRepository {
	return fakeMemberRepository{
		listFn: func(context.Context) ([]storage.Member, error) {
			return []storage.Member{
				{ID: ownerUserID, Username: "alice", Role: "owner", JoinedAtUnixMilli: 1},
				{ID: memberUserID, Username: "bob", Role: "member", JoinedAtUnixMilli: 2},
			}, nil
		},
	}
}

func TestAuthMeHandlerReturnsCallerIdentity(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		wantBody authMeResponseBody
	}{
		{
			name: "owner",
			user: "owner",
			wantBody: authMeResponseBody{
				GroupID: sessionsTestGroup, UserID: ownerUserID, Username: "alice", Role: "owner",
			},
		},
		{
			name: "member",
			user: "member",
			wantBody: authMeResponseBody{
				GroupID: sessionsTestGroup, UserID: memberUserID, Username: "bob", Role: "member",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := NewRouter(authMeTestConfig(authMeRoster()))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}

			rec := httptest.NewRecorder()
			req := asTestUser(httptest.NewRequest(http.MethodGet, authMePath, nil), tt.user)
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s as %s: status = %d, want %d; body = %s", authMePath, tt.user, rec.Code, http.StatusOK, rec.Body.String())
			}
			var got authMeResponseBody
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
			}
			if got != tt.wantBody {
				t.Errorf("GET %s as %s: body = %+v, want %+v", authMePath, tt.user, got, tt.wantBody)
			}
		})
	}
}

func TestAuthMeRouteAnswersADeviceToken(t *testing.T) {
	h, err := NewRouter(authMeTestConfig(authMeRoster()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, authMePath, nil), "owner")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAuthMeRouteRejectsNoCredentialAtAll(t *testing.T) {
	h, err := NewRouter(testConfig())
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, authMePath, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAuthMeHandlerBodyShapeCarriesNoSecret(t *testing.T) {
	h, err := NewRouter(authMeTestConfig(authMeRoster()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, authMePath, nil), "owner")
	h.ServeHTTP(rec, req)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}

	wantKeys := map[string]bool{"group_id": true, "user_id": true, "username": true, "role": true}
	for k := range raw {
		if !wantKeys[k] {
			t.Errorf("unexpected key %q in auth/me response: %v", k, raw)
		}
	}
	for k := range wantKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing key %q in auth/me response: %v", k, raw)
		}
	}

	lower := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"password", "hash", "session", "token"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("response body mentions %q, which must never appear in an auth/me response: %s", forbidden, rec.Body.String())
		}
	}
}

func TestAuthMeHandlerReportsStorageErrors(t *testing.T) {
	repo := fakeMemberRepository{
		listFn: func(context.Context) ([]storage.Member, error) { return nil, errors.New("boom") },
	}
	h, err := NewRouter(authMeTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, authMePath, nil), "owner")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestAuthMeHandlerReturnsInternalWhenCallerMissingFromOwnRoster(t *testing.T) {
	repo := fakeMemberRepository{
		listFn: func(context.Context) ([]storage.Member, error) { return nil, nil },
	}
	h, err := NewRouter(authMeTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, authMePath, nil), "owner")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}
