package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/devicetoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDeviceTokenService struct {
	result     devicetoken.Issued
	err        error
	gotRequest devicetoken.IssueRequest
}

func (f *fakeDeviceTokenService) Issue(_ context.Context, req devicetoken.IssueRequest) (devicetoken.Issued, error) {
	f.gotRequest = req
	return f.result, f.err
}

const fakeCookieName = "test_session"

var fakeCookieOnlyAuth = middleware.AuthenticatorFunc(func(r *http.Request) (middleware.Identity, error) {
	c, err := r.Cookie(fakeCookieName)
	if err != nil || c.Value == "" {
		return middleware.Identity{}, fmt.Errorf("no session cookie: %w", middleware.ErrUnauthenticated)
	}
	return middleware.Identity{Group: testGroup, UserID: "usr-test", Role: "owner"}, nil
})

var fakeBearerAcceptingAuth = middleware.AuthenticatorFunc(func(r *http.Request) (middleware.Identity, error) {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return middleware.Identity{Group: testGroup, UserID: "usr-device", Role: "member"}, nil
	}
	return middleware.Identity{}, fmt.Errorf("no bearer token: %w", middleware.ErrUnauthenticated)
})

func deviceTokenConfig(svc DeviceTokenIssuer) Config {
	cfg := testConfig()
	cfg.SessionAuthenticator = fakeCookieOnlyAuth
	cfg.DeviceTokenService = svc
	return cfg
}

func postDeviceTokenWithCookie(t *testing.T, h http.Handler, body []byte, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: fakeCookieName, Value: cookieValue})
	}
	h.ServeHTTP(rec, req)
	return rec
}

func TestDeviceTokenIssueHandlerHappyPath(t *testing.T) {
	svc := &fakeDeviceTokenService{result: devicetoken.Issued{ID: "dvc-1", DeviceLabel: "Sarah's Pixel", Token: "raw-token-value"}}
	h, err := NewRouter(deviceTokenConfig(svc))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	body, err := json.Marshal(deviceTokenIssueRequestBody{DeviceLabel: "Sarah's Pixel"})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	rec := postDeviceTokenWithCookie(t, h, body, "valid-session-cookie")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var got deviceTokenIssueResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if got.ID != "dvc-1" || got.DeviceLabel != "Sarah's Pixel" || got.Token != "raw-token-value" {
		t.Errorf("response body = %+v, want {dvc-1 Sarah's Pixel raw-token-value}", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}

	if svc.gotRequest.GroupID != testGroup {
		t.Errorf("Issue was called with GroupID = %q, want %q (the authenticated identity's group)", svc.gotRequest.GroupID, testGroup)
	}
	if svc.gotRequest.UserID != "usr-test" {
		t.Errorf("Issue was called with UserID = %q, want %q", svc.gotRequest.UserID, "usr-test")
	}
	if svc.gotRequest.DeviceLabel != "Sarah's Pixel" {
		t.Errorf("Issue was called with DeviceLabel = %q, want %q", svc.gotRequest.DeviceLabel, "Sarah's Pixel")
	}
}

func TestDeviceTokenRouteUnmountedWithoutBothDependencies(t *testing.T) {
	body, err := json.Marshal(deviceTokenIssueRequestBody{DeviceLabel: "x"})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	t.Run("no SessionAuthenticator", func(t *testing.T) {
		cfg := testConfig()
		cfg.DeviceTokenService = &fakeDeviceTokenService{}
		h, err := NewRouter(cfg)
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		rec := postDeviceTokenWithCookie(t, h, body, "whatever")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d (route not mounted)", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("no DeviceTokenService", func(t *testing.T) {
		cfg := testConfig()
		cfg.SessionAuthenticator = fakeCookieOnlyAuth
		h, err := NewRouter(cfg)
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		rec := postDeviceTokenWithCookie(t, h, body, "whatever")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d (route not mounted)", rec.Code, http.StatusNotFound)
		}
	})
}

func TestDeviceTokenRouteRejectsABearerOnlyRequest(t *testing.T) {
	svc := &fakeDeviceTokenService{result: devicetoken.Issued{ID: "dvc-1", DeviceLabel: "x", Token: "t"}}
	cfg := deviceTokenConfig(svc)
	cfg.Authenticator = fakeBearerAcceptingAuth
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	body, err := json.Marshal(deviceTokenIssueRequestBody{DeviceLabel: "x"})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer some-device-token-value")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (a bearer-only request must not reach device-token issuance)", rec.Code, http.StatusUnauthorized)
	}
	if svc.gotRequest != (devicetoken.IssueRequest{}) {
		t.Fatalf("Issue was called with %+v, want it never called", svc.gotRequest)
	}
}

func TestDeviceTokenRouteRejectsNoCredentialAtAll(t *testing.T) {
	svc := &fakeDeviceTokenService{}
	h, err := NewRouter(deviceTokenConfig(svc))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	body, err := json.Marshal(deviceTokenIssueRequestBody{DeviceLabel: "x"})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	rec := postDeviceTokenWithCookie(t, h, body, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestDeviceTokenIssueHandlerRejectsInvalidJSON(t *testing.T) {
	svc := &fakeDeviceTokenService{}
	h, err := NewRouter(deviceTokenConfig(svc))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := postDeviceTokenWithCookie(t, h, []byte("not json"), "valid-session-cookie")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDeviceTokenIssueHandlerMapsDeviceLabelErrorsTo400(t *testing.T) {
	svc := &fakeDeviceTokenService{err: devicetoken.ErrDeviceLabelRequired}
	h, err := NewRouter(deviceTokenConfig(svc))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	body, err := json.Marshal(deviceTokenIssueRequestBody{DeviceLabel: ""})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	rec := postDeviceTokenWithCookie(t, h, body, "valid-session-cookie")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
