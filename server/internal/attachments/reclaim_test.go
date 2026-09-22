package attachments

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newReclaimStorage(t *testing.T) *storage.Storage {
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

func createReclaimGroup(t *testing.T, s *storage.Storage, groupID string) storage.Scope {
	t.Helper()
	if err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    groupID,
		UserID:       groupID + "-owner",
		Username:     groupID + "-user",
		PasswordHash: "$argon2id$fake$" + groupID,
		Now:          time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("RegisterNewGroup(%s): %v", groupID, err)
	}
	scope, err := s.ForGroup(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%s): %v", groupID, err)
	}
	return scope
}

func createReclaimItem(t *testing.T, scope storage.Scope, itemID string) {
	t.Helper()
	if _, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID:        itemID,
		Name:      itemID,
		ShortCode: itemID,
		Now:       time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("create item %q: %v", itemID, err)
	}
}

func createReclaimAttachment(t *testing.T, scope storage.Scope, itemID, id, storagePath string) storage.Attachment {
	t.Helper()
	created, err := scope.Attachments().Create(t.Context(), storage.CreateAttachmentParams{
		ID:               id,
		ItemID:           itemID,
		Category:         CategoryGeneral,
		OriginalFilename: id + ".bin",
		ContentType:      "application/octet-stream",
		SizeBytes:        4,
		StoragePath:      storagePath,
		SHA256:           "deadbeef",
		Now:              time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("create attachment %q: %v", id, err)
	}
	return created
}

func assertFileExists(t *testing.T, root, relativePath string) {
	t.Helper()
	full := filepath.Join(root, "attachments", relativePath)
	if _, err := os.Stat(full); err != nil {
		t.Errorf("os.Stat(%s) = %v, want the file to still exist", full, err)
	}
}

func assertFileMissing(t *testing.T, root, relativePath string) {
	t.Helper()
	full := filepath.Join(root, "attachments", relativePath)
	if _, err := os.Stat(full); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Stat(%s) = %v, want a not-exist error", full, err)
	}
}

func backdateFile(t *testing.T, root, relativePath string, age time.Duration) {
	t.Helper()
	full := filepath.Join(root, "attachments", relativePath)
	older := time.Now().Add(-age)
	if err := os.Chtimes(full, older, older); err != nil {
		t.Fatalf("backdate %s: %v", full, err)
	}
}

func TestReclaimRemovesAnOrphanedFileWithNoRowAtAll(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	createReclaimGroup(t, s, "group-a")
	writeAttachmentFixture(t, root, "group-a/orphan-file", []byte("nothing points at this"))
	backdateFile(t, root, "group-a/orphan-file", minOrphanFileAge+time.Minute)

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.FilesRemoved != 1 {
		t.Errorf("FilesRemoved = %d, want 1", report.FilesRemoved)
	}
	if len(report.Errors) != 0 {
		t.Errorf("Errors = %v, want none", report.Errors)
	}
	assertFileMissing(t, root, "group-a/orphan-file")
}

func TestReclaimRemovesAnOrphanedFileWhoseRowIsTombstoned(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	created := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	writeAttachmentFixture(t, root, "group-a/att-1", []byte("orphaned by a tombstoned row"))
	backdateFile(t, root, "group-a/att-1", minOrphanFileAge+time.Minute)
	if err := scope.Attachments().Delete(t.Context(), "item-a", created.ID, time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete att-1: %v", err)
	}

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.FilesRemoved != 1 {
		t.Errorf("FilesRemoved = %d, want 1", report.FilesRemoved)
	}
	if report.RowsTombstoned != 0 {
		t.Errorf("RowsTombstoned = %d, want 0 -- the row was already tombstoned before this pass ran", report.RowsTombstoned)
	}
	assertFileMissing(t, root, "group-a/att-1")
}

func TestReclaimLeavesARecentlyOrphanedFileUntouched(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	createReclaimGroup(t, s, "group-a")
	writeAttachmentFixture(t, root, "group-a/orphan-file", []byte("could be an upload still in flight"))

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.FilesRemoved != 0 {
		t.Errorf("FilesRemoved = %d, want 0 -- a file younger than minOrphanFileAge must survive this pass", report.FilesRemoved)
	}
	if len(report.Errors) != 0 {
		t.Errorf("Errors = %v, want none", report.Errors)
	}
	assertFileExists(t, root, "group-a/orphan-file")
}

