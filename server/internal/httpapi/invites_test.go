package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeInviteIssuer struct {
	result     invite.Issued
	err        error
	gotRequest invite.IssueRequest
}

func (f *fakeInviteIssuer) Issue(_ context.Context, req invite.IssueRequest) (invite.Issued, error) {
	f.gotRequest = req
	return f.result, f.err
}

var _ InviteIssuer = (*fakeInviteIssuer)(nil)

type fakeInviteRow struct {
	groupID, id, createdByUserID string
	redeemed                     bool
	expiresAt                    int64
	revoked                      bool
}

type fakeInviteAdmin struct {
	rows []fakeInviteRow
	err  error
}

func (f *fakeInviteAdmin) ListInvites(_ context.Context, groupID string) ([]storage.InviteInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []storage.InviteInfo
	for _, r := range f.rows {
		if r.groupID == groupID {
			out = append(out, storage.InviteInfo{
				ID:                 r.id,
				CreatedByUserID:    r.createdByUserID,
				Redeemed:           r.redeemed,
				ExpiresAtUnixMilli: r.expiresAt,
			})
		}
	}
	return out, nil
}

func (f *fakeInviteAdmin) RevokeInvite(_ context.Context, groupID, inviteID string, _ int64) error {
	if f.err != nil {
		return f.err
	}
	for i := range f.rows {
		r := &f.rows[i]
		if r.id != inviteID {
			continue
		}
		if r.groupID != groupID {
			return storage.ErrNotFound
		}
		r.revoked = true
		return nil
	}
	return storage.ErrNotFound
}

var _ InviteAdmin = (*fakeInviteAdmin)(nil)

func invitesConfig(issuer *fakeInviteIssuer, admin *fakeInviteAdmin) Config {
	cfg := testConfig()
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	cfg.InviteService = issuer
	cfg.Invites = admin
	return cfg
}

func TestInviteCreateHandlerSucceedsForOwner(t *testing.T) {
	expiresAt := time.UnixMilli(1_700_100_000_000)
	issuer := &fakeInviteIssuer{result: invite.Issued{ID: "inv-1", Token: "raw-token-value", ExpiresAt: expiresAt}}
	h, err := NewRouter(invitesConfig(issuer, &fakeInviteAdmin{}))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/invites", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var got inviteCreateResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if got.ID != "inv-1" || got.Token != "raw-token-value" || got.ExpiresAt != expiresAt.UnixMilli() {
		t.Errorf("response = %+v, want {ID: inv-1, Token: raw-token-value, ExpiresAt: %d}", got, expiresAt.UnixMilli())
	}

	if issuer.gotRequest.GroupID != sessionsTestGroup || issuer.gotRequest.CreatedByUserID != ownerUserID {
		t.Errorf("Issue was called with %+v, want GroupID=%q CreatedByUserID=%q (the authenticated owner's own identity)",
			issuer.gotRequest, sessionsTestGroup, ownerUserID)
	}
}

