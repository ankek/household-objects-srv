package middleware

import (
	"bytes"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureLogs() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func chain(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return RequestID(Log(logger)(Recover(logger)(MaxBody(0)(next))))
	}
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problem.Problem {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != problem.MediaType {
		t.Fatalf("Content-Type = %q, want %q", got, problem.MediaType)
	}
	var p problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	return p
}

const (
	secretBearer   = "SECRETBEARER-aG9tZS1kZXZpY2U"
	secretCookie   = "SECRETCOOKIE-c2Vzc2lvbi12YWw"
	secretQuery    = "SECRETQUERY-aW52aXRlLXRva2Vu"
	secretReqBody  = "SECRETBODY-YXR0YWNobWVudA"
	secretRespBody = "SECRETRESP-c2V0LWNvb2tpZQ"
)

func TestChainNeverLogsCredentials(t *testing.T) {
	logger, logs := captureLogs()

	h := chain(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("reading body: %v", err)
		}
		http.SetCookie(w, &http.Cookie{Name: "hho_session", Value: secretCookie})
		_, _ = io.WriteString(w, secretRespBody)
	}))

	req := httptest.NewRequest(http.MethodPost, "/invites/redeem?token="+secretQuery, strings.NewReader(secretReqBody))
	req.Header.Set("Authorization", "Bearer "+secretBearer)
	req.Header.Set("Cookie", "hho_session="+secretCookie)
	req.Header.Set("X-Request-ID", `injected","level":"INFO","msg":"admin login`)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %q", rec.Code, rec.Body.String())
	}

	got := logs.String()

	for _, want := range []string{`"msg":"http request"`, `"method":"POST"`, `"status":200`} {
		if !strings.Contains(got, want) {
			t.Fatalf("log is missing %s, so the absence assertions in this test would prove nothing: %q", want, got)
		}
	}

	for name, secret := range map[string]string{
		"Authorization header":       secretBearer,
		"Cookie / Set-Cookie value":  secretCookie,
		"query-string token":         secretQuery,
		"request body":               secretReqBody,
		"response body":              secretRespBody,
		"client-supplied request id": "injected",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("%s reached the log (NFR-018): %q", name, got)
		}
	}

	if strings.Contains(got, "/invites") || strings.Contains(got, "?") {
		t.Errorf("the log line carries a raw URL: %q", got)
	}
}