func TestReclaimRemovesAnOrphanedFileOnceItAges(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	createReclaimGroup(t, s, "group-a")
	writeAttachmentFixture(t, root, "group-a/orphan-file", []byte("genuinely orphaned, and old"))
	backdateFile(t, root, "group-a/orphan-file", minOrphanFileAge+time.Minute)

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.FilesRemoved != 1 {
		t.Errorf("FilesRemoved = %d, want 1", report.FilesRemoved)
	}
	assertFileMissing(t, root, "group-a/orphan-file")
}

func TestReclaimOrphanFileLifecycleSurvivesUntilItAges(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	createReclaimGroup(t, s, "group-a")
	writeAttachmentFixture(t, root, "group-a/orphan-file", []byte("ages into an orphan reclamation"))

	first, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("first Reclaim: %v", err)
	}
	if first.FilesRemoved != 0 {
		t.Fatalf("first pass FilesRemoved = %d, want 0", first.FilesRemoved)
	}
	assertFileExists(t, root, "group-a/orphan-file")

	backdateFile(t, root, "group-a/orphan-file", minOrphanFileAge+time.Minute)

	second, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("second Reclaim: %v", err)
	}
	if second.FilesRemoved != 1 {
		t.Fatalf("second pass FilesRemoved = %d, want 1 -- the file has now aged past minOrphanFileAge", second.FilesRemoved)
	}
	assertFileMissing(t, root, "group-a/orphan-file")
}

func TestReclaimLeavesAFileALiveRowStillReferencesUntouched(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	writeAttachmentFixture(t, root, "group-a/att-1", []byte("still referenced, must survive"))

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.FilesRemoved != 0 {
		t.Errorf("FilesRemoved = %d, want 0", report.FilesRemoved)
	}
	assertFileExists(t, root, "group-a/att-1")

	if _, err := scope.Attachments().Get(t.Context(), "item-a", "att-1"); err != nil {
		t.Errorf("Get after Reclaim: %v, want the row to still be live", err)
	}
}

func TestReclaimTombstonesALiveRowMissingItsOriginal(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.RowsTombstoned != 1 {
		t.Errorf("RowsTombstoned = %d, want 1", report.RowsTombstoned)
	}
	if len(report.Errors) != 0 {
		t.Errorf("Errors = %v, want none", report.Errors)
	}

	if _, err := scope.Attachments().Get(t.Context(), "item-a", "att-1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get after Reclaim tombstoned the row = %v, want ErrNotFound", err)
	}
}

func TestReclaimClearsAThumbnailPathWhenOnlyTheThumbnailFileIsMissing(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	created := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	writeAttachmentFixture(t, root, "group-a/att-1", []byte("the original -- present"))
	if _, err := scope.Attachments().SetThumbnailPath(t.Context(), "item-a", "att-1", "group-a/att-1-thumb.jpg", time.Now().UnixMilli()); err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.ThumbnailsCleared != 1 {
		t.Errorf("ThumbnailsCleared = %d, want 1", report.ThumbnailsCleared)
	}
	if report.RowsTombstoned != 0 {
		t.Errorf("RowsTombstoned = %d, want 0 -- only the thumbnail is missing, the row must survive", report.RowsTombstoned)
	}

	got, err := scope.Attachments().Get(t.Context(), "item-a", "att-1")
	if err != nil {
		t.Fatalf("Get after Reclaim: %v, want the row to still be live", err)
	}
	if got.ThumbnailPath.Valid {
		t.Errorf("ThumbnailPath = %+v, want NULL", got.ThumbnailPath)
	}
	if got.StoragePath != created.StoragePath {
		t.Errorf("StoragePath = %q, want unchanged %q", got.StoragePath, created.StoragePath)
	}
	assertFileExists(t, root, "group-a/att-1")
}

