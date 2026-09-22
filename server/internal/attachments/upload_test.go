package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("skipping permission-based fault injection: running as root, which bypasses file-mode checks entirely")
	}
}

type fakeItemRepository struct {
	getFn func(ctx context.Context, itemID string) (storage.Item, error)
}

func (f fakeItemRepository) Get(ctx context.Context, itemID string) (storage.Item, error) {
	return f.getFn(ctx, itemID)
}

func (fakeItemRepository) GetByShortCode(context.Context, string) (storage.Item, error) {
	panic("attachments: fakeItemRepository.GetByShortCode: Upload never calls this")
}

func (fakeItemRepository) GetByIDs(context.Context, []string) ([]storage.Item, error) {
	panic("attachments: fakeItemRepository.GetByIDs: Upload never calls this")
}

func (fakeItemRepository) List(context.Context, storage.Page) ([]storage.Item, error) {
	panic("attachments: fakeItemRepository.List: Upload never calls this")
}

func (fakeItemRepository) ListFiltered(context.Context, storage.ItemFilter, storage.Page) ([]storage.Item, error) {
	panic("attachments: fakeItemRepository.ListFiltered: Upload never calls this")
}

func (fakeItemRepository) Create(context.Context, storage.CreateItemParams) (storage.Item, error) {
	panic("attachments: fakeItemRepository.Create: Upload never calls this")
}

func (fakeItemRepository) Update(context.Context, storage.UpdateItemParams) (storage.Item, error) {
	panic("attachments: fakeItemRepository.Update: Upload never calls this")
}

func (fakeItemRepository) Delete(context.Context, string, int64) error {
	panic("attachments: fakeItemRepository.Delete: Upload never calls this")
}

func liveItem() fakeItemRepository {
	return fakeItemRepository{getFn: func(context.Context, string) (storage.Item, error) {
		return storage.Item{ID: "item-1"}, nil
	}}
}

type fakeAttachmentRepository struct {
	created storage.CreateAttachmentParams
	err     error

	setThumbnailPathFn func(ctx context.Context, itemID, id, thumbnailPath string, now int64) (storage.Attachment, error)
	thumbnailPathCalls []string
}

func (f *fakeAttachmentRepository) Create(_ context.Context, p storage.CreateAttachmentParams) (storage.Attachment, error) {
	f.created = p
	if f.err != nil {
		return storage.Attachment{}, f.err
	}
	return storage.Attachment{
		ID:               p.ID,
		ItemID:           p.ItemID,
		Category:         p.Category,
		OriginalFilename: p.OriginalFilename,
		ContentType:      p.ContentType,
		SizeBytes:        p.SizeBytes,
		StoragePath:      p.StoragePath,
		Sha256:           p.SHA256,
		Version:          1,
	}, nil
}

func (f *fakeAttachmentRepository) SetThumbnailPath(ctx context.Context, itemID, id, thumbnailPath string, now int64) (storage.Attachment, error) {
	f.thumbnailPathCalls = append(f.thumbnailPathCalls, thumbnailPath)
	if f.setThumbnailPathFn != nil {
		return f.setThumbnailPathFn(ctx, itemID, id, thumbnailPath, now)
	}
	return storage.Attachment{
		ID:            id,
		ItemID:        itemID,
		ThumbnailPath: sql.NullString{String: thumbnailPath, Valid: true},
		Version:       2,
	}, nil
}

func (f *fakeAttachmentRepository) Get(context.Context, string, string) (storage.Attachment, error) {
	panic("attachments: fakeAttachmentRepository.Get: Upload never calls this")
}

func (f *fakeAttachmentRepository) Delete(context.Context, string, string, int64) error {
	panic("attachments: fakeAttachmentRepository.Delete: Upload never calls this")
}

func (f *fakeAttachmentRepository) ListForItem(context.Context, string) ([]storage.Attachment, error) {
	panic("attachments: fakeAttachmentRepository.ListForItem: Upload never calls this")
}

func (f *fakeAttachmentRepository) ListAll(context.Context) ([]storage.Attachment, error) {
	panic("attachments: fakeAttachmentRepository.ListAll: Upload never calls this")
}

func (f *fakeAttachmentRepository) ClearThumbnailPath(context.Context, string, string, int64) error {
	panic("attachments: fakeAttachmentRepository.ClearThumbnailPath: Upload never calls this")
}

func testRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := datadir.Ensure(root); err != nil {
		t.Fatalf("datadir.Ensure(%s): %v", root, err)
	}
	return root
}

