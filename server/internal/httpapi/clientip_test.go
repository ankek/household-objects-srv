package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPDefaultsToRemoteAddrRegardlessOfHeader(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.RemoteAddr = "203.0.113.9:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")

	if got := clientIP(r, false); got != "203.0.113.9" {
		t.Errorf("clientIP(trustProxyHeaders=false) = %q, want RemoteAddr's host %q regardless of X-Forwarded-For", got, "203.0.113.9")
	}
}

func TestClientIPHonoursForwardedForOnlyWhenOptedIn(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.RemoteAddr = "172.18.0.5:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")

	if got := clientIP(r, true); got != "198.51.100.1" {
		t.Errorf("clientIP(trustProxyHeaders=true) = %q, want the X-Forwarded-For entry %q, not the proxy's own RemoteAddr", got, "198.51.100.1")
	}
}

func TestClientIPFallsBackToRemoteAddrWhenHeaderAbsent(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.RemoteAddr = "203.0.113.9:54321"

	if got := clientIP(r, true); got != "203.0.113.9" {
		t.Errorf("clientIP(trustProxyHeaders=true, no header) = %q, want RemoteAddr's host %q", got, "203.0.113.9")
	}
}

func TestClientIPDiscardsAClientSuppliedPrefix(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.RemoteAddr = "203.0.113.9:54321"
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 203.0.113.9")

	got := clientIP(r, true)
	if got == "10.0.0.1" {
		t.Fatal("clientIP returned the client-supplied prefix, not the proxy-appended entry -- an unauthenticated caller can forge whoever they like")
	}
	if got != "203.0.113.9" {
		t.Errorf("clientIP = %q, want the proxy-appended entry %q", got, "203.0.113.9")
	}
}
