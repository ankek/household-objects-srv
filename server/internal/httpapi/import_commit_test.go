package httpapi

import (
	"database/sql"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func doImportCommit(t *testing.T, h http.Handler, group, importID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import/"+importID+"/commit", nil)
	req.Header.Set("X-Test-Group", group)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type commitCreatedItemDTO struct {
	Line   int    `json:"line"`
	ItemID string `json:"item_id"`
}

type commitResponseDTO struct {
	ImportID string `json:"import_id"`
	Summary  struct {
		Created   int `json:"created"`
		Updated   int `json:"updated"`
		Unchanged int `json:"unchanged"`
	} `json:"summary"`
	CreatedItems []commitCreatedItemDTO `json:"created_items"`
}

func decodeCommit(t *testing.T, rec *httptest.ResponseRecorder) commitResponseDTO {
	t.Helper()
	var resp commitResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode commit response: %v; body: %s", err, rec.Body.String())
	}
	return resp
}

func decodeCommitRejected(t *testing.T, rec *httptest.ResponseRecorder) previewResponseDTO {
	t.Helper()
	var resp previewResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode rejected commit response: %v; body: %s", err, rec.Body.String())
	}
	return resp
}

func readItemCoreFields(t *testing.T, dbPath, itemID string) (name string, quantity int64, shortCode string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	if err := db.QueryRowContext(t.Context(), `SELECT name, quantity, short_code FROM items WHERE id = ?`, itemID).Scan(&name, &quantity, &shortCode); err != nil {
		t.Fatalf("query items: %v", err)
	}
	return name, quantity, shortCode
}

func TestImportCommitMixedFileAppliesCreateUpdateUnchanged(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-commit-http-mixed"
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
	csvBody := strings.Join([]string{header, rowCreate, rowUpdate, rowUnchanged}, "\n") + "\n"
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())

	beforeUnchangedVersion, beforeUnchangedUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, "item-same")

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportCommit(t, h, group, importID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommit(t, rec)
	if resp.ImportID != importID {
		t.Errorf("import_id = %q, want %q", resp.ImportID, importID)
	}
	if resp.Summary.Created != 1 || resp.Summary.Updated != 1 || resp.Summary.Unchanged != 1 {
		t.Fatalf("Summary = %+v, want {1,1,1}", resp.Summary)
	}
	if len(resp.CreatedItems) != 1 || resp.CreatedItems[0].Line != 2 || resp.CreatedItems[0].ItemID == "" {
		t.Fatalf("CreatedItems = %+v, want one entry for line 2 with a non-empty item_id", resp.CreatedItems)
	}

	updatedVersion, _ := readItemVersionAndUpdatedAt(t, dbPath, "item-upd")
	if updatedVersion != 2 {
		t.Errorf("item-upd version = %d, want 2 (bumped once)", updatedVersion)
	}
	afterUnchangedVersion, afterUnchangedUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, "item-same")
	if afterUnchangedVersion != beforeUnchangedVersion || afterUnchangedUpdatedAt != beforeUnchangedUpdatedAt {
		t.Errorf("item-same changed by a commit that classified it unchanged: version %d->%d, updated_at %d->%d",
			beforeUnchangedVersion, afterUnchangedVersion, beforeUnchangedUpdatedAt, afterUnchangedUpdatedAt)
	}
}