func validParams() UploadParams {
	return UploadParams{
		ItemID:           "item-1",
		GroupID:          "group-1",
		Category:         CategoryImage,
		OriginalFilename: "photo.jpg",
	}
}

func TestUploadSniffsContentTypeFromTheBytesThemselves(t *testing.T) {
	pngSignature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

	for _, tc := range []struct {
		name    string
		content []byte
		want    string
	}{
		{"plain_text", []byte("just a plain text warranty note, nothing binary about it"), "text/plain; charset=utf-8"},
		{"png_signature_only", pngSignature, "image/png"},
		{"png_signature_plus_body", append(append([]byte{}, pngSignature...), bytes.Repeat([]byte{0x00}, 100)...), "image/png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeAttachmentRepository{}
			created, err := Upload(t.Context(), liveItem(), repo, testRoot(t), DefaultMaxUploadBytes, validParams(), bytes.NewReader(tc.content))
			if err != nil {
				t.Fatalf("Upload: %v", err)
			}
			if created.ContentType != tc.want {
				t.Errorf("ContentType = %q, want %q", created.ContentType, tc.want)
			}
			if repo.created.ContentType != tc.want {
				t.Errorf("CreateAttachmentParams.ContentType = %q, want %q", repo.created.ContentType, tc.want)
			}
		})
	}
}

func TestUploadPreservesExactBytesAcrossTheSniffSplice(t *testing.T) {
	for _, size := range []int{10, sniffLen, sniffLen + 200} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			content := deterministicBytes(size)
			wantHash := sha256Hex(content)

			repo := &fakeAttachmentRepository{}
			created, err := Upload(t.Context(), liveItem(), repo, testRoot(t), DefaultMaxUploadBytes, validParams(), bytes.NewReader(content))
			if err != nil {
				t.Fatalf("Upload: %v", err)
			}
			if created.SizeBytes != int64(size) {
				t.Errorf("SizeBytes = %d, want %d", created.SizeBytes, size)
			}
			if created.Sha256 != wantHash {
				t.Errorf("SHA256 = %s, want %s (the sniff splice corrupted the stored bytes)", created.Sha256, wantHash)
			}
		})
	}
}

func TestUploadRejectsFilesOverTheConfiguredMaxBytes(t *testing.T) {
	const limit = 100
	content := deterministicBytes(limit + 1)

	_, err := Upload(t.Context(), liveItem(), &fakeAttachmentRepository{}, testRoot(t), limit, validParams(), bytes.NewReader(content))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Upload error = %v, want ErrTooLarge", err)
	}
}

func TestUploadAcceptsAFileAtExactlyMaxBytes(t *testing.T) {
	const limit = 100
	content := deterministicBytes(limit)

	created, err := Upload(t.Context(), liveItem(), &fakeAttachmentRepository{}, testRoot(t), limit, validParams(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Upload at exactly the limit: %v", err)
	}
	if created.SizeBytes != limit {
		t.Errorf("SizeBytes = %d, want %d", created.SizeBytes, limit)
	}
}

func TestUploadRejectsANonPositiveMaxBytes(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		t.Run(strconv.Itoa(int(limit)), func(t *testing.T) {
			itemGetCalled := false
			items := fakeItemRepository{getFn: func(context.Context, string) (storage.Item, error) {
				itemGetCalled = true
				return storage.Item{}, nil
			}}

			_, err := Upload(t.Context(), items, &fakeAttachmentRepository{}, testRoot(t), limit, validParams(), strings.NewReader("x"))
			if err == nil {
				t.Fatal("Upload with a non-positive maxBytes returned no error")
			}
			if itemGetCalled {
				t.Error("Upload read the parent item before rejecting the non-positive maxBytes; the cheap rejection should come first")
			}
		})
	}
}

func TestUploadRejectsAnItemFromAnotherGroup(t *testing.T) {
	srcRead := false
	src := &readTrackingReader{r: strings.NewReader("should never be read"), read: &srcRead}

	items := fakeItemRepository{getFn: func(context.Context, string) (storage.Item, error) {
		return storage.Item{}, storage.ErrNotFound
	}}
	repo := &fakeAttachmentRepository{}

	_, err := Upload(t.Context(), items, repo, testRoot(t), DefaultMaxUploadBytes, validParams(), src)
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Upload error = %v, want storage.ErrNotFound", err)
	}
	if srcRead {
		t.Error("Upload read src before the parent-item check failed; a cross-group upload must write nothing to disk")
	}
	if repo.created != (storage.CreateAttachmentParams{}) {
		t.Error("Upload called repo.Create despite the parent-item check failing")
	}
}

