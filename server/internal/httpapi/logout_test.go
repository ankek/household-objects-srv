package httpapi

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var logoutCookieAuth = middleware.AuthenticatorFunc(func(r *http.Request) (middleware.Identity, error) {
	c, err := r.Cookie(session.CookieName)
	if err != nil || c.Value == "" {
		return middleware.Identity{}, fmt.Errorf("no session cookie: %w", middleware.ErrUnauthenticated)
	}
	return middleware.Identity{Group: sessionsTestGroup, UserID: ownerUserID, Role: "owner"}, nil
})

func logoutConfig(admin *fakeSessionAdmin) Config {
	cfg := testConfig()
	cfg.Authenticator = fakeBearerAcceptingAuth
	cfg.SessionAuthenticator = logoutCookieAuth
	cfg.Sessions = admin
	return cfg
}

func postLogout(t *testing.T, h http.Handler, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookieValue})
	}
	h.ServeHTTP(rec, req)
	return rec
}

const logoutCookieValue = "raw-cookie-value-for-the-calling-session"

func logoutAdmin() *fakeSessionAdmin {
	return &fakeSessionAdmin{rows: []fakeSessionRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-calling", tokenHash: bearertoken.Hash(logoutCookieValue)},
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-other-tab", tokenHash: bearertoken.Hash("a-different-cookie")},
	}}
}

func rowByID(t *testing.T, admin *fakeSessionAdmin, id string) fakeSessionRow {
	t.Helper()
	for _, r := range admin.rows {
		if r.id == id {
			return r
		}
	}
	t.Fatalf("no fake session row with id %q", id)
	return fakeSessionRow{}
}

func TestLogoutRevokesOnlyTheSessionBehindTheCallersOwnCookie(t *testing.T) {
	admin := logoutAdmin()
	h, err := NewRouter(logoutConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postLogout(t, h, logoutCookieValue)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /auth/logout = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if got := rowByID(t, admin, "ses-calling"); !got.revoked {
		t.Error("the session behind the presented cookie is still live after a 204 logout")
	}
	if got := rowByID(t, admin, "ses-other-tab"); got.revoked {
		t.Error("logout revoked the caller's OTHER session too; it must end the calling session alone")
	}
}

func TestLogoutClearsTheSessionCookie(t *testing.T) {
	h, err := NewRouter(logoutConfig(logoutAdmin()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postLogout(t, h, logoutCookieValue)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /auth/logout = %d, want %d", rec.Code, http.StatusNoContent)
	}

	var cleared *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			cleared = c
		}
	}
	if cleared == nil {
		t.Fatalf("no %s cookie in the response; Set-Cookie headers: %v", session.CookieName, rec.Header().Values("Set-Cookie"))
	}
	if cleared.Value != "" {
		t.Errorf("cleared cookie carries value %q, want empty", cleared.Value)
	}
	if cleared.MaxAge > 0 {
		t.Errorf("cleared cookie MaxAge = %d, want <= 0 so the client drops it", cleared.MaxAge)
	}
	if cleared.Path != "/" {
		t.Errorf("cleared cookie Path = %q, want %q -- a browser replaces a cookie only when Name, Path and Domain all agree with the one login set", cleared.Path, "/")
	}
	if !cleared.HttpOnly || !cleared.Secure {
		t.Errorf("cleared cookie HttpOnly=%v Secure=%v, want both true to match the attributes login set (A39)", cleared.HttpOnly, cleared.Secure)
	}
}

func TestLogoutNeverEchoesTheCookie(t *testing.T) {
	h, err := NewRouter(logoutConfig(logoutAdmin()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postLogout(t, h, logoutCookieValue)
	if strings.Contains(rec.Body.String(), logoutCookieValue) {
		t.Error("the response body echoes the session cookie")
	}
	for _, v := range rec.Header().Values("Set-Cookie") {
		if strings.Contains(v, logoutCookieValue) {
			t.Errorf("a Set-Cookie header echoes the session cookie: %q", v)
		}
	}
	if strings.Contains(rec.Body.String(), bearertoken.Hash(logoutCookieValue)) {
		t.Error("the response body echoes the cookie's digest")
	}
}

func TestLogoutRejectsARequestWithNoCookie(t *testing.T) {
	admin := logoutAdmin()
	h, err := NewRouter(logoutConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postLogout(t, h, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /auth/logout with no cookie = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rowByID(t, admin, "ses-calling").revoked {
		t.Error("an unauthenticated logout revoked a session")
	}
}

func TestLogoutRejectsABearerOnlyRequest(t *testing.T) {
	h, err := NewRouter(logoutConfig(logoutAdmin()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer a-perfectly-good-device-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /auth/logout with a bearer token = %d, want %d -- logout must be gated by the cookie-only authenticator, since the cookie IS the address of the session it ends", rec.Code, http.StatusUnauthorized)
	}
}

func TestLogoutOfAnAlreadyEndedSessionStillSucceeds(t *testing.T) {
	admin := &fakeSessionAdmin{rows: []fakeSessionRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-other", tokenHash: bearertoken.Hash("someone-elses-cookie")},
	}}
	h, err := NewRouter(logoutConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postLogout(t, h, logoutCookieValue)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /auth/logout for an already-ended session = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rowByID(t, admin, "ses-other").revoked {
		t.Error("logout revoked a session whose digest did not match the presented cookie")
	}
}

func TestLogoutTranslatesAStorageFailureToA500(t *testing.T) {
	admin := &fakeSessionAdmin{err: errors.New("boom")}
	h, err := NewRouter(logoutConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postLogout(t, h, logoutCookieValue)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /auth/logout with a failing storage = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestLogoutRouteUnmountedWhenConfigHasNoSessionAdmin(t *testing.T) {
	cfg := logoutConfig(nil)
	cfg.Sessions = nil
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := postLogout(t, h, logoutCookieValue)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /auth/logout with no Config.Sessions = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
