package httpapi

import (
	"context"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDeviceTokenRow struct {
	groupID, userID, id string
	deviceLabel         string
	revoked             bool
}

type fakeDeviceTokenAdmin struct {
	rows []fakeDeviceTokenRow
}

func (f *fakeDeviceTokenAdmin) ListDeviceTokens(_ context.Context, groupID, userID string) ([]storage.DeviceTokenInfo, error) {
	var out []storage.DeviceTokenInfo
	for _, r := range f.rows {
		if r.groupID == groupID && r.userID == userID {
			out = append(out, storage.DeviceTokenInfo{ID: r.id, DeviceLabel: r.deviceLabel, Revoked: r.revoked})
		}
	}
	return out, nil
}

func (f *fakeDeviceTokenAdmin) RevokeDeviceToken(_ context.Context, groupID, userID, deviceTokenID string, _ int64) error {
	for i := range f.rows {
		r := &f.rows[i]
		if r.id != deviceTokenID {
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

var _ DeviceTokenAdmin = (*fakeDeviceTokenAdmin)(nil)

func deviceTokensConfig(admin *fakeDeviceTokenAdmin) Config {
	cfg := testConfig()
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	cfg.DeviceTokens = admin
	return cfg
}

func TestDeviceTokensListHandlerReturnsOnlyTheCallersOwnDevices(t *testing.T) {
	admin := &fakeDeviceTokenAdmin{rows: []fakeDeviceTokenRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "dvc-owner-1", deviceLabel: "Owner's Phone"},
		{groupID: sessionsTestGroup, userID: memberUserID, id: "dvc-member-1", deviceLabel: "Member's Phone"},
	}}
	h, err := NewRouter(deviceTokensConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/auth/device-tokens", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got deviceTokenListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if len(got.DeviceTokens) != 1 || got.DeviceTokens[0].ID != "dvc-owner-1" {
		t.Fatalf("DeviceTokens = %+v, want exactly [dvc-owner-1] (the member's device in the same group must not leak in)", got.DeviceTokens)
	}
}

func TestDeviceTokensListHandlerBodyShapeCarriesNoCredential(t *testing.T) {
	admin := &fakeDeviceTokenAdmin{rows: []fakeDeviceTokenRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "dvc-owner-1", deviceLabel: "Owner's Phone"},
	}}
	h, err := NewRouter(deviceTokensConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/auth/device-tokens", nil), "owner")
	h.ServeHTTP(rec, req)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	tokens, ok := raw["device_tokens"].([]any)
	if !ok || len(tokens) != 1 {
		t.Fatalf("response = %v, want a one-element \"device_tokens\" array", raw)
	}
	entry, ok := tokens[0].(map[string]any)
	if !ok {
		t.Fatalf("device_tokens[0] = %v, want an object", tokens[0])
	}

	wantKeys := map[string]bool{"id": true, "device_label": true, "created_at": true, "revoked": true}
	for k := range entry {
		if !wantKeys[k] {
			t.Errorf("unexpected key %q in device token list entry: %v", k, entry)
		}
	}

	lower := strings.ToLower(rec.Body.String())
	if strings.Contains(lower, "token_hash") || (strings.Contains(lower, "hash") && !strings.Contains(lower, "device_tokens")) {
		t.Fatalf("response body mentions a hash, which must never appear in a device listing: %s", rec.Body.String())
	}
}

func TestDeviceTokenRevokeHandlerSucceedsForTheCallersOwnToken(t *testing.T) {
	admin := &fakeDeviceTokenAdmin{rows: []fakeDeviceTokenRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "dvc-owner-1"},
	}}
	h, err := NewRouter(deviceTokensConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/device-tokens/dvc-owner-1", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if !admin.rows[0].revoked {
		t.Error("the fake's row was not marked revoked")
	}
}

func TestDeviceTokenRevokeHandlerRefusesASameGroupDifferentUsersToken(t *testing.T) {
	admin := &fakeDeviceTokenAdmin{rows: []fakeDeviceTokenRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "dvc-owner-1"},
	}}
	h, err := NewRouter(deviceTokensConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/device-tokens/dvc-owner-1", nil), "member")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("RevokeDeviceToken(member revoking owner's device): status = %d, want %d (a household member must not revoke another member's device); body = %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if admin.rows[0].revoked {
		t.Fatal("the owner's device token was revoked by the member's rejected request")
	}
}

func TestDeviceTokenRevokeHandlerUnknownAndForeignIDsAnswerIdentically(t *testing.T) {
	admin := &fakeDeviceTokenAdmin{rows: []fakeDeviceTokenRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "dvc-owner-1"},
	}}
	h, err := NewRouter(deviceTokensConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	unknown := httptest.NewRecorder()
	h.ServeHTTP(unknown, asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/device-tokens/no-such-id", nil), "member"))

	foreign := httptest.NewRecorder()
	h.ServeHTTP(foreign, asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/device-tokens/dvc-owner-1", nil), "member"))

	if unknown.Code != http.StatusNotFound || foreign.Code != http.StatusNotFound {
		t.Fatalf("status codes = %d (unknown), %d (foreign), want both %d", unknown.Code, foreign.Code, http.StatusNotFound)
	}
	unknownProblem, foreignProblem := decodeProblem(t, unknown), decodeProblem(t, foreign)
	unknownProblem.RequestID, foreignProblem.RequestID = "", ""
	if unknownProblem != foreignProblem {
		t.Errorf("problem documents differ once request_id is discounted: unknown = %+v, foreign = %+v; an attacker could tell the ids apart", unknownProblem, foreignProblem)
	}
}

func TestDeviceTokenRevokeHandlerIsIdempotent(t *testing.T) {
	admin := &fakeDeviceTokenAdmin{rows: []fakeDeviceTokenRow{
		{groupID: sessionsTestGroup, userID: ownerUserID, id: "dvc-owner-1"},
	}}
	h, err := NewRouter(deviceTokensConfig(admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for i := range 2 {
		rec := httptest.NewRecorder()
		req := asTestUser(httptest.NewRequest(http.MethodDelete, "/api/v1/auth/device-tokens/dvc-owner-1", nil), "owner")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("revoke attempt %d: status = %d, want %d", i+1, rec.Code, http.StatusNoContent)
		}
	}
}

func TestDeviceTokensRoutesUnmountedWhenConfigHasNoDeviceTokenAdmin(t *testing.T) {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/device-tokens", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (nil Config.DeviceTokens must leave the route unmounted)", rec.Code, http.StatusNotFound)
	}
}
