package integration

import (
	"net/http"
	"testing"
)

func TestFullLifecycleThroughRealRouterAndDatabase(t *testing.T) {
	srv := newLiveServer(t, false)
	const alicePassword = "alice-correct-horse-battery"
	const bobPassword = "bob-correct-horse-battery-2"

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", alicePassword))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var registered registerResponse
	mustDecode(t, rec, &registered)
	if registered.GroupID == "" || registered.UserID == "" {
		t.Fatalf("register: empty ids in response %+v", registered)
	}

	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("mallory", "does-not-matter-1")); rec.Code != http.StatusConflict {
		t.Fatalf("second register: status = %d, want 409 (registration closed by default)", rec.Code)
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", alicePassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("alice login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var aliceLogin loginResponse
	mustDecode(t, rec, &aliceLogin)
	if aliceLogin.Role != "owner" {
		t.Fatalf("alice's role = %q, want owner (RegisterFirstUser's contract)", aliceLogin.Role)
	}
	aliceCookie := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/invites", aliceCookie, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite issue: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var issued inviteCreateResponse
	mustDecode(t, rec, &issued)
	if issued.Token == "" {
		t.Fatal("invite issue: empty token")
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/invites/redeem", "", redeemBody(issued.Token, "bob", bobPassword))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite redeem: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var redeemed inviteRedeemResponse
	mustDecode(t, rec, &redeemed)
	if redeemed.GroupID != registered.GroupID {
		t.Fatalf("bob's GroupID = %q, want alice's group %q -- an invite must seat the redeemer in the INVITE's own group", redeemed.GroupID, registered.GroupID)
	}

	if rec := srv.do(t, http.MethodPost, "/api/v1/invites/redeem", "", redeemBody(issued.Token, "carol", "does-not-matter-2")); rec.Code != http.StatusNotFound {
		t.Fatalf("re-redeeming an already-used invite: status = %d, want 404", rec.Code)
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("bob", bobPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("bob login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var bobLogin loginResponse
	mustDecode(t, rec, &bobLogin)
	if bobLogin.Role != "member" {
		t.Fatalf("bob's role = %q, want member (FR-007/A4: redemption never grants owner)", bobLogin.Role)
	}
	if bobLogin.GroupID != registered.GroupID {
		t.Fatalf("bob's login GroupID = %q, want %q", bobLogin.GroupID, registered.GroupID)
	}
	bobCookie := sessionCookie(t, rec)

	if rec := srv.do(t, http.MethodPost, "/api/v1/invites", bobCookie, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("bob (member) POST /invites: status = %d, want 403", rec.Code)
	}

	rec = srv.do(t, http.MethodGet, "/api/v1/auth/sessions", bobCookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bob list sessions: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var bobSessions sessionListResponse
	mustDecode(t, rec, &bobSessions)
	if len(bobSessions.Sessions) != 1 {
		t.Fatalf("bob's sessions = %+v, want exactly 1", bobSessions.Sessions)
	}
	bobSessionID := bobSessions.Sessions[0].ID

	rec = srv.do(t, http.MethodDelete, "/api/v1/auth/sessions/"+bobSessionID, bobCookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("bob revoke own session: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodGet, "/api/v1/auth/sessions", bobCookie, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bob's request with the just-revoked cookie: status = %d, want 401 -- the revoked credential must be refused on the very next request", rec.Code)
	}

	if rec := srv.do(t, http.MethodGet, "/api/v1/auth/sessions", aliceCookie, nil); rec.Code != http.StatusOK {
		t.Fatalf("alice's session after bob's revoke: status = %d, want 200 -- one member's session revocation must not touch another's", rec.Code)
	}
}
