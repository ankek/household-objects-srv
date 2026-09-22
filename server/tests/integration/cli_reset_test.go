package integration

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCLIResetPasswordIsRejectedOnTheVeryNextHTTPRequest(t *testing.T) {
	binPath := buildHHOBinary(t)

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "db", "hho.db")
	srv := newLiveServerAtPath(t, dbPath, false)

	const oldPassword = "alice-original-password-1"
	const newPassword = "alice-operator-reset-password-2"

	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", oldPassword)); rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d: %s", rec.Code, rec.Body.String())
	}
	rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", oldPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	if rec := srv.do(t, http.MethodGet, "/api/v1/auth/sessions", cookie, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /auth/sessions before CLI reset: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "user", "reset-password", "--username", "alice", "--data-dir", dataDir)
	cmd.Stdin = strings.NewReader(newPassword + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("hho user reset-password: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Password reset for user") {
		t.Fatalf("unexpected CLI output: %s", stdout.String())
	}

	if rec := srv.do(t, http.MethodGet, "/api/v1/auth/sessions", cookie, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /auth/sessions with the pre-reset cookie: status = %d, want 401 -- the CLI's reset must revoke the session and the HTTP layer must honour that on the very next request", rec.Code)
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", newPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("login with the CLI-reset password: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", oldPassword)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with the OLD, reset password: status = %d, want 401", rec.Code)
	}
}

func buildHHOBinary(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH, so the real hho binary cannot be built for this cross-task test: %v", err)
	}

	moduleRoot := findServerModuleRoot(t)
	binPath := filepath.Join(t.TempDir(), "hho")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", binPath, "./cmd/hho")
	cmd.Dir = moduleRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/hho: %v\n%s", err, out)
	}
	return binPath
}

func findServerModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}
