package httpapi

import (
	"archive/tar"
	"compress/gzip"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func backupTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(t.TempDir(), "db", "hho.db"), ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func backupConfig(t *testing.T) Config {
	t.Helper()
	cfg := testConfig()
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	cfg.Store = backupTestStorage(t)
	cfg.DataDir = t.TempDir()
	return cfg
}

func TestBackupHandlerStreamsAValidArchiveForTheOwner(t *testing.T) {
	h, err := NewRouter(backupConfig(t))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/backup", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != backupContentType {
		t.Errorf("Content-Type = %q, want %q", got, backupContentType)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, ".tar.gz") {
		t.Errorf("Content-Disposition = %q, want it to declare an attachment with a .tar.gz filename", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "private, no-store")
	}

	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("response body is not a valid gzip stream: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("read first tar entry: %v", err)
	}
	if hdr.Name != "manifest.json" {
		t.Errorf("first tar entry = %q, want %q", hdr.Name, "manifest.json")
	}
	if _, err := io.ReadAll(tr); err != nil {
		t.Errorf("read manifest.json entry body: %v", err)
	}
}

func TestBackupHandlerRejectsMembers(t *testing.T) {
	h, err := NewRouter(backupConfig(t))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/backup", nil), "member")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestBackupHandlerRejectsUnauthenticated(t *testing.T) {
	h, err := NewRouter(backupConfig(t))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backup", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestBackupHandlerWithNoStoreConfiguredAnswersInternalWithAProblemBody(t *testing.T) {
	cfg := backupConfig(t)
	cfg.Store = nil
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, "/api/v1/backup", nil), "owner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Status != http.StatusInternalServerError {
		t.Errorf("problem.Status = %d, want %d", p.Status, http.StatusInternalServerError)
	}
}
