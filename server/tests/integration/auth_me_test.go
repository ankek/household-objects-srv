package integration

import (
	"net/http"
	"testing"
)

func TestAuthMeAtHTTPLayer(t *testing.T) {
	srv := newLiveServer(t, false)
	const alicePassword = "alice-correct-horse-battery-auth-me"
	const bobPassword = "bob-correct-horse-battery-auth-me"

	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", alicePassword)); rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d: %s", rec.Code, rec.Body.String())
	}
	rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", alicePassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("alice login: status = %d: %s", rec.Code, rec.Body.String())
	}
	var aliceLogin loginResponse
	mustDecode(t, rec, &aliceLogin)
	aliceCookie := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodGet, "/api/v1/auth/me", aliceCookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /auth/me as owner (cookie): status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var aliceMe loginResponse
	mustDecode(t, rec, &aliceMe)
	if aliceMe != aliceLogin {
		t.Fatalf("GET /auth/me as owner (cookie) = %+v, want it to match POST /auth/login's own response %+v", aliceMe, aliceLogin)
	}
	if aliceMe.Role != "owner" {
		t.Fatalf("GET /auth/me as owner: role = %q, want owner", aliceMe.Role)
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/device-tokens", aliceCookie, []byte(`{"device_label":"alice's phone"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue device token: status = %d: %s", rec.Code, rec.Body.String())
	}
	var deviceToken deviceTokenIssueResponse
	mustDecode(t, rec, &deviceToken)

	rec = srv.doBearer(t, http.MethodGet, "/api/v1/auth/me", deviceToken.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /auth/me as owner (bearer device token): status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var aliceMeBearer loginResponse
	mustDecode(t, rec, &aliceMeBearer)
	if aliceMeBearer != aliceLogin {
		t.Fatalf("GET /auth/me as owner (bearer device token) = %+v, want it to match the cookie-authenticated response %+v", aliceMeBearer, aliceLogin)
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/invites", aliceCookie, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite issue: status = %d: %s", rec.Code, rec.Body.String())
	}
	var issued inviteCreateResponse
	mustDecode(t, rec, &issued)

	rec = srv.do(t, http.MethodPost, "/api/v1/invites/redeem", "", redeemBody(issued.Token, "bob", bobPassword))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite redeem: status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("bob", bobPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("bob login: status = %d: %s", rec.Code, rec.Body.String())
	}
	var bobLogin loginResponse
	mustDecode(t, rec, &bobLogin)
	bobCookie := sessionCookie(t, rec)
	if bobLogin.Role != "member" {
		t.Fatalf("bob's role = %q, want member (invite redemption never grants owner)", bobLogin.Role)
	}

	rec = srv.do(t, http.MethodGet, "/api/v1/auth/me", bobCookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /auth/me as member (cookie): status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var bobMe loginResponse
	mustDecode(t, rec, &bobMe)
	if bobMe != bobLogin {
		t.Fatalf("GET /auth/me as member (cookie) = %+v, want it to match POST /auth/login's own response %+v", bobMe, bobLogin)
	}
	if bobMe.GroupID != aliceLogin.GroupID {
		t.Fatalf("bob's GroupID via GET /auth/me = %q, want alice's group %q (invite redemption seats the redeemer in the invite's own group)", bobMe.GroupID, aliceLogin.GroupID)
	}

	rec = srv.do(t, http.MethodGet, "/api/v1/auth/me", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /auth/me with no credential: status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}
