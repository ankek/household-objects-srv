package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSessionRow struct {
	groupID, userID, id string
	userAgent           string
	revoked             bool
	tokenHash           string
}

type fakeSessionAdmin struct {
	rows []fakeSessionRow
	err  error
}

func (f *fakeSessionAdmin) ListSessions(_ context.Context, groupID, userID string) ([]storage.SessionInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []storage.SessionInfo
	for _, r := range f.rows {
		if r.groupID == groupID && r.userID == userID {
			out = append(out, storage.SessionInfo{ID: r.id, UserAgent: r.userAgent, Revoked: r.revoked})
		}
	}
	return out, nil
}

func (f *fakeSessionAdmin) RevokeSession(_ context.Context, groupID, userID, sessionID string, _ int64) error {
	if f.err != nil {
		return f.err
	}
	for i := range f.rows {
		r := &f.rows[i]
		if r.id != sessionID {
			continue
		}
		if r.groupID != groupID || r.userID != userID {
			return storage.ErrNotFound
		}
		r.revoked = true
		return nil
	}
	return storage.ErrNotFound
}

func (f *fakeSessionAdmin) RevokeCallingSession(_ context.Context, groupID, userID, tokenHash string, _ int64) error {
	if f.err != nil {
		return f.err
	}
	for i := range f.rows {
		r := &f.rows[i]
		if r.tokenHash != tokenHash || r.groupID != groupID || r.userID != userID {
			continue
		}
		r.revoked = true
		return nil
	}
	return nil
}

var _ SessionAdmin = (*fakeSessionAdmin)(nil)

func identityByHeaderAuth(table map[string]middleware.Identity) middleware.AuthenticatorFunc {
	return func(r *http.Request) (middleware.Identity, error) {
		id, ok := table[r.Header.Get("X-Test-User")]
		if !ok {
			return middleware.Identity{}, fmt.Errorf("no such test user: %w", middleware.ErrUnauthenticated)
		}
		return id, nil
	}
}

const (
	sessionsTestGroup = "grp-household"
	ownerUserID       = "usr-owner"
	memberUserID      = "usr-member"
)

var sessionsTestIdentities = map[string]middleware.Identity{
	"owner":  {Group: sessionsTestGroup, UserID: ownerUserID, Role: "owner"},
	"member": {Group: sessionsTestGroup, UserID: memberUserID, Role: "member"},
}

func sessionsConfig(admin *fakeSessionAdmin) Config {
	cfg := testConfig()
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	cfg.Sessions = admin
	return cfg
}

func asTestUser(req *http.Request, user string) *http.Request {
	req.Header.Set("X-Test-User", user)
	return req
}

func TestSessionsListHandlerReturnsOnlyTheCallersOwnSessions(t *testing.T) {
	admin := &fakeSessionAdmin{rows: []fakeSessionRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-owner-1", userAgent: "owner-ua"},
		{groupID: sessionsTestGroup, userID: memberUserID, id: "ses-member-1", userAgent: "member-ua"},
	}}
	h, err := NewRouter(sessionsConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got sessionListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "ses-owner-1" {
		t.Fatalf("Sessions = %+v, want exactly [ses-owner-1] (the member's session in the same group must not leak in)", got.Sessions)
	}
}

func TestSessionsListHandlerBodyShapeCarriesNoCredential(t *testing.T) {
	admin := &fakeSessionAdmin{rows: []fakeSessionRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-owner-1", userAgent: "owner-ua"},
	}}
	h, err := NewRouter(sessionsConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil), "owner")
	h.ServeHTTP(rec, req)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	sessions, ok := raw["sessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("response = %v, want a one-element \"sessions\" array", raw)
	}
	entry, ok := sessions[0].(map[string]any)
	if !ok {
		t.Fatalf("sessions[0] = %v, want an object", sessions[0])
	}

	wantKeys := map[string]bool{"id": true, "user_agent": true, "created_from_ip": true, "created_at": true, "expires_at": true, "revoked": true}
	for k := range entry {
		if !wantKeys[k] {
			t.Errorf("unexpected key %q in session list entry: %v", k, entry)
		}
	}

	lower := strings.ToLower(rec.Body.String())
	if strings.Contains(lower, "token") || strings.Contains(lower, "hash") {
		t.Fatalf("response body mentions a token or a hash, which must never appear in a session listing: %s", rec.Body.String())
	}
}

