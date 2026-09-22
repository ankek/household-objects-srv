package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doAttachmentDelete(t *testing.T, h http.Handler, itemID, attachmentID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/items/"+itemID+"/attachments/"+attachmentID, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func deletingAttachmentRepository(row storage.Attachment) fakeAttachmentRepository {
	return fakeAttachmentRepository{
		getFn: func(context.Context, string, string) (storage.Attachment, error) {
			return row, nil
		},
		deleteFn: func(context.Context, string, string, int64) error {
			return nil
		},
	}
}

func requireFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("os.Stat(%s) = %v, want a not-exist error -- the file was supposed to be unlinked", path, err)
	}
}

func TestAttachmentDeleteRemovesTheRowAndTheFile(t *testing.T) {
	repo := deletingAttachmentRepository(storage.Attachment{ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1"})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1", []byte("bytes to be deleted"))
	filePath := filepath.Join(cfg.DataDir, "attachments", testGroup, "att-1")

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDelete(t, h, "item-1", "att-1")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 response has a non-empty body: %q", rec.Body.String())
	}
	requireFileAbsent(t, filePath)
}

func TestAttachmentDeleteRemovesTheGeneratedThumbnailToo(t *testing.T) {
	repo := deletingAttachmentRepository(storage.Attachment{
		ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1",
		ThumbnailPath: sql.NullString{String: testGroup + "/att-1-thumb.jpg", Valid: true},
	})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1", []byte("original bytes"))
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1-thumb.jpg", []byte("thumbnail bytes"))
	originalPath := filepath.Join(cfg.DataDir, "attachments", testGroup, "att-1")
	thumbPath := filepath.Join(cfg.DataDir, "attachments", testGroup, "att-1-thumb.jpg")

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDelete(t, h, "item-1", "att-1")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	requireFileAbsent(t, originalPath)
	requireFileAbsent(t, thumbPath)
}

func TestAttachmentDeleteSucceedsWhenTheFileIsAlreadyMissingOnDisk(t *testing.T) {
	repo := deletingAttachmentRepository(storage.Attachment{ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1"})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDelete(t, h, "item-1", "att-1")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 even though the file was already absent: %s", rec.Code, rec.Body.String())
	}
}

func TestAttachmentDeleteRejectsAForeignOrUnknownAttachment(t *testing.T) {
	repo := fakeAttachmentRepository{getFn: func(context.Context, string, string) (storage.Attachment, error) {
		return storage.Attachment{}, storage.ErrNotFound
	}}
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDelete(t, h, "item-1", "att-from-another-group")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Detail != "" {
		t.Errorf("404 carries a detail (%q); an existence oracle for a foreign-group attachment is exactly what NFR-010 forbids", p.Detail)
	}
}

func TestAttachmentDeleteOnAnAlreadyTombstonedRowAnswersNotFound(t *testing.T) {
	repo := fakeAttachmentRepository{
		getFn: func(context.Context, string, string) (storage.Attachment, error) {
			return storage.Attachment{ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1"}, nil
		},
		deleteFn: func(context.Context, string, string, int64) error {
			return storage.ErrNotFound
		},
	}
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1", []byte("still on disk"))
	filePath := filepath.Join(cfg.DataDir, "attachments", testGroup, "att-1")

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDelete(t, h, "item-1", "att-1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Errorf("os.Stat(%s) = %v, want the file to still be present after a 404 delete", filePath, err)
	}
}

func TestAttachmentDeleteRejectsAnUnauthenticatedRequest(t *testing.T) {
	repo := deletingAttachmentRepository(storage.Attachment{ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1"})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	cfg.Authenticator = rejectingAuth

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDelete(t, h, "item-1", "att-1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestAttachmentDeleteStillSucceedsWhenTheFileCannotBeUnlinked(t *testing.T) {
	var deleteCalled bool
	repo := fakeAttachmentRepository{
		getFn: func(context.Context, string, string) (storage.Attachment, error) {
			return storage.Attachment{ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1"}, nil
		},
		deleteFn: func(context.Context, string, string, int64) error {
			deleteCalled = true
			return nil
		},
	}
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	var logs bytes.Buffer
	cfg.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	dirPath := filepath.Join(cfg.DataDir, "attachments", testGroup, "att-1")
	if err := os.MkdirAll(dirPath, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dirPath, err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "inner-file"), []byte("makes att-1 a non-empty directory"), 0o600); err != nil {
		t.Fatalf("write inner file: %v", err)
	}

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDelete(t, h, "item-1", "att-1")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 even though the unlink genuinely failed: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 response has a non-empty body: %q", rec.Body.String())
	}
	if !deleteCalled {
		t.Fatal("scope.Attachments().Delete was never reached -- the row was not tombstoned, so this test proves nothing about the unlink failure being non-fatal")
	}
	if _, err := os.Stat(dirPath); err != nil {
		t.Errorf("os.Stat(%s) = %v, want the un-removable directory to still be present (this test's own fixture, not a claim about correctness)", dirPath, err)
	}

	got := logs.String()
	if !strings.Contains(got, `"level":"WARN"`) {
		t.Errorf("unlink failure was not logged at WARN: %s", got)
	}
	if strings.Contains(got, `"level":"ERROR"`) {
		t.Errorf("unlink failure was logged at ERROR -- a stuck file is not this request's own failure: %s", got)
	}
}
