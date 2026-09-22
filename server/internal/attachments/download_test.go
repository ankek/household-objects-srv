package attachments

import (
	"database/sql"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeAttachmentFixture(t *testing.T, root, relativePath string, content []byte) {
	t.Helper()
	full := filepath.Join(root, "attachments", relativePath)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("mkdir for fixture %s: %v", full, err)
	}
	if err := os.WriteFile(full, content, 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", full, err)
	}
}

func TestOpenOriginalReadsExactBytes(t *testing.T) {
	root := testRoot(t)
	want := []byte("the exact original bytes, unmodified")
	writeAttachmentFixture(t, root, "group-1/att-1", want)

	f, err := OpenOriginal(root, "group-1", storage.Attachment{ID: "att-1", StoragePath: "group-1/att-1"})
	if err != nil {
		t.Fatalf("OpenOriginal: %v", err)
	}
	defer func() { _ = f.Close() }()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("OpenOriginal bytes = %q, want %q", got, want)
	}
}

func TestOpenOriginalRejectsAnUnknownFile(t *testing.T) {
	root := testRoot(t)

	_, err := OpenOriginal(root, "group-1", storage.Attachment{ID: "att-1", StoragePath: "group-1/does-not-exist"})
	if err == nil {
		t.Fatal("OpenOriginal succeeded for a file that was never written")
	}
}

func TestOpenThumbnailReturnsErrNoThumbnailWhenTheColumnIsNull(t *testing.T) {
	root := testRoot(t)

	_, err := OpenThumbnail(root, "group-1", storage.Attachment{ID: "att-1"})
	if !errors.Is(err, ErrNoThumbnail) {
		t.Fatalf("OpenThumbnail(no thumbnail_path) error = %v, want ErrNoThumbnail", err)
	}
}

func TestOpenThumbnailOpensTheRecordedFile(t *testing.T) {
	root := testRoot(t)
	want := []byte("pretend this is a JPEG-encoded thumbnail")
	writeAttachmentFixture(t, root, "group-1/att-1-thumb.jpg", want)

	row := storage.Attachment{
		ID:            "att-1",
		ThumbnailPath: sql.NullString{String: "group-1/att-1-thumb.jpg", Valid: true},
	}
	f, err := OpenThumbnail(root, "group-1", row)
	if err != nil {
		t.Fatalf("OpenThumbnail: %v", err)
	}
	defer func() { _ = f.Close() }()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("OpenThumbnail bytes = %q, want %q", got, want)
	}
}

func TestOpenUnderGroupRejectsAStoragePathNamingAnotherGroupsDirectory(t *testing.T) {
	root := testRoot(t)
	writeAttachmentFixture(t, root, "group-2/att-secret", []byte("group 2's own secret bytes"))

	_, err := OpenOriginal(root, "group-1", storage.Attachment{ID: "att-x", StoragePath: "group-2/att-secret"})
	if err == nil {
		t.Fatal("OpenOriginal(groupID=group-1, storagePath naming group-2) succeeded, want a refusal")
	}
}

func TestOpenUnderGroupRejectsPathTraversalOutsideTheAttachmentsTree(t *testing.T) {
	root := testRoot(t)
	if err := os.WriteFile(filepath.Join(root, "escaped-secret"), []byte("not an attachment at all"), 0o600); err != nil {
		t.Fatalf("write escape target: %v", err)
	}

	_, err := OpenOriginal(root, "group-1", storage.Attachment{ID: "att-x", StoragePath: "../escaped-secret"})
	if err == nil {
		t.Fatal("OpenOriginal(storagePath containing ..) succeeded, want a refusal")
	}
}

func TestOpenUnderGroupRejectsASiblingDirectorySharingATextualPrefix(t *testing.T) {
	root := testRoot(t)
	writeAttachmentFixture(t, root, "group-1-evil/att-x", []byte("belongs to a different, similarly-named group"))

	_, err := OpenOriginal(root, "group-1", storage.Attachment{ID: "att-x", StoragePath: "group-1-evil/att-x"})
	if err == nil {
		t.Fatal("OpenOriginal(storagePath naming a sibling directory sharing a prefix) succeeded, want a refusal")
	}
}

func TestOpenThumbnailWrapsAMissingThumbnailFile(t *testing.T) {
	root := testRoot(t)

	row := storage.Attachment{
		ID:            "att-1",
		ThumbnailPath: sql.NullString{String: "group-1/att-1-thumb.jpg", Valid: true},
	}
	_, err := OpenThumbnail(root, "group-1", row)
	if err == nil {
		t.Fatal("OpenThumbnail succeeded for a thumbnail file that was never written")
	}
	if errors.Is(err, ErrNoThumbnail) {
		t.Error("OpenThumbnail reported ErrNoThumbnail for a SET column whose file is merely missing -- that sentinel is reserved for a NULL column")
	}
}

func TestResolveUnderGroupRejectsEachEmptyArgument(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		root, groupID, storagePath string
	}{
		{"empty_root", "", "group-1", "group-1/att-1"},
		{"empty_group_id", "/data", "", "group-1/att-1"},
		{"empty_storage_path", "/data", "group-1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveUnderGroup(tc.root, tc.groupID, tc.storagePath)
			if err == nil {
				t.Errorf("resolveUnderGroup(%q, %q, %q) succeeded, want an error", tc.root, tc.groupID, tc.storagePath)
			}
		})
	}
}

func TestInlineSafeContentType(t *testing.T) {
	cases := []struct {
		contentType string
		want        bool
	}{
		{"image/jpeg", true},
		{"image/png", true},
		{"image/webp", true},
		{"image/gif", false},
		{"image/svg+xml", false},
		{"text/html", false},
		{"text/html; charset=utf-8", false},
		{"application/pdf", false},
		{"application/octet-stream", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.contentType, func(t *testing.T) {
			if got := InlineSafeContentType(tc.contentType); got != tc.want {
				t.Errorf("InlineSafeContentType(%q) = %v, want %v", tc.contentType, got, tc.want)
			}
		})
	}
}
