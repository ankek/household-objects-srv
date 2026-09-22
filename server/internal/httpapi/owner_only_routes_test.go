package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

var ownerOnlyRoutes = []struct {
	method string
	path   string
}{
	{http.MethodGet, "/api/v1/invites"},
	{http.MethodPost, "/api/v1/invites"},
	{http.MethodDelete, "/api/v1/invites/inv-x"},
	{http.MethodPut, "/api/v1/groups/detail-visibility"},
	{http.MethodPost, "/api/v1/custom-field-defs"},
	{http.MethodPut, "/api/v1/custom-field-defs/cfd-x"},
	{http.MethodDelete, "/api/v1/custom-field-defs/cfd-x"},
	{http.MethodGet, "/api/v1/backup"},
}

func TestOwnerOnlyRoutesRejectMembers(t *testing.T) {
	admin := &fakeInviteAdmin{rows: []fakeInviteRow{
		{groupID: sessionsTestGroup, id: "inv-x", createdByUserID: ownerUserID, expiresAt: 1_700_100_000_000},
	}}
	h, err := NewRouter(invitesConfig(&fakeInviteIssuer{}, admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, rt := range ownerOnlyRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := asTestUser(httptest.NewRequest(rt.method, rt.path, nil), "member")
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s: status = %d, want %d (a member must not reach an owner-only route); body = %s",
					rt.method, rt.path, rec.Code, http.StatusForbidden, rec.Body.String())
			}
			p := decodeProblem(t, rec)
			if p.Detail != "" {
				t.Errorf("%s %s: Forbidden carried a Detail %q, want none (A54)", rt.method, rt.path, p.Detail)
			}
		})
	}
}

func TestOwnerOnlyRoutesAllowOwners(t *testing.T) {
	admin := &fakeInviteAdmin{rows: []fakeInviteRow{
		{groupID: sessionsTestGroup, id: "inv-x", createdByUserID: ownerUserID, expiresAt: 1_700_100_000_000},
	}}
	h, err := NewRouter(invitesConfig(&fakeInviteIssuer{}, admin))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, rt := range ownerOnlyRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := asTestUser(httptest.NewRequest(rt.method, rt.path, nil), "owner")
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusForbidden {
				t.Fatalf("%s %s: status = 403 for the OWNER identity; the gate is refusing everyone, not checking role", rt.method, rt.path)
			}
		})
	}
}
