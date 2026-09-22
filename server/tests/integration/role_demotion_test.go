package integration

import (
	"net/http"
	"testing"
)

func TestOwnerDemotedMidSessionLosesOwnerRouteOnTheVeryNextRequest(t *testing.T) {
	srv := newLiveServer(t, false)

	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", "alice-password-1")); rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d: %s", rec.Code, rec.Body.String())
	}
	rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", "alice-password-1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d: %s", rec.Code, rec.Body.String())
	}
	var login loginResponse
	mustDecode(t, rec, &login)
	if login.Role != "owner" {
		t.Fatalf("alice's role at login = %q, want owner", login.Role)
	}
	cookie := sessionCookie(t, rec)

	for i := 0; i < 2; i++ {
		if rec := srv.do(t, http.MethodPost, "/api/v1/invites", cookie, nil); rec.Code != http.StatusCreated {
			t.Fatalf("POST /invites before demotion (call %d): status = %d, want 201: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	srv.execSQL(t, "UPDATE users SET role = 'member' WHERE id = ?", login.UserID)

	rec = srv.do(t, http.MethodPost, "/api/v1/invites", cookie, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /invites immediately after demotion: status = %d, want 403 -- a demoted owner must lose the owner-only route on the very next request, not on the next login", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Detail != "" {
		t.Errorf("403 after demotion carries a Detail %q, want none (A54)", p.Detail)
	}

	if rec := srv.do(t, http.MethodGet, "/api/v1/auth/sessions", cookie, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /auth/sessions after demotion: status = %d, want 200 -- demotion must not revoke the session itself, only owner-gated routes", rec.Code)
	}
}