func TestReclaimLeavesAFullyHealthyRowAndFilesCompletelyUntouched(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	writeAttachmentFixture(t, root, "group-a/att-1", []byte("healthy original"))
	withThumb, err := scope.Attachments().SetThumbnailPath(t.Context(), "item-a", "att-1", "group-a/att-1-thumb.jpg", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}
	writeAttachmentFixture(t, root, "group-a/att-1-thumb.jpg", []byte("healthy thumbnail"))

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.FilesRemoved != 0 || report.RowsTombstoned != 0 || report.ThumbnailsCleared != 0 || len(report.Errors) != 0 {
		t.Fatalf("Report = %+v, want every count zero and no errors for a fully healthy row", report)
	}

	got, err := scope.Attachments().Get(t.Context(), "item-a", "att-1")
	if err != nil {
		t.Fatalf("Get after Reclaim: %v", err)
	}
	if got.Version != withThumb.Version {
		t.Errorf("Version = %d, want unchanged %d -- a healthy row must not be written at all", got.Version, withThumb.Version)
	}
	assertFileExists(t, root, "group-a/att-1")
	assertFileExists(t, root, "group-a/att-1-thumb.jpg")
}

func TestReclaimSecondPassIsANoOp(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")

	writeAttachmentFixture(t, root, "group-a/orphan-file", []byte("orphan"))
	backdateFile(t, root, "group-a/orphan-file", minOrphanFileAge+time.Minute)

	createReclaimAttachment(t, scope, "item-a", "att-dead", "group-a/att-dead")
	writeAttachmentFixture(t, root, "group-a/att-dead", []byte("orphaned by delete"))
	backdateFile(t, root, "group-a/att-dead", minOrphanFileAge+time.Minute)
	if err := scope.Attachments().Delete(t.Context(), "item-a", "att-dead", time.Now().UnixMilli()); err != nil {
		t.Fatalf("Delete att-dead: %v", err)
	}

	createReclaimAttachment(t, scope, "item-a", "att-healthy", "group-a/att-healthy")
	writeAttachmentFixture(t, root, "group-a/att-healthy", []byte("healthy"))

	createReclaimAttachment(t, scope, "item-a", "att-missing-original", "group-a/att-missing-original")

	createReclaimAttachment(t, scope, "item-a", "att-missing-thumb", "group-a/att-missing-thumb")
	writeAttachmentFixture(t, root, "group-a/att-missing-thumb", []byte("original present"))
	if _, err := scope.Attachments().SetThumbnailPath(t.Context(), "item-a", "att-missing-thumb", "group-a/att-missing-thumb-thumb.jpg", time.Now().UnixMilli()); err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	first, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("first Reclaim: %v", err)
	}
	if first.FilesRemoved != 2 || first.RowsTombstoned != 1 || first.ThumbnailsCleared != 1 || len(first.Errors) != 0 {
		t.Fatalf("first pass Report = %+v, want {FilesRemoved:2 RowsTombstoned:1 ThumbnailsCleared:1 Errors:[]}", first)
	}

	second, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("second Reclaim: %v", err)
	}
	if second.FilesRemoved != 0 || second.RowsTombstoned != 0 || second.ThumbnailsCleared != 0 || len(second.Errors) != 0 {
		t.Fatalf("second pass Report = %+v, want every count zero -- the first pass should have left nothing to reconcile", second)
	}
}

func TestReclaimNeverTouchesAnotherGroupsRowsOrFiles(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)

	scopeA := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scopeA, "item-a")
	createReclaimAttachment(t, scopeA, "item-a", "att-a", "group-a/att-a")
	writeAttachmentFixture(t, root, "group-a/att-a", []byte("group A's own healthy original"))

	scopeB := createReclaimGroup(t, s, "group-b")
	createReclaimItem(t, scopeB, "item-b")
	createReclaimAttachment(t, scopeB, "item-b", "att-b", "group-b/att-b")

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.RowsTombstoned != 1 {
		t.Errorf("RowsTombstoned = %d, want 1 (group B's att-b only)", report.RowsTombstoned)
	}

	gotA, err := scopeA.Attachments().Get(t.Context(), "item-a", "att-a")
	if err != nil {
		t.Fatalf("group A Get after Reclaim: %v, want the row to still be live", err)
	}
	if gotA.Version != 1 {
		t.Errorf("group A att-a Version = %d, want unchanged 1", gotA.Version)
	}
	assertFileExists(t, root, "group-a/att-a")

	if _, err := scopeB.Attachments().Get(t.Context(), "item-b", "att-b"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("group B Get after Reclaim = %v, want ErrNotFound (its att-b was missing its original)", err)
	}
}