func TestImportCommitRejectsFileWithErrorRowAndWritesNothing(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-commit-http-error-row"
	registerImportPreviewGroup(t, s, group)

	createPreviewItem(t, s, group, "item-upd", "Old Name", "UPD001", 1)

	header := strings.Join(importexport.FixedColumns, ",")
	rowGood := csvRowFrom(map[string]string{
		importexport.ColumnID: "item-upd", importexport.ColumnName: "New Name", importexport.ColumnQuantity: "1", importexport.ColumnShortCode: "UPD001",
	})
	rowBad := csvRowFrom(map[string]string{importexport.ColumnQuantity: "not-a-number"})
	csvBody := strings.Join([]string{header, rowGood, rowBad}, "\n") + "\n"
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())

	beforeVersion, beforeUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, "item-upd")
	beforeSeq := readGroupChangeSeq(t, dbPath, group)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportCommit(t, h, group, importID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}

	resp := decodeCommitRejected(t, rec)
	if resp.Summary.Error != 1 {
		t.Errorf("rejected response Summary.Error = %d, want 1: %+v", resp.Summary.Error, resp.Summary)
	}
	if len(resp.Rows) != 2 || resp.Rows[1].Action != "error" || len(resp.Rows[1].Errors) == 0 {
		t.Fatalf("rejected response Rows = %+v, want row 2 classified error with at least one Errors entry", resp.Rows)
	}

	afterVersion, afterUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, "item-upd")
	if afterVersion != beforeVersion || afterUpdatedAt != beforeUpdatedAt {
		t.Errorf("item-upd changed even though the whole file was rejected: version %d->%d, updated_at %d->%d",
			beforeVersion, afterVersion, beforeUpdatedAt, afterUpdatedAt)
	}
	afterSeq := readGroupChangeSeq(t, dbPath, group)
	if afterSeq != beforeSeq {
		t.Errorf("group change_seq_counter changed even though the whole file was rejected: %d -> %d", beforeSeq, afterSeq)
	}

	if !importSessionExists(t, dbPath, importID) {
		t.Error("import session was deleted after a rejected commit, want it left staged")
	}
}

