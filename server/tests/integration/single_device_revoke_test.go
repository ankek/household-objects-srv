package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRevokeSessionAtHTTPLayerLeavesOtherSessionUsable(t *testing.T) {
	srv := newLiveServer(t, false)
	const password = "alice-correct-horse-battery-3"

	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", password)); rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d: %s", rec.Code, rec.Body.String())
	}

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login 1: status = %d: %s", rec.Code, rec.Body.String())
	}
	cookieA := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login 2: status = %d: %s", rec.Code, rec.Body.String())
	}
	cookieB := sessionCookie(t, rec)

	if cookieA == cookieB {
		t.Fatal("two separate logins minted the identical session cookie; this test needs two distinct sessions to mean anything")
	}

	rec = srv.do(t, http.MethodGet, "/api/v1/auth/sessions", cookieA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sessions before revoke: status = %d: %s", rec.Code, rec.Body.String())
	}
	var before sessionListResponse
	mustDecode(t, rec, &before)
	if len(before.Sessions) != 2 {
		t.Fatalf("session listing before revoke = %+v, want exactly 2 (both of alice's logins)", before.Sessions)
	}
	for _, s := range before.Sessions {
		if s.Revoked {
			t.Fatalf("a freshly minted session is already Revoked in the listing: %+v", before.Sessions)
		}
	}
	targetID := before.Sessions[0].ID
	otherID := before.Sessions[1].ID

	rec = srv.do(t, http.MethodDelete, "/api/v1/auth/sessions/"+targetID, cookieA, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke session %q: status = %d, want 204: %s", targetID, rec.Code, rec.Body.String())
	}

	recA := srv.do(t, http.MethodGet, "/api/v1/auth/sessions", cookieA, nil)
	recB := srv.do(t, http.MethodGet, "/api/v1/auth/sessions", cookieB, nil)

	var survivingRec *httptest.ResponseRecorder
	switch {
	case recA.Code == http.StatusUnauthorized && recB.Code == http.StatusOK:
		survivingRec = recB
	case recB.Code == http.StatusUnauthorized && recA.Code == http.StatusOK:
		survivingRec = recA
	default:
		t.Fatalf("after revoking one of alice's two sessions: cookieA status = %d, cookieB status = %d -- want exactly one 401 and one 200 (revoking one session must refuse only its own cookie, on the very next request)", recA.Code, recB.Code)
	}

	var after sessionListResponse
	mustDecode(t, survivingRec, &after)
	if len(after.Sessions) != 2 {
		t.Fatalf("session listing on the surviving cookie after the revoke = %+v, want exactly 2 (the revoke must not delete rows, only mark one revoked)", after.Sessions)
	}
	var sawTarget, sawOther bool
	for _, s := range after.Sessions {
		switch s.ID {
		case targetID:
			sawTarget = true
			if !s.Revoked {
				t.Errorf("targeted session %q reports Revoked = false after being revoked", targetID)
			}
		case otherID:
			sawOther = true
			if s.Revoked {
				t.Fatalf("the OTHER session %q reports Revoked = true after revoking a DIFFERENT session (%q) -- FR-004's isolation promise broke at the HTTP layer", otherID, targetID)
			}
		default:
			t.Errorf("unexpected session id %q in listing after revoke", s.ID)
		}
	}
	if !sawTarget || !sawOther {
		t.Fatalf("listing after revoke = %+v, want both %q (revoked) and %q (live) present", after.Sessions, targetID, otherID)
	}
}

func TestRevokeDeviceTokenAtHTTPLayerLeavesOtherDeviceTokenUsable(t *testing.T) {
	srv := newLiveServer(t, false)
	const password = "alice-correct-horse-battery-4"

	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", password)); rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d: %s", rec.Code, rec.Body.String())
	}
	rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/device-tokens", cookie, []byte(`{"device_label":"alice's phone"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue device token (phone): status = %d: %s", rec.Code, rec.Body.String())
	}
	var phone deviceTokenIssueResponse
	mustDecode(t, rec, &phone)

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/device-tokens", cookie, []byte(`{"device_label":"alice's tablet"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue device token (tablet): status = %d: %s", rec.Code, rec.Body.String())
	}
	var tablet deviceTokenIssueResponse
	mustDecode(t, rec, &tablet)

	if phone.Token == tablet.Token || phone.ID == tablet.ID {
		t.Fatalf("two separate issuances minted colliding credentials: phone=%+v tablet=%+v", phone, tablet)
	}

	if rec := srv.doBearer(t, http.MethodGet, "/api/v1/auth/device-tokens", phone.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("phone token before any revoke: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := srv.doBearer(t, http.MethodGet, "/api/v1/auth/device-tokens", tablet.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("tablet token before any revoke: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodDelete, "/api/v1/auth/device-tokens/"+phone.ID, cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke phone device token: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	if rec := srv.doBearer(t, http.MethodGet, "/api/v1/auth/device-tokens", phone.Token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("phone token immediately after its own revoke: status = %d, want 401", rec.Code)
	}

	rec = srv.doBearer(t, http.MethodGet, "/api/v1/auth/device-tokens", tablet.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("tablet token after revoking a DIFFERENT device token (phone): status = %d, want 200 -- FR-004's isolation promise broke at the HTTP layer", rec.Code)
	}
	var listing deviceTokenListResponse
	mustDecode(t, rec, &listing)
	var sawPhone, sawTablet bool
	for _, d := range listing.DeviceTokens {
		switch d.ID {
		case phone.ID:
			sawPhone = true
			if !d.Revoked {
				t.Errorf("phone device token %q reports Revoked = false after being revoked", phone.ID)
			}
		case tablet.ID:
			sawTablet = true
			if d.Revoked {
				t.Fatalf("tablet device token %q reports Revoked = true after revoking a DIFFERENT device token (phone)", tablet.ID)
			}
		}
	}
	if !sawPhone || !sawTablet {
		t.Fatalf("device-token listing (via the surviving tablet credential) = %+v, want both %q (revoked) and %q (live) present", listing.DeviceTokens, phone.ID, tablet.ID)
	}
}