func TestReclaimOneGroupsBadRowDoesNotAbortAnotherGroupsPass(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)

	scopeA := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scopeA, "item-a")
	createReclaimAttachment(t, scopeA, "item-a", "att-bad", "../../escaped")
	createReclaimAttachment(t, scopeA, "item-a", "att-good", "group-a/att-good")
	writeAttachmentFixture(t, root, "group-a/att-good", []byte("healthy, same group as the bad row"))

	scopeB := createReclaimGroup(t, s, "group-b")
	createReclaimItem(t, scopeB, "item-b")
	writeAttachmentFixture(t, root, "group-b/orphan", []byte("group B's own orphan, unrelated to group A's bad row"))
	backdateFile(t, root, "group-b/orphan", minOrphanFileAge+time.Minute)

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1 (the bad row's unresolvable storage_path)", report.Errors)
	}

	gotGood, err := scopeA.Attachments().Get(t.Context(), "item-a", "att-good")
	if err != nil {
		t.Fatalf("group A Get(att-good) after Reclaim: %v, want it to still be live", err)
	}
	if gotGood.Version != 1 {
		t.Errorf("att-good Version = %d, want unchanged 1", gotGood.Version)
	}
	assertFileExists(t, root, "group-a/att-good")

	gotBad, err := scopeA.Attachments().Get(t.Context(), "item-a", "att-bad")
	if err != nil {
		t.Fatalf("group A Get(att-bad) after Reclaim: %v, want it to still be live (refused, not acted on)", err)
	}
	if gotBad.Version != 1 {
		t.Errorf("att-bad Version = %d, want unchanged 1", gotBad.Version)
	}

	if report.FilesRemoved != 1 {
		t.Errorf("FilesRemoved = %d, want 1 (group B's own orphan)", report.FilesRemoved)
	}
	assertFileMissing(t, root, "group-b/orphan")
}

func TestReclaimIsEmptyOnAnEmptyDatabase(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.FilesRemoved != 0 || report.RowsTombstoned != 0 || report.ThumbnailsCleared != 0 || len(report.Errors) != 0 {
		t.Fatalf("Report = %+v, want every count zero and no errors on an empty database", report)
	}
}

func TestReclaimSkipsAGroupWithNoAttachmentDirectoryYet(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	createReclaimGroup(t, s, "group-a")

	report, err := Reclaim(t.Context(), s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("Errors = %v, want none -- a group with no attachment directory yet is not a fault", report.Errors)
	}
}

type countingCtx struct {
	context.Context
	calls       *int
	cancelAfter int
}

func (c countingCtx) Err() error {
	*c.calls++
	if *c.calls > c.cancelAfter {
		return context.Canceled
	}
	return nil
}

func TestReclaimReturnsAnErrorWhenGroupIDsFails(t *testing.T) {
	root := testRoot(t)

	_, err := Reclaim(t.Context(), nil, root)
	if err == nil {
		t.Fatal("Reclaim(nil storage) succeeded, want an error")
	}
}

func TestReclaimStopsStartingNewGroupsWhenTheContextIsCancelledBetweenGroups(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	createReclaimGroup(t, s, "group-a")
	scopeB := createReclaimGroup(t, s, "group-b")
	createReclaimItem(t, scopeB, "item-b")
	createReclaimAttachment(t, scopeB, "item-b", "att-b", "group-b/att-b")

	calls := 0
	ctx := countingCtx{Context: context.Background(), calls: &calls, cancelAfter: 1}

	report, err := Reclaim(ctx, s, root)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if report.RowsTombstoned != 0 || len(report.Errors) != 0 {
		t.Fatalf("Report = %+v, want every count zero and no errors -- group B must never have been reached", report)
	}
	if _, err := scopeB.Attachments().Get(t.Context(), "item-b", "att-b"); err != nil {
		t.Errorf("group B Get after Reclaim = %v, want it still live (its own sweep never ran)", err)
	}
}