func TestInviteCreateHandlerReportsIssuanceErrors(t *testing.T) {
	issuer := &fakeInviteIssuer{err: errors.New("boom")}
	h, err := NewRouter(invitesConfig(issuer, &fakeInviteAdmin{}))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/invites", nil), "owner")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestInviteListHandlerReturnsEveryInviteInTheGroup(t *testing.T) {
	admin := &fakeInviteAdmin{rows: []fakeInviteRow{
		{groupID: sessionsTestGroup, id: "inv-owner-1", createdByUserID: ownerUserID, expiresAt: 1_700_100_000_000},
		{groupID: sessionsTestGroup, id: "inv-member-1", createdByUserID: memberUserID, expiresAt: 1_700_200_000_000},
	}}
	h, err := NewRouter(invitesConfig(&fakeInviteIssuer{}, admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/invites", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got inviteListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if len(got.Invites) != 2 {
		t.Fatalf("Invites = %+v, want both invites regardless of which household member created them", got.Invites)
	}
}

func TestInviteListHandlerBodyShapeCarriesNoCredential(t *testing.T) {
	admin := &fakeInviteAdmin{rows: []fakeInviteRow{
		{groupID: sessionsTestGroup, id: "inv-owner-1", createdByUserID: ownerUserID, expiresAt: 1_700_100_000_000},
	}}
	h, err := NewRouter(invitesConfig(&fakeInviteIssuer{}, admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/invites", nil), "owner")
	h.ServeHTTP(rec, req)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	invites, ok := raw["invites"].([]any)
	if !ok || len(invites) != 1 {
		t.Fatalf("response = %v, want a one-element \"invites\" array", raw)
	}
	entry, ok := invites[0].(map[string]any)
	if !ok {
		t.Fatalf("invites[0] = %v, want an object", invites[0])
	}

	wantKeys := map[string]bool{"id": true, "created_by_user_id": true, "created_at": true, "expires_at": true, "redeemed": true}
	for k := range entry {
		if !wantKeys[k] {
			t.Errorf("unexpected key %q in invite list entry: %v", k, entry)
		}
	}

	lower := strings.ToLower(rec.Body.String())
	if strings.Contains(lower, "token") || strings.Contains(lower, "hash") {
		t.Fatalf("response body mentions a token or a hash, which must never appear in an invite listing: %s", rec.Body.String())
	}
}

func TestInviteRevokeHandlerSucceedsForAnyOwner(t *testing.T) {
	admin := &fakeInviteAdmin{rows: []fakeInviteRow{
		{groupID: sessionsTestGroup, id: "inv-1", createdByUserID: memberUserID, expiresAt: 1_700_100_000_000},
	}}
	h, err := NewRouter(invitesConfig(&fakeInviteIssuer{}, admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/invites/inv-1", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if !admin.rows[0].revoked {
		t.Error("the fake's row was not marked revoked")
	}
}

func TestInviteRevokeHandlerRejectsAForeignGroupID(t *testing.T) {
	admin := &fakeInviteAdmin{rows: []fakeInviteRow{
		{groupID: "grp-other-household", id: "inv-1", createdByUserID: "usr-other", expiresAt: 1_700_100_000_000},
	}}
	h, err := NewRouter(invitesConfig(&fakeInviteIssuer{}, admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/invites/inv-1", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (an invite belonging to another group must not be revocable); body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if admin.rows[0].revoked {
		t.Fatal("another group's invite was revoked by this request")
	}
}

func TestInviteRevokeHandlerIsIdempotent(t *testing.T) {
	admin := &fakeInviteAdmin{rows: []fakeInviteRow{
		{groupID: sessionsTestGroup, id: "inv-1", createdByUserID: ownerUserID, expiresAt: 1_700_100_000_000},
	}}
	h, err := NewRouter(invitesConfig(&fakeInviteIssuer{}, admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for i := range 2 {
		rec := httptest.NewRecorder()
		req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/invites/inv-1", nil), "owner")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("revoke attempt %d: status = %d, want %d", i+1, rec.Code, http.StatusNoContent)
		}
	}
}

func TestInvitesRoutesRejectNoCredentialAtAll(t *testing.T) {
	h, err := NewRouter(invitesConfig(&fakeInviteIssuer{}, &fakeInviteAdmin{}))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/invites", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/invites", nil),
		httptest.NewRequest(http.MethodDelete, "/api/v1/invites/inv-1", nil),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want %d", req.Method, req.URL.Path, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestInvitesRoutesUnmountedWhenConfigIsIncomplete(t *testing.T) {
	cases := []struct {
		name string
		cfg  func() Config
	}{
		{"nil InviteService", func() Config {
			cfg := invitesConfig(&fakeInviteIssuer{}, &fakeInviteAdmin{})
			cfg.InviteService = nil
			return cfg
		}},
		{"nil Invites", func() Config {
			cfg := invitesConfig(&fakeInviteIssuer{}, &fakeInviteAdmin{})
			cfg.Invites = nil
			return cfg
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(tc.cfg())
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			rec := httptest.NewRecorder()
			req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/invites", nil), "owner")
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (an incomplete invite seam must leave the routes unmounted)", rec.Code, http.StatusNotFound)
			}
		})
	}
}
