package httpapi

import (
	"bytes"
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const importUploadTestGroup = "grp-import-upload"

func importUploadConfig(t *testing.T) (cfg Config, dbPath string) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "db", "hho.db")
	s, err := storage.Open(t.Context(), storage.Config{Path: dbPath, ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	if err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      importUploadTestGroup,
		GroupName:    importUploadTestGroup,
		UserID:       importUploadTestGroup + "-owner",
		Username:     importUploadTestGroup + "-owner",
		PasswordHash: "not-a-real-hash",
		Now:          1,
	}); err != nil {
		t.Fatalf("RegisterNewGroup(%s): %v", importUploadTestGroup, err)
	}

	root := t.TempDir()
	if err := datadir.Ensure(root); err != nil {
		t.Fatalf("datadir.Ensure(%s): %v", root, err)
	}

	cfg = testConfig()
	cfg.Authenticator = middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
		return middleware.Identity{Group: importUploadTestGroup, UserID: importUploadTestGroup + "-owner", Role: "owner"}, nil
	})
	cfg.Store = s
	cfg.Scopes = s
	cfg.DataDir = root
	cfg.MaxImportBytes = importexport.DefaultMaxUploadBytes
	return cfg, dbPath
}

func validImportCSV() []byte {
	header := strings.Join(importexport.FixedColumns, ",")
	row := "item-1,Lawnmower,,1,,,SHORT1,,,,,,,,,,,,,,,,,,"
	return []byte(header + "\n" + row + "\n")
}

func doImportUpload(t *testing.T, h http.Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import/native/upload", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type importSessionRow struct {
	groupID   string
	source    string
	status    string
	createdAt int64
	expiresAt int64
}

func readImportSession(t *testing.T, dbPath, importID string) (importSessionRow, bool) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	var row importSessionRow
	err = db.QueryRowContext(t.Context(),
		`SELECT group_id, source, status, created_at, expires_at FROM import_sessions WHERE id = ?`, importID,
	).Scan(&row.groupID, &row.source, &row.status, &row.createdAt, &row.expiresAt)
	switch {
	case err == sql.ErrNoRows:
		return importSessionRow{}, false
	case err != nil:
		t.Fatalf("query import_sessions: %v", err)
	}
	return row, true
}

func countImportSessions(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM import_sessions`).Scan(&n); err != nil {
		t.Fatalf("count import_sessions: %v", err)
	}
	return n
}

func TestImportNativeUploadSucceeds(t *testing.T) {
	cfg, dbPath := importUploadConfig(t)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	content := validImportCSV()
	rec := doImportUpload(t, h, content)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	body := decodeBody(t, rec)
	importID, _ := body["import_id"].(string)
	if importID == "" {
		t.Fatal("response carries no import_id")
	}

	stagedPath := importexport.StagingPath(cfg.DataDir, importUploadTestGroup, importID)
	got, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", stagedPath, err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("staged bytes = %q, want %q", got, content)
	}

	row, ok := readImportSession(t, dbPath, importID)
	if !ok {
		t.Fatalf("no import_sessions row for %q", importID)
	}
	if row.groupID != importUploadTestGroup {
		t.Errorf("group_id = %q, want %q", row.groupID, importUploadTestGroup)
	}
	if row.source != importexport.SourceNative {
		t.Errorf("source = %q, want %q", row.source, importexport.SourceNative)
	}
	if row.status != importexport.StatusStaged {
		t.Errorf("status = %q, want %q", row.status, importexport.StatusStaged)
	}
	wantExpires := row.createdAt + storage.ImportSessionTTL.Milliseconds()
	if row.expiresAt != wantExpires {
		t.Errorf("expires_at = %d, want %d (created_at + ImportSessionTTL)", row.expiresAt, wantExpires)
	}
}

func TestImportNativeUploadRejectsAMalformedUpload(t *testing.T) {
	cfg, dbPath := importUploadConfig(t)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doImportUpload(t, h, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if n := countImportSessions(t, dbPath); n != 0 {
		t.Errorf("import_sessions has %d rows after a rejected malformed upload, want 0", n)
	}

	entries, err := os.ReadDir(filepath.Join(cfg.DataDir, datadir.TmpSubdir))
	if err != nil {
		t.Fatalf("ReadDir(tmp): %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("tmp/ has %d leftover entries after a rejected malformed upload, want 0: %v", len(entries), names)
	}
}

func TestImportNativeUploadRejectsAnOversizeBody(t *testing.T) {
	cfg, _ := importUploadConfig(t)
	cfg.MaxImportBytes = 8

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doImportUpload(t, h, validImportCSV())
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}

	p := decodeProblem(t, rec)
	if p.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("problem status = %d, want 413", p.Status)
	}
}

func TestImportNativeUploadRejectsAnUnauthenticatedRequest(t *testing.T) {
	cfg, _ := importUploadConfig(t)
	cfg.Authenticator = rejectingAuth

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doImportUpload(t, h, validImportCSV())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestMaxImportBytesDefaultsWhenUnconfigured(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		if got := maxImportBytes(Config{MaxImportBytes: limit}); got != importexport.DefaultMaxUploadBytes {
			t.Errorf("maxImportBytes(Config{MaxImportBytes: %d}) = %d, want %d (DefaultMaxUploadBytes)", limit, got, importexport.DefaultMaxUploadBytes)
		}
	}
}
