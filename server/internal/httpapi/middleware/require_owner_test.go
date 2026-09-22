package middleware

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/roles"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"testing"
)

func ownerGatedChain(t *testing.T, identity Identity) (http.Handler, *bool) {
	t.Helper()
	logger, _ := captureLogs()
	reached := false
	inner := Scoped(func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		reached = true
		if want := storage.MustGroupID(identity.Group); scope.GroupID() != want {
			t.Errorf("handler saw scope for group %q, want %q -- RequireOwner must not disturb TenantScope's own binding", scope.GroupID(), want)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := chain(logger)(TenantScope(authFor(identity), fakeScopes, logger)(RequireOwner(logger)(inner)))
	return h, &reached
}

func TestRequireOwnerAllowsAnOwner(t *testing.T) {
	h, reached := ownerGatedChain(t, Identity{Group: testGroup, UserID: "u-owner", Role: roles.Owner})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invites", nil))

	if !*reached {
		t.Fatal("the handler never ran for an owner")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %q", rec.Code, rec.Body.String())
	}
}

func TestRequireOwnerRefusesAMember(t *testing.T) {
	h, reached := ownerGatedChain(t, Identity{Group: testGroup, UserID: "u-member", Role: roles.Member})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invites", nil))

	if *reached {
		t.Fatal("the handler ran for a member on an owner-only route")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %q", rec.Code, rec.Body.String())
	}

	p := decodeProblem(t, rec)
	if p.Type != "urn:hho:problem:forbidden" {
		t.Errorf("problem.type = %q, want the forbidden kind", p.Type)
	}
	if p.Detail != "" {
		t.Errorf("the 403 carries detail %q; a role refusal states no more than the refusal itself", p.Detail)
	}
	if p.RequestID == "" {
		t.Error("the 403 carries no request_id (FR-136)")
	}
}

func TestRequireOwnerRefusesAnEmptyRole(t *testing.T) {
	h, reached := ownerGatedChain(t, Identity{Group: testGroup, UserID: "u-blank", Role: ""})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invites", nil))

	if *reached {
		t.Fatal("the handler ran for an identity with no recognised role")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %q", rec.Code, rec.Body.String())
	}
}

func TestRequireOwnerFailsClosedWhenMountedOutsideTenantScope(t *testing.T) {
	logger, _ := captureLogs()
	reached := false
	h := RequireOwner(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invites", nil))

	if reached {
		t.Fatal("the handler ran with no identity in context")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %q", rec.Code, rec.Body.String())
	}
}

func TestRequireOwnerPanicsOnNilLogger(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RequireOwner(nil) did not panic")
		}
	}()
	RequireOwner(nil)
}

func TestRequireOwnerDoesNotWidenAccessAcrossGroups(t *testing.T) {
	h, reached := ownerGatedChain(t, Identity{Group: otherGroup, UserID: "u-owner-b", Role: roles.Owner})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invites", nil))

	if !*reached {
		t.Fatal("the handler never ran for an owner of otherGroup")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %q", rec.Code, rec.Body.String())
	}
}
