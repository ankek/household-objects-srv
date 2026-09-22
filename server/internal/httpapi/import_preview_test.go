package httpapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
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
	"time"
)

func importPreviewConfig(t *testing.T) (cfg Config, store *storage.Storage, dbPath string) {
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

	root := t.TempDir()
	if err := datadir.Ensure(root); err != nil {
		t.Fatalf("datadir.Ensure(%s): %v", root, err)
	}

	cfg = testConfig()
	cfg.Authenticator = middleware.AuthenticatorFunc(func(r *http.Request) (middleware.Identity, error) {
		group := r.Header.Get("X-Test-Group")
		if group == "" {
			return middleware.Identity{}, middleware.ErrUnauthenticated
		}
		return middleware.Identity{Group: group, UserID: group + "-owner", Role: "owner"}, nil
	})
	cfg.Store = s
	cfg.Scopes = s
	cfg.DataDir = root
	cfg.MaxImportBytes = importexport.DefaultMaxUploadBytes
	return cfg, s, dbPath
}

func registerImportPreviewGroup(t *testing.T, s *storage.Storage, groupID string) {
	t.Helper()
	if err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    groupID,
		UserID:       groupID + "-owner",
		Username:     groupID + "-owner",
		PasswordHash: "not-a-real-hash",
		Now:          1,
	}); err != nil {
		t.Fatalf("RegisterNewGroup(%s): %v", groupID, err)
	}
}

func createPreviewItem(t *testing.T, s *storage.Storage, groupID, id, name, shortCode string, quantity int64) storage.Item {
	t.Helper()
	scope, err := s.ForGroup(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%s): %v", groupID, err)
	}
	item, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID:        id,
		Name:      name,
		Quantity:  quantity,
		ShortCode: shortCode,
		Now:       1000,
	})
	if err != nil {
		t.Fatalf("Items().Create(%s): %v", id, err)
	}
	return item
}

func csvRowFrom(values map[string]string) string {
	cells := make([]string, len(importexport.FixedColumns))
	for i, col := range importexport.FixedColumns {
		cells[i] = values[col]
	}
	return strings.Join(cells, ",")
}

func stageImportSession(t *testing.T, cfg Config, s *storage.Storage, groupID string, csv []byte, now int64) string {
	t.Helper()
	importID := "import-" + groupID + "-" + time.Now().Format("150405.000000000")
	if err := importexport.Stage(cfg.DataDir, groupID, importID, bytes.NewReader(csv), cfg.MaxImportBytes); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	sessions, err := s.ForGroupImportSessions(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupImportSessions(%s): %v", groupID, err)
	}
	if _, err := sessions.Create(t.Context(), storage.CreateImportSessionParams{
		ID:     importID,
		Source: importexport.SourceNative,
		Now:    now,
	}); err != nil {
		t.Fatalf("sessions.Create: %v", err)
	}
	return importID
}

func setImportSessionStatus(t *testing.T, dbPath, importID, status string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), `UPDATE import_sessions SET status = ? WHERE id = ?`, status, importID); err != nil {
		t.Fatalf("UPDATE import_sessions: %v", err)
	}
}