func TestSessionRevokeHandlerSucceedsForTheCallersOwnSession(t *testing.T) {
	admin := &fakeSessionAdmin{rows: []fakeSessionRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-owner-1"},
	}}
	h, err := NewRouter(sessionsConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/ses-owner-1", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if !admin.rows[0].revoked {
		t.Error("the fake's row was not marked revoked")
	}
	if got := rec.Header().Get("Set-Cookie"); got != "" {
		t.Errorf("Set-Cookie = %q, want none; a revoke response must not touch the cookie", got)
	}
}

func TestSessionRevokeHandlerRefusesASameGroupDifferentUsersSession(t *testing.T) {
	admin := &fakeSessionAdmin{rows: []fakeSessionRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-owner-1"},
	}}
	h, err := NewRouter(sessionsConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/ses-owner-1", nil), "member")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("RevokeSession(member revoking owner's session): status = %d, want %d (a household member must not revoke another member's session); body = %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if admin.rows[0].revoked {
		t.Fatal("the owner's session was revoked by the member's rejected request")
	}
}

func TestSessionRevokeHandlerUnknownAndForeignIDsAnswerIdentically(t *testing.T) {
	admin := &fakeSessionAdmin{rows: []fakeSessionRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-owner-1"},
	}}
	h, err := NewRouter(sessionsConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	unknown := httptest.NewRecorder()
	h.ServeHTTP(unknown, asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/no-such-id", nil), "member"))

	foreign := httptest.NewRecorder()
	h.ServeHTTP(foreign, asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/ses-owner-1", nil), "member"))

	if unknown.Code != http.StatusNotFound || foreign.Code != http.StatusNotFound {
		t.Fatalf("status codes = %d (unknown), %d (foreign), want both %d", unknown.Code, foreign.Code, http.StatusNotFound)
	}
	unknownProblem, foreignProblem := decodeProblem(t, unknown), decodeProblem(t, foreign)
	unknownProblem.RequestID, foreignProblem.RequestID = "", ""
	if unknownProblem != foreignProblem {
		t.Errorf("problem documents differ once request_id is discounted: unknown = %+v, foreign = %+v; an attacker could tell the ids apart", unknownProblem, foreignProblem)
	}
	if unknownProblem.Detail != "" {
		t.Errorf("Detail = %q, want empty; NotFound must carry no elaboration", unknownProblem.Detail)
	}
}

func TestSessionRevokeHandlerIsIdempotent(t *testing.T) {
	admin := &fakeSessionAdmin{rows: []fakeSessionRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "ses-owner-1"},
	}}
	h, err := NewRouter(sessionsConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for i := range 2 {
		rec := httptest.NewRecorder()
		req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/ses-owner-1", nil), "owner")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("revoke attempt %d: status = %d, want %d", i+1, rec.Code, http.StatusNoContent)
		}
	}
}

func TestSessionsRoutesRejectNoCredentialAtAll(t *testing.T) {
	admin := &fakeSessionAdmin{}
	h, err := NewRouter(sessionsConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil),
		httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/ses-1", nil),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want %d", req.Method, req.URL.Path, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestSessionsRoutesUnmountedWhenConfigHasNoSessionAdmin(t *testing.T) {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (nil Config.Sessions must leave the route unmounted)", rec.Code, http.StatusNotFound)
	}
}

func TestSessionsListHandlerReportsStorageErrors(t *testing.T) {
	admin := &fakeSessionAdmin{err: errors.New("boom")}
	h, err := NewRouter(sessionsConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil), "owner")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