type readTrackingReader struct {
	r    *strings.Reader
	read *bool
}

func (t *readTrackingReader) Read(p []byte) (int, error) {
	*t.read = true
	return t.r.Read(p)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func deterministicBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func TestUploadParamsValidateRejectsEachEmptyRequiredField(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(p *UploadParams)
		wantErr error
	}{
		{"empty_item_id", func(p *UploadParams) { p.ItemID = "" }, nil},
		{"empty_group_id", func(p *UploadParams) { p.GroupID = "" }, nil},
		{"empty_original_filename", func(p *UploadParams) { p.OriginalFilename = "" }, ErrFilenameRequired},
		{"invalid_category", func(p *UploadParams) { p.Category = "not-a-real-category" }, ErrCategoryInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := validParams()
			tc.mutate(&p)

			itemGetCalled := false
			items := fakeItemRepository{getFn: func(context.Context, string) (storage.Item, error) {
				itemGetCalled = true
				return storage.Item{}, nil
			}}

			_, err := Upload(t.Context(), items, &fakeAttachmentRepository{}, testRoot(t), DefaultMaxUploadBytes, p, strings.NewReader("x"))
			if err == nil {
				t.Fatal("Upload succeeded with an invalid UploadParams, want an error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("Upload error = %v, want it to wrap %v", err, tc.wantErr)
			}
			if itemGetCalled {
				t.Error("Upload read the parent item before rejecting an invalid UploadParams; the cheap rejection should come first")
			}
		})
	}
}

func TestUploadRejectsANilItemRepository(t *testing.T) {
	_, err := Upload(t.Context(), nil, &fakeAttachmentRepository{}, testRoot(t), DefaultMaxUploadBytes, validParams(), strings.NewReader("x"))
	if err == nil {
		t.Fatal("Upload with a nil storage.ItemRepository succeeded, want an error")
	}
}

func TestUploadRejectsANilAttachmentRepository(t *testing.T) {
	_, err := Upload(t.Context(), liveItem(), nil, testRoot(t), DefaultMaxUploadBytes, validParams(), strings.NewReader("x"))
	if err == nil {
		t.Fatal("Upload with a nil storage.AttachmentRepository succeeded, want an error")
	}
}

func TestUploadRejectsAnEmptyRoot(t *testing.T) {
	_, err := Upload(t.Context(), liveItem(), &fakeAttachmentRepository{}, "", DefaultMaxUploadBytes, validParams(), strings.NewReader("x"))
	if err == nil {
		t.Fatal("Upload with an empty root succeeded, want an error")
	}
}