func importSessionExists(t *testing.T, dbPath, importID string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM import_sessions WHERE id = ?`, importID).Scan(&n); err != nil {
		t.Fatalf("count import_sessions: %v", err)
	}
	return n > 0
}

func readItemVersionAndUpdatedAt(t *testing.T, dbPath, itemID string) (version, updatedAt int64) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	if err := db.QueryRowContext(t.Context(), `SELECT version, updated_at FROM items WHERE id = ?`, itemID).Scan(&version, &updatedAt); err != nil {
		t.Fatalf("query items: %v", err)
	}
	return version, updatedAt
}

func readGroupChangeSeq(t *testing.T, dbPath, groupID string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	var seq int64
	if err := db.QueryRowContext(t.Context(), `SELECT change_seq_counter FROM groups WHERE id = ?`, groupID).Scan(&seq); err != nil {
		t.Fatalf("query groups: %v", err)
	}
	return seq
}

func doImportPreview(t *testing.T, h http.Handler, group, importID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/import/"+importID+"/preview", nil)
	req.Header.Set("X-Test-Group", group)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type previewRowDTO struct {
	Line    int      `json:"line"`
	Action  string   `json:"action"`
	ItemID  string   `json:"item_id"`
	Name    string   `json:"name"`
	Changes []string `json:"changes"`
	Errors  []struct {
		Line    int    `json:"line"`
		Column  string `json:"column"`
		Message string `json:"message"`
	} `json:"errors"`
}

type previewResponseDTO struct {
	ImportID string          `json:"import_id"`
	Rows     []previewRowDTO `json:"rows"`
	Summary  struct {
		Create    int `json:"create"`
		Update    int `json:"update"`
		Unchanged int `json:"unchanged"`
		Error     int `json:"error"`
	} `json:"summary"`
}

func decodePreview(t *testing.T, rec *httptest.ResponseRecorder) previewResponseDTO {
	t.Helper()
	var resp previewResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode preview response: %v; body: %s", err, rec.Body.String())
	}
	return resp
}

func TestImportPreviewClassifiesAMixedFileWithLineNumbers(t *testing.T) {
	cfg, s, _ := importPreviewConfig(t)
	const group = "grp-preview-mixed"
	registerImportPreviewGroup(t, s, group)

	createPreviewItem(t, s, group, "item-upd", "Old Name", "UPD001", 1)
	createPreviewItem(t, s, group, "item-same", "Same Name", "SAM001", 2)

	header := strings.Join(importexport.FixedColumns, ",")
	rowCreate := csvRowFrom(map[string]string{
		importexport.ColumnName: "Brand New Item", importexport.ColumnQuantity: "5", importexport.ColumnShortCode: "NEW001",
	})
	rowUpdate := csvRowFrom(map[string]string{
		importexport.ColumnID: "item-upd", importexport.ColumnName: "New Name", importexport.ColumnQuantity: "1", importexport.ColumnShortCode: "UPD001",
	})
	rowUnchanged := csvRowFrom(map[string]string{
		importexport.ColumnID: "item-same", importexport.ColumnName: "Same Name", importexport.ColumnQuantity: "2", importexport.ColumnShortCode: "SAM001",
	})
	rowError := csvRowFrom(map[string]string{
		importexport.ColumnQuantity: "not-a-number",
	})
	csvBody := strings.Join([]string{header, rowCreate, rowUpdate, rowUnchanged, rowError}, "\n") + "\n"

	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportPreview(t, h, group, importID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	resp := decodePreview(t, rec)
	if resp.ImportID != importID {
		t.Errorf("import_id = %q, want %q", resp.ImportID, importID)
	}
	if len(resp.Rows) != 4 {
		t.Fatalf("len(Rows) = %d, want 4: %+v", len(resp.Rows), resp.Rows)
	}

	wantLines := []int{2, 3, 4, 5}
	wantActions := []string{"create", "update", "unchanged", "error"}
	for i, row := range resp.Rows {
		if row.Line != wantLines[i] {
			t.Errorf("Rows[%d].Line = %d, want %d", i, row.Line, wantLines[i])
		}
		if row.Action != wantActions[i] {
			t.Errorf("Rows[%d].Action = %q, want %q", i, row.Action, wantActions[i])
		}
	}

	if resp.Rows[1].ItemID != "item-upd" {
		t.Errorf("update row ItemID = %q, want %q", resp.Rows[1].ItemID, "item-upd")
	}
	if len(resp.Rows[1].Changes) != 1 || resp.Rows[1].Changes[0] != "name" {
		t.Errorf("update row Changes = %v, want exactly [\"name\"]", resp.Rows[1].Changes)
	}
	if resp.Rows[2].ItemID != "item-same" {
		t.Errorf("unchanged row ItemID = %q, want %q", resp.Rows[2].ItemID, "item-same")
	}
	if len(resp.Rows[3].Errors) == 0 {
		t.Error("error row carries no Errors")
	}

	if resp.Summary.Create != 1 || resp.Summary.Update != 1 || resp.Summary.Unchanged != 1 || resp.Summary.Error != 1 {
		t.Errorf("Summary = %+v, want {1,1,1,1}", resp.Summary)
	}
}

func TestImportPreviewWritesNothing(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-preview-nowrite"
	registerImportPreviewGroup(t, s, group)

	createPreviewItem(t, s, group, "item-upd", "Old Name", "UPD001", 1)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{
		importexport.ColumnID: "item-upd", importexport.ColumnName: "A Totally Different Name", importexport.ColumnQuantity: "1", importexport.ColumnShortCode: "UPD001",
	})
	csvBody := header + "\n" + row + "\n"
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())

	beforeVersion, beforeUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, "item-upd")
	beforeSeq := readGroupChangeSeq(t, dbPath, group)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportPreview(t, h, group, importID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	resp := decodePreview(t, rec)
	if len(resp.Rows) != 1 || resp.Rows[0].Action != "update" {
		t.Fatalf("preview did not classify the row as an update: %+v", resp.Rows)
	}

	afterVersion, afterUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, "item-upd")
	afterSeq := readGroupChangeSeq(t, dbPath, group)

	if beforeVersion != afterVersion {
		t.Errorf("item version changed: before %d, after %d", beforeVersion, afterVersion)
	}
	if beforeUpdatedAt != afterUpdatedAt {
		t.Errorf("item updated_at changed: before %d, after %d", beforeUpdatedAt, afterUpdatedAt)
	}
	if beforeSeq != afterSeq {
		t.Errorf("group change_seq_counter changed: before %d, after %d", beforeSeq, afterSeq)
	}
}

func TestImportPreviewExpiredSessionIs404AndIsDeleted(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-preview-expired"
	registerImportPreviewGroup(t, s, group)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{importexport.ColumnName: "Whatever", importexport.ColumnShortCode: "X1"})
	csvBody := header + "\n" + row + "\n"

	longAgo := time.Now().Add(-2 * time.Hour).UnixMilli()
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), longAgo)
	stagedPath := importexport.StagingPath(cfg.DataDir, group, importID)
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("staged file missing before the test even runs: %v", err)
	}

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportPreview(t, h, group, importID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}

	if importSessionExists(t, dbPath, importID) {
		t.Error("import_sessions row still exists after previewing an expired session, want it deleted")
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Errorf("staged file still exists after previewing an expired session (err = %v), want it deleted", err)
	}
}

func TestImportPreviewMissingStagedFileIs404AndDeletesTheRow(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-preview-a127"
	registerImportPreviewGroup(t, s, group)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{importexport.ColumnName: "Whatever", importexport.ColumnShortCode: "X1"})
	csvBody := header + "\n" + row + "\n"
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())

	if err := os.Remove(importexport.StagingPath(cfg.DataDir, group, importID)); err != nil {
		t.Fatalf("Remove staged file: %v", err)
	}

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportPreview(t, h, group, importID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if importSessionExists(t, dbPath, importID) {
		t.Error("import_sessions row still exists after previewing a session whose staged file is missing (A127), want it deleted")
	}
}

func TestImportPreviewAnotherGroupsImportIDIs404(t *testing.T) {
	cfg, s, _ := importPreviewConfig(t)
	const groupA, groupB = "grp-preview-tenant-a", "grp-preview-tenant-b"
	registerImportPreviewGroup(t, s, groupA)
	registerImportPreviewGroup(t, s, groupB)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{importexport.ColumnName: "Whatever", importexport.ColumnShortCode: "X1"})
	csvBody := header + "\n" + row + "\n"
	importID := stageImportSession(t, cfg, s, groupA, []byte(csvBody), time.Now().UnixMilli())

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportPreview(t, h, groupB, importID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestImportPreviewUnknownImportIDIs404(t *testing.T) {
	cfg, s, _ := importPreviewConfig(t)
	const group = "grp-preview-unknown"
	registerImportPreviewGroup(t, s, group)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportPreview(t, h, group, "does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestImportPreviewCommittedSessionIs409(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-preview-committed"
	registerImportPreviewGroup(t, s, group)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{importexport.ColumnName: "Whatever", importexport.ColumnShortCode: "X1"})
	csvBody := header + "\n" + row + "\n"
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())
	setImportSessionStatus(t, dbPath, importID, importexport.StatusCommitted)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportPreview(t, h, group, importID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !importSessionExists(t, dbPath, importID) {
		t.Error("import_sessions row was deleted for a committed session, want it left untouched")
	}
}

func TestImportPreviewRejectsAnUnauthenticatedRequest(t *testing.T) {
	cfg, s, _ := importPreviewConfig(t)
	const group = "grp-preview-unauth"
	registerImportPreviewGroup(t, s, group)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/import/whatever/preview", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}