func TestReclaimGroupStopsBeforeItsRowLoopWhenTheContextIsAlreadyCancelled(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	createReclaimAttachment(t, scope, "item-a", "att-2", "group-a/att-2")

	calls := 0
	ctx := countingCtx{Context: context.Background(), calls: &calls, cancelAfter: 0}

	var report Report
	reclaimGroup(ctx, scope, "group-a", root, &report)
	if report.RowsTombstoned != 0 || len(report.Errors) != 0 {
		t.Fatalf("Report = %+v, want every count zero -- the row loop must never have started", report)
	}
	if _, err := scope.Attachments().Get(t.Context(), "item-a", "att-1"); err != nil {
		t.Errorf("att-1 Get after reclaimGroup = %v, want it still live", err)
	}
	if _, err := scope.Attachments().Get(t.Context(), "item-a", "att-2"); err != nil {
		t.Errorf("att-2 Get after reclaimGroup = %v, want it still live", err)
	}
}

func TestReclaimGroupReportsAnErrorWhenListAllFails(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var report Report
	reclaimGroup(t.Context(), scope, "group-a", root, &report)
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1", report.Errors)
	}
	if report.FilesRemoved != 0 || report.RowsTombstoned != 0 || report.ThumbnailsCleared != 0 {
		t.Errorf("Report = %+v, want every count zero -- nothing could be read to act on", report)
	}
}

func TestReclaimOrphanedFilesSkipsSubdirectories(t *testing.T) {
	root := testRoot(t)
	subdir := filepath.Join(root, "attachments", "group-a", "a-subdirectory")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	older := time.Now().Add(-(minOrphanFileAge + time.Minute))
	if err := os.Chtimes(subdir, older, older); err != nil {
		t.Fatalf("backdate subdirectory: %v", err)
	}

	var report Report
	reclaimOrphanedFiles("group-a", root, map[string]bool{}, &report)
	if report.FilesRemoved != 0 || len(report.Errors) != 0 {
		t.Fatalf("Report = %+v, want every count zero -- a subdirectory must never be removed or reported as a fault", report)
	}
	if _, err := os.Stat(subdir); err != nil {
		t.Errorf("os.Stat(%s) = %v, want the subdirectory to still exist", subdir, err)
	}
}

func TestReclaimOrphanedFilesReportsAReadDirPermissionFailure(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	dir := filepath.Join(root, "attachments", "group-a")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	var report Report
	reclaimOrphanedFiles("group-a", root, map[string]bool{}, &report)
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1", report.Errors)
	}
	if report.FilesRemoved != 0 {
		t.Errorf("FilesRemoved = %d, want 0", report.FilesRemoved)
	}
}