func TestUploadWrapsAGenericItemsGetFailure(t *testing.T) {
	wantErr := errors.New("attachments_test: simulated storage fault reading the parent item")
	items := fakeItemRepository{getFn: func(context.Context, string) (storage.Item, error) {
		return storage.Item{}, wantErr
	}}

	_, err := Upload(t.Context(), items, &fakeAttachmentRepository{}, testRoot(t), DefaultMaxUploadBytes, validParams(), strings.NewReader("x"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Upload error = %v, want it to wrap %v", err, wantErr)
	}
	if errors.Is(err, storage.ErrNotFound) {
		t.Error("Upload reported a generic items.Get fault as storage.ErrNotFound; httpapi's handler would answer 404 for what should be a 500")
	}
}

type errImmediateReader struct{ err error }

func (r errImmediateReader) Read([]byte) (int, error) { return 0, r.err }

func TestUploadWrapsAReadFailureDuringContentTypeSniffing(t *testing.T) {
	wantErr := errors.New("attachments_test: simulated read fault during sniffing")

	_, err := Upload(t.Context(), liveItem(), &fakeAttachmentRepository{}, testRoot(t), DefaultMaxUploadBytes, validParams(), errImmediateReader{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Upload error = %v, want it to wrap %v", err, wantErr)
	}
}

type errAfterSniffReader struct {
	err  error
	done bool
}

func (r *errAfterSniffReader) Read(p []byte) (int, error) {
	if !r.done {
		for i := range p {
			p[i] = byte(i)
		}
		r.done = true
		return len(p), nil
	}
	return 0, r.err
}

func TestUploadWrapsAReadFailureDuringTheStagingCopy(t *testing.T) {
	wantErr := errors.New("attachments_test: simulated read fault mid-copy")

	_, err := Upload(t.Context(), liveItem(), &fakeAttachmentRepository{}, testRoot(t), DefaultMaxUploadBytes, validParams(), &errAfterSniffReader{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Upload error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestUploadFailsWhenTheStagingDirectoryCannotBeWrittenTo(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	tmpDir := filepath.Join(root, datadir.TmpSubdir)
	if err := os.Chmod(tmpDir, 0o500); err != nil {
		t.Fatalf("chmod tmp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, 0o700) })

	_, err := Upload(t.Context(), liveItem(), &fakeAttachmentRepository{}, root, DefaultMaxUploadBytes, validParams(), strings.NewReader("x"))
	if err == nil {
		t.Fatal("Upload succeeded despite an unwritable staging directory, want an error")
	}
}

func TestUploadFailsWhenTheFinalGroupPathIsAFileNotADirectory(t *testing.T) {
	root := testRoot(t)
	groupPath := filepath.Join(root, datadir.AttachmentsSubdir, "group-1")
	if err := os.WriteFile(groupPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write file where the group directory belongs: %v", err)
	}

	_, err := Upload(t.Context(), liveItem(), &fakeAttachmentRepository{}, root, DefaultMaxUploadBytes, validParams(), strings.NewReader("x"))
	if err == nil {
		t.Fatal("Upload succeeded despite its own group directory path being a plain file, want an error")
	}
}

func TestUploadFailsWhenRenameCannotWriteIntoTheFinalDirectory(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	finalDir := filepath.Join(root, datadir.AttachmentsSubdir, "group-1")
	if err := os.MkdirAll(finalDir, 0o700); err != nil {
		t.Fatalf("pre-create final dir: %v", err)
	}
	if err := os.Chmod(finalDir, 0o500); err != nil {
		t.Fatalf("chmod final dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(finalDir, 0o700) })

	_, err := Upload(t.Context(), liveItem(), &fakeAttachmentRepository{}, root, DefaultMaxUploadBytes, validParams(), strings.NewReader("x"))
	if err == nil {
		t.Fatal("Upload succeeded despite an unwritable final group directory, want an error")
	}
}

func TestUploadFailsWhenFsyncDirCannotOpenTheFinalDirectory(t *testing.T) {
	skipIfRoot(t)
	root := testRoot(t)
	finalDir := filepath.Join(root, datadir.AttachmentsSubdir, "group-1")
	if err := os.MkdirAll(finalDir, 0o700); err != nil {
		t.Fatalf("pre-create final dir: %v", err)
	}
	if err := os.Chmod(finalDir, 0o300); err != nil {
		t.Fatalf("chmod final dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(finalDir, 0o700) })

	repo := &fakeAttachmentRepository{}
	_, err := Upload(t.Context(), liveItem(), repo, root, DefaultMaxUploadBytes, validParams(), strings.NewReader("x"))
	if err == nil {
		t.Fatal("Upload succeeded despite fsyncDir being unable to open the final directory, want an error")
	}
	if repo.created != (storage.CreateAttachmentParams{}) {
		t.Error("Upload called repo.Create despite fsyncDir failing; the row must never be committed for a file whose rename durability could not be confirmed")
	}
}

func TestUploadRemovesTheRenamedFileWhenRepoCreateFails(t *testing.T) {
	root := testRoot(t)
	wantErr := errors.New("attachments_test: simulated repo.Create fault")
	repo := &fakeAttachmentRepository{err: wantErr}

	_, err := Upload(t.Context(), liveItem(), repo, root, DefaultMaxUploadBytes, validParams(), strings.NewReader("some upload bytes"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Upload error = %v, want it to wrap %v", err, wantErr)
	}
	if repo.created.ID == "" {
		t.Fatal("repo.Create was never called; nothing to check the cleanup of")
	}

	finalPath := filepath.Join(root, datadir.AttachmentsSubdir, "group-1", repo.created.ID)
	if _, statErr := os.Stat(finalPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("os.Stat(%s) = %v, want a not-exist error -- Upload should have removed the orphaned file synchronously", finalPath, statErr)
	}
}

func TestFsyncDirFailsWhenTheDirectoryDoesNotExist(t *testing.T) {
	err := fsyncDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("fsyncDir on a nonexistent directory succeeded, want an error")
	}
}
