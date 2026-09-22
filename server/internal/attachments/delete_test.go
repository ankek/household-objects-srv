package attachments

import (
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteRemovesTheOriginalFile(t *testing.T) {
	root := testRoot(t)
	writeAttachmentFixture(t, root, "group-1/att-1", []byte("bytes to be deleted"))
	fullPath := filepath.Join(root, "attachments", "group-1", "att-1")

	errs := Delete(root, "group-1", storage.Attachment{ID: "att-1", StoragePath: "group-1/att-1"})
	if len(errs) != 0 {
		t.Fatalf("Delete returned %d error(s), want 0: %v", len(errs), errs)
	}
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Fatalf("os.Stat(%s) = %v, want a not-exist error", fullPath, err)
	}
}

func TestDeleteRemovesTheThumbnailWhenOneIsSet(t *testing.T) {
	root := testRoot(t)
	writeAttachmentFixture(t, root, "group-1/att-1", []byte("original bytes"))
	writeAttachmentFixture(t, root, "group-1/att-1-thumb.jpg", []byte("thumbnail bytes"))
	originalPath := filepath.Join(root, "attachments", "group-1", "att-1")
	thumbPath := filepath.Join(root, "attachments", "group-1", "att-1-thumb.jpg")

	row := storage.Attachment{
		ID:            "att-1",
		StoragePath:   "group-1/att-1",
		ThumbnailPath: sql.NullString{String: "group-1/att-1-thumb.jpg", Valid: true},
	}
	errs := Delete(root, "group-1", row)
	if len(errs) != 0 {
		t.Fatalf("Delete returned %d error(s), want 0: %v", len(errs), errs)
	}
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Errorf("original: os.Stat(%s) = %v, want a not-exist error", originalPath, err)
	}
	if _, err := os.Stat(thumbPath); !os.IsNotExist(err) {
		t.Errorf("thumbnail: os.Stat(%s) = %v, want a not-exist error", thumbPath, err)
	}
}

func TestDeleteSkipsTheThumbnailWhenNoneIsRecorded(t *testing.T) {
	root := testRoot(t)
	writeAttachmentFixture(t, root, "group-1/att-1", []byte("original bytes, no thumbnail"))

	errs := Delete(root, "group-1", storage.Attachment{ID: "att-1", StoragePath: "group-1/att-1"})
	if len(errs) != 0 {
		t.Fatalf("Delete returned %d error(s), want 0: %v", len(errs), errs)
	}
}

func TestDeleteIsIdempotentWhenTheFileIsAlreadyMissing(t *testing.T) {
	root := testRoot(t)

	errs := Delete(root, "group-1", storage.Attachment{ID: "att-1", StoragePath: "group-1/does-not-exist"})
	if len(errs) != 0 {
		t.Fatalf("Delete against an already-missing file returned %d error(s), want 0: %v", len(errs), errs)
	}
}

func TestDeleteReportsAGenuineRemovalFailure(t *testing.T) {
	root := testRoot(t)
	writeAttachmentFixture(t, root, "group-1/att-1/inner-file", []byte("makes att-1 a non-empty directory"))

	errs := Delete(root, "group-1", storage.Attachment{ID: "att-1", StoragePath: "group-1/att-1"})
	if len(errs) != 1 {
		t.Fatalf("Delete returned %d error(s), want exactly 1 for a genuine removal failure: %v", len(errs), errs)
	}
}

func TestDeleteReportsAGenuineThumbnailRemovalFailure(t *testing.T) {
	root := testRoot(t)
	writeAttachmentFixture(t, root, "group-1/att-1", []byte("removes cleanly"))
	writeAttachmentFixture(t, root, "group-1/att-1-thumb.jpg/inner-file", []byte("makes the thumbnail path a non-empty directory"))
	originalPath := filepath.Join(root, "attachments", "group-1", "att-1")

	row := storage.Attachment{
		ID:            "att-1",
		StoragePath:   "group-1/att-1",
		ThumbnailPath: sql.NullString{String: "group-1/att-1-thumb.jpg", Valid: true},
	}
	errs := Delete(root, "group-1", row)
	if len(errs) != 1 {
		t.Fatalf("Delete returned %d error(s), want exactly 1 (the thumbnail removal failure): %v", len(errs), errs)
	}
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Errorf("original: os.Stat(%s) = %v, want a not-exist error -- the original's own removal must succeed independently", originalPath, err)
	}
}

func TestDeleteRejectsAStoragePathNamingAnotherGroupsDirectory(t *testing.T) {
	root := testRoot(t)
	writeAttachmentFixture(t, root, "group-2/att-secret", []byte("group 2's own secret bytes"))
	fullPath := filepath.Join(root, "attachments", "group-2", "att-secret")

	errs := Delete(root, "group-1", storage.Attachment{ID: "att-x", StoragePath: "group-2/att-secret"})
	if len(errs) != 1 {
		t.Fatalf("Delete(groupID=group-1, storagePath naming group-2) returned %d error(s), want exactly 1", len(errs))
	}
	if _, err := os.Stat(fullPath); err != nil {
		t.Errorf("group-2's file was removed by a group-1-scoped Delete call: os.Stat(%s) = %v, want it to still exist", fullPath, err)
	}
}