func TestReclaimOrphanedFilesReportsARemovalFailure(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	dir := filepath.Join(root, "attachments", "group-a")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	orphan := filepath.Join(dir, "orphan-file")
	if err := os.WriteFile(orphan, []byte("cannot be removed"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	older := time.Now().Add(-(minOrphanFileAge + time.Minute))
	if err := os.Chtimes(orphan, older, older); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	var report Report
	reclaimOrphanedFiles("group-a", root, map[string]bool{}, &report)
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1", report.Errors)
	}
	if report.FilesRemoved != 0 {
		t.Errorf("FilesRemoved = %d, want 0 -- the removal itself failed", report.FilesRemoved)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("restore permissions to check survival: %v", err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("orphan file missing after a failed removal: %v", err)
	}
}

func TestReclaimRowReportsANonNotExistStatErrorForTheOriginal(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	row := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")

	dir := filepath.Join(root, "attachments", "group-a")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	var report Report
	reclaimRow(t.Context(), scope, root, "group-a", row, &report)
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1", report.Errors)
	}
	if report.RowsTombstoned != 0 {
		t.Errorf("RowsTombstoned = %d, want 0 -- a permission fault is not the same as a confirmed-missing original", report.RowsTombstoned)
	}
}

func TestReclaimRowRefusesAThumbnailPathEscapingTheGroupDirectory(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	created := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	writeAttachmentFixture(t, root, "group-a/att-1", []byte("healthy original"))
	row, err := scope.Attachments().SetThumbnailPath(t.Context(), "item-a", created.ID, "../../escaped-thumb", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	var report Report
	reclaimRow(t.Context(), scope, root, "group-a", row, &report)
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1", report.Errors)
	}
	if report.RowsTombstoned != 0 || report.ThumbnailsCleared != 0 {
		t.Errorf("Report = %+v, want every count zero -- a bad thumbnail_path is refused, not acted on", report)
	}
}

func TestReclaimRowReportsANonNotExistStatErrorForTheThumbnail(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	created := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	writeAttachmentFixture(t, root, "group-a/att-1", []byte("healthy original"))

	lockedDir := filepath.Join(root, "attachments", "group-a", "locked")
	if err := os.MkdirAll(lockedDir, 0o700); err != nil {
		t.Fatalf("mkdir locked subdir: %v", err)
	}
	if err := os.Chmod(lockedDir, 0o000); err != nil {
		t.Fatalf("chmod locked subdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o700) })

	row, err := scope.Attachments().SetThumbnailPath(t.Context(), "item-a", created.ID, "group-a/locked/thumb.jpg", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	var report Report
	reclaimRow(t.Context(), scope, root, "group-a", row, &report)
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1", report.Errors)
	}
	if report.ThumbnailsCleared != 0 {
		t.Errorf("ThumbnailsCleared = %d, want 0 -- a permission fault is not the same as a confirmed-missing thumbnail", report.ThumbnailsCleared)
	}
}

func TestReclaimRowSwallowsAConcurrentDeleteRace(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	row := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")

	if err := scope.Attachments().Delete(t.Context(), "item-a", row.ID, time.Now().UnixMilli()); err != nil {
		t.Fatalf("simulate concurrent Delete: %v", err)
	}

	var report Report
	reclaimRow(t.Context(), scope, root, "group-a", row, &report)
	if len(report.Errors) != 0 {
		t.Fatalf("Errors = %v, want none -- a concurrent delete race must be swallowed silently", report.Errors)
	}
	if report.RowsTombstoned != 0 {
		t.Errorf("RowsTombstoned = %d, want 0 -- the row was already tombstoned by the race winner, not by this call", report.RowsTombstoned)
	}
}

func TestReclaimRowReportsAGenuineDeleteFailure(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	row := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var report Report
	reclaimRow(t.Context(), scope, root, "group-a", row, &report)
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1", report.Errors)
	}
	if report.RowsTombstoned != 0 {
		t.Errorf("RowsTombstoned = %d, want 0 -- the write itself failed", report.RowsTombstoned)
	}
}

func TestReclaimRowSwallowsAConcurrentThumbnailClearRace(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	created := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	writeAttachmentFixture(t, root, "group-a/att-1", []byte("original present"))
	row, err := scope.Attachments().SetThumbnailPath(t.Context(), "item-a", created.ID, "group-a/att-1-thumb.jpg", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	if err := scope.Attachments().ClearThumbnailPath(t.Context(), "item-a", row.ID, time.Now().UnixMilli()); err != nil {
		t.Fatalf("simulate concurrent ClearThumbnailPath: %v", err)
	}

	var report Report
	reclaimRow(t.Context(), scope, root, "group-a", row, &report)
	if len(report.Errors) != 0 {
		t.Fatalf("Errors = %v, want none -- a concurrent thumbnail-clear race must be swallowed silently", report.Errors)
	}
	if report.ThumbnailsCleared != 0 {
		t.Errorf("ThumbnailsCleared = %d, want 0 -- it was already cleared by the race winner, not by this call", report.ThumbnailsCleared)
	}
}

func TestReclaimRowReportsAGenuineClearThumbnailPathFailure(t *testing.T) {
	root := testRoot(t)
	s := newReclaimStorage(t)
	scope := createReclaimGroup(t, s, "group-a")
	createReclaimItem(t, scope, "item-a")
	created := createReclaimAttachment(t, scope, "item-a", "att-1", "group-a/att-1")
	writeAttachmentFixture(t, root, "group-a/att-1", []byte("original present"))
	row, err := scope.Attachments().SetThumbnailPath(t.Context(), "item-a", created.ID, "group-a/att-1-thumb.jpg", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var report Report
	reclaimRow(t.Context(), scope, root, "group-a", row, &report)
	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1", report.Errors)
	}
	if report.ThumbnailsCleared != 0 {
		t.Errorf("ThumbnailsCleared = %d, want 0 -- the write itself failed", report.ThumbnailsCleared)
	}
}
