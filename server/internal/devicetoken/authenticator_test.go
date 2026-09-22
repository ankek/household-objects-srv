package devicetoken

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

func requestWithBearer(header string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	return r
}

func TestAuthenticateAcceptsAValidDeviceToken(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: "test device"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	authr, err := NewAuthenticator(f.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	got, err := authr.Authenticate(requestWithBearer("Bearer " + issued.Token))
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

func TestAuthenticateAcceptsALowercaseScheme(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: "test device"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	authr, err := NewAuthenticator(f.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	if _, err := authr.Authenticate(requestWithBearer("bearer " + issued.Token)); err != nil {
		t.Fatalf("Authenticate(lowercase scheme): %v", err)
	}
}

func TestAuthenticateRejectsEveryInvalidCredentialAsUnauthenticated(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: "test device"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	authr, err := NewAuthenticator(f.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	cases := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"wrong scheme", "Basic " + issued.Token},
		{"no scheme separator", issued.Token},
		{"empty token", "Bearer "},
		{"unknown token", "Bearer not-a-real-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := authr.Authenticate(requestWithBearer(tc.header)); !errors.Is(err, middleware.ErrUnauthenticated) {
				t.Fatalf("Authenticate(%q): error = %v, want ErrUnauthenticated", tc.header, err)
			}
		})
	}
}

func TestAuthenticateRejectsARevokedToken(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: "test device"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	authr, err := NewAuthenticator(f.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	if _, err := authr.Authenticate(requestWithBearer("Bearer " + issued.Token)); err != nil {
		t.Fatalf("Authenticate before revocation: %v", err)
	}

	f.exec(t, "UPDATE device_tokens SET revoked_at = ? WHERE id = ?", time.Now().UnixMilli(), issued.ID)

	if _, err := authr.Authenticate(requestWithBearer("Bearer " + issued.Token)); !errors.Is(err, middleware.ErrUnauthenticated) {
		t.Fatalf("Authenticate(revoked): error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateRevocationTakesEffectOnTheVeryNextRequest(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: "test device"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	authr, err := NewAuthenticator(f.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := authr.Authenticate(requestWithBearer("Bearer " + issued.Token)); err != nil {
			t.Fatalf("Authenticate call %d before revocation: %v", i, err)
		}
	}

	f.exec(t, "UPDATE device_tokens SET revoked_at = ? WHERE id = ?", time.Now().UnixMilli(), issued.ID)

	if _, err := authr.Authenticate(requestWithBearer("Bearer " + issued.Token)); !errors.Is(err, middleware.ErrUnauthenticated) {
		t.Fatalf("Authenticate immediately after revocation: error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateReflectsARoleChangeOnTheVeryNextRequest(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: "test device"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	authr, err := NewAuthenticator(f.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	for i := 0; i < 3; i++ {
		got, err := authr.Authenticate(requestWithBearer("Bearer " + issued.Token))
		if err != nil {
			t.Fatalf("Authenticate call %d before demotion: %v", i, err)
		}
		if got.Role != "owner" {
			t.Fatalf("Authenticate call %d before demotion: Role = %q, want %q", i, got.Role, "owner")
		}
	}

	f.exec(t, "UPDATE users SET role = 'member' WHERE id = ?", f.userID)

	got, err := authr.Authenticate(requestWithBearer("Bearer " + issued.Token))
	if err != nil {
		t.Fatalf("Authenticate immediately after demotion: %v", err)
	}
	if got.Role != "member" {
		t.Fatalf("Authenticate immediately after demotion: Role = %q, want %q -- a demoted owner must lose the role on the very next request, with no re-issuance", got.Role, "member")
	}
}

func TestAuthenticateErrorsNeverMentionTheToken(t *testing.T) {
	f := newFixture(t)
	authr, err := NewAuthenticator(f.storage)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	const secretToken = "super-secret-device-token-value-should-never-appear"
	_, authErr := authr.Authenticate(requestWithBearer("Bearer " + secretToken))
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