func TestImportCommitReimportAfterCommitGivesAllUnchanged(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-commit-http-reimport"
	registerImportPreviewGroup(t, s, group)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{
		importexport.ColumnName: "Round Trip Item", importexport.ColumnQuantity: "7", importexport.ColumnShortCode: "RT00001",
	})
	firstBody := header + "\n" + row + "\n"
	firstImportID := stageImportSession(t, cfg, s, group, []byte(firstBody), time.Now().UnixMilli())

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportCommit(t, h, group, firstImportID)
	if rec.Code != http.StatusOK {
		t.Fatalf("first commit status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	firstResp := decodeCommit(t, rec)
	if firstResp.Summary.Created != 1 {
		t.Fatalf("first commit Summary = %+v, want Created:1", firstResp.Summary)
	}
	itemID := firstResp.CreatedItems[0].ItemID

	name, quantity, shortCode := readItemCoreFields(t, dbPath, itemID)
	beforeVersion, beforeUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, itemID)
	beforeSeq := readGroupChangeSeq(t, dbPath, group)

	secondRow := csvRowFrom(map[string]string{
		importexport.ColumnID: itemID, importexport.ColumnName: name,
		importexport.ColumnQuantity: itoa(quantity), importexport.ColumnShortCode: shortCode,
	})
	secondBody := header + "\n" + secondRow + "\n"
	secondImportID := stageImportSession(t, cfg, s, group, []byte(secondBody), time.Now().UnixMilli())

	rec = doImportCommit(t, h, group, secondImportID)
	if rec.Code != http.StatusOK {
		t.Fatalf("second commit status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	secondResp := decodeCommit(t, rec)
	if secondResp.Summary.Unchanged != 1 || secondResp.Summary.Created != 0 || secondResp.Summary.Updated != 0 {
		t.Fatalf("second commit Summary = %+v, want {Created:0 Updated:0 Unchanged:1}", secondResp.Summary)
	}

	afterVersion, afterUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, itemID)
	if afterVersion != beforeVersion || afterUpdatedAt != beforeUpdatedAt {
		t.Errorf("re-importing an unchanged row wrote something: version %d->%d, updated_at %d->%d",
			beforeVersion, afterVersion, beforeUpdatedAt, afterUpdatedAt)
	}
	afterSeq := readGroupChangeSeq(t, dbPath, group)
	if afterSeq != beforeSeq {
		t.Errorf("group change_seq_counter changed on an all-unchanged re-import: %d -> %d", beforeSeq, afterSeq)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestImportCommitTwiceGives409(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-commit-http-twice"
	registerImportPreviewGroup(t, s, group)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{importexport.ColumnName: "Whatever", importexport.ColumnShortCode: "X1"})
	csvBody := header + "\n" + row + "\n"
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportCommit(t, h, group, importID)
	if rec.Code != http.StatusOK {
		t.Fatalf("first commit status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	first := decodeCommit(t, rec)
	beforeVersion, beforeUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, first.CreatedItems[0].ItemID)
	beforeSeq := readGroupChangeSeq(t, dbPath, group)

	rec = doImportCommit(t, h, group, importID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second commit status = %d, want 409: %s", rec.Code, rec.Body.String())
	}

	afterVersion, afterUpdatedAt := readItemVersionAndUpdatedAt(t, dbPath, first.CreatedItems[0].ItemID)
	if afterVersion != beforeVersion || afterUpdatedAt != beforeUpdatedAt {
		t.Error("second commit of an already-committed session changed the item it created the first time")
	}
	afterSeq := readGroupChangeSeq(t, dbPath, group)
	if afterSeq != beforeSeq {
		t.Errorf("group change_seq_counter changed on a second, rejected commit: %d -> %d", beforeSeq, afterSeq)
	}
}

func TestImportCommitForeignGroupImportIDIs404(t *testing.T) {
	cfg, s, _ := importPreviewConfig(t)
	const groupA, groupB = "grp-commit-http-tenant-a", "grp-commit-http-tenant-b"
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
	rec := doImportCommit(t, h, groupB, importID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestImportCommitUnknownImportIDIs404(t *testing.T) {
	cfg, s, _ := importPreviewConfig(t)
	const group = "grp-commit-http-unknown"
	registerImportPreviewGroup(t, s, group)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportCommit(t, h, group, "does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestImportCommitAlreadyCommittedSessionIs409(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-commit-http-committed"
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
	rec := doImportCommit(t, h, group, importID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestImportCommitSetsSessionCommittedAndRemovesStagedFile(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const group = "grp-commit-http-lifecycle"
	registerImportPreviewGroup(t, s, group)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{importexport.ColumnName: "Whatever", importexport.ColumnShortCode: "X1"})
	csvBody := header + "\n" + row + "\n"
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())
	stagedPath := importexport.StagingPath(cfg.DataDir, group, importID)
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("staged file missing before the test even runs: %v", err)
	}

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportCommit(t, h, group, importID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var status string
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM import_sessions WHERE id = ?`, importID).Scan(&status); err != nil {
		t.Fatalf("query import_sessions: %v", err)
	}
	if status != importexport.StatusCommitted {
		t.Errorf("import session status = %q, want %q", status, importexport.StatusCommitted)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Errorf("staged file still exists after a successful commit (err = %v), want it removed", err)
	}
}

func TestImportCommitLabelCreationLinkingAndRemoval(t *testing.T) {
	cfg, s, _ := importPreviewConfig(t)
	const group = "grp-commit-http-labels"
	registerImportPreviewGroup(t, s, group)

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{
		importexport.ColumnName: "Tagged Thing", importexport.ColumnShortCode: "TAG0001",
		importexport.ColumnLabels: "BrandNewLabel", importexport.ColumnIdentifications: "serial:SN-777",
	})
	csvBody := header + "\n" + row + "\n"
	importID := stageImportSession(t, cfg, s, group, []byte(csvBody), time.Now().UnixMilli())

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doImportCommit(t, h, group, importID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	resp := decodeCommit(t, rec)
	itemID := resp.CreatedItems[0].ItemID

	scope, err := s.ForGroup(storage.MustGroupID(group))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	labels, err := scope.ItemLabels().ListForItem(t.Context(), itemID)
	if err != nil {
		t.Fatalf("ListForItem: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "BrandNewLabel" {
		t.Fatalf("labels = %+v, want exactly [BrandNewLabel]", labels)
	}
	idents, err := scope.Identifications().List(t.Context(), itemID)
	if err != nil {
		t.Fatalf("Identifications.List: %v", err)
	}
	if len(idents) != 1 || idents[0].Value != "SN-777" {
		t.Fatalf("identifications = %+v, want exactly one SN-777", idents)
	}

	secondRow := csvRowFrom(map[string]string{
		importexport.ColumnID: itemID, importexport.ColumnName: "Tagged Thing", importexport.ColumnShortCode: "TAG0001",
	})
	secondBody := header + "\n" + secondRow + "\n"
	secondImportID := stageImportSession(t, cfg, s, group, []byte(secondBody), time.Now().UnixMilli())
	rec = doImportCommit(t, h, group, secondImportID)
	if rec.Code != http.StatusOK {
		t.Fatalf("second commit status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	secondResp := decodeCommit(t, rec)
	if secondResp.Summary.Updated != 1 {
		t.Fatalf("second commit Summary = %+v, want Updated:1 (labels/identifications cleared is a change)", secondResp.Summary)
	}

	labelsAfter, err := scope.ItemLabels().ListForItem(t.Context(), itemID)
	if err != nil {
		t.Fatalf("ListForItem after clear: %v", err)
	}
	if len(labelsAfter) != 0 {
		t.Errorf("labels after clearing commit = %+v, want none", labelsAfter)
	}
	identsAfter, err := scope.Identifications().List(t.Context(), itemID)
	if err != nil {
		t.Fatalf("Identifications.List after clear: %v", err)
	}
	if len(identsAfter) != 0 {
		t.Errorf("identifications after clearing commit = %+v, want none", identsAfter)
	}
}

func TestImportCommitRejectsForeignLocationID(t *testing.T) {
	cfg, s, dbPath := importPreviewConfig(t)
	const groupA, groupB = "grp-commit-http-loc-a", "grp-commit-http-loc-b"
	registerImportPreviewGroup(t, s, groupA)
	registerImportPreviewGroup(t, s, groupB)

	scopeB, err := s.ForGroup(storage.MustGroupID(groupB))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	foreignLoc, err := scopeB.Locations().Create(t.Context(), storage.CreateLocationParams{ID: "loc-b", Name: "B's Garage", Now: 1})
	if err != nil {
		t.Fatalf("create foreign location: %v", err)
	}

	header := strings.Join(importexport.FixedColumns, ",")
	row := csvRowFrom(map[string]string{
		importexport.ColumnName: "Misplaced Item", importexport.ColumnShortCode: "MIS0001", importexport.ColumnLocationID: foreignLoc.ID,
	})
	csvBody := header + "\n" + row + "\n"
	importID := stageImportSession(t, cfg, s, groupA, []byte(csvBody), time.Now().UnixMilli())

	beforeSeq := readGroupChangeSeq(t, dbPath, groupA)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	previewRec := doImportPreview(t, h, groupA, importID)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200: %s", previewRec.Code, previewRec.Body.String())
	}
	previewResp := decodePreview(t, previewRec)
	if len(previewResp.Rows) != 1 || previewResp.Rows[0].Action != "error" {
		t.Fatalf("preview Rows = %+v, want the foreign location_id row classified error", previewResp.Rows)
	}

	commitRec := doImportCommit(t, h, groupA, importID)
	if commitRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("commit status = %d, want 422: %s", commitRec.Code, commitRec.Body.String())
	}

	afterSeq := readGroupChangeSeq(t, dbPath, groupA)
	if afterSeq != beforeSeq {
		t.Errorf("group change_seq_counter changed on a rejected commit: %d -> %d", beforeSeq, afterSeq)
	}
}
