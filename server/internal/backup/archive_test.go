package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type archiveEntry struct {
	Name string
	Data []byte
	Mode int64
}

func readArchiveEntries(t *testing.T, r io.Reader) []archiveEntry {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var entries []archiveEntry
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar Next: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %q: %v", hdr.Name, err)
		}
		entries = append(entries, archiveEntry{Name: hdr.Name, Data: data, Mode: hdr.Mode})
	}
	return entries
}

func writeAttachmentFile(t *testing.T, dataDir, groupID, name string, content []byte) {
	t.Helper()
	dir := filepath.Join(dataDir, "attachments", groupID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
		t.Fatalf("write attachment %s/%s: %v", groupID, name, err)
	}
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(42)).Read(b) //nolint:gosec // test fixture, not a security boundary
	return b
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return string(sum[:])
}

func TestWriteArchiveProducesTheDocumentedLayoutInOrder(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "groupA"), "itemA1")

	dataDir := t.TempDir()
	fileA := []byte("hello from groupA")
	fileB := randomBytes(8192)
	writeAttachmentFile(t, dataDir, "groupA", "note.txt", fileA)
	writeAttachmentFile(t, dataDir, "groupB", "photo.bin", fileB)

	var buf bytes.Buffer
	stats, err := WriteArchive(t.Context(), s, dataDir, &buf)
	if err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}

	entries := readArchiveEntries(t, &buf)
	if len(entries) != 4 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name
		}
		t.Fatalf("archive has %d entries (%v), want exactly 4", len(entries), names)
	}

	if entries[0].Name != ManifestEntryName {
		t.Fatalf("entries[0].Name = %q, want %q (manifest must be first)", entries[0].Name, ManifestEntryName)
	}
	var m ArchiveManifest
	if err := json.Unmarshal(entries[0].Data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.FormatVersion != ArchiveFormatVersion {
		t.Errorf("manifest FormatVersion = %d, want %d", m.FormatVersion, ArchiveFormatVersion)
	}
	if m.SchemaVersion != s.SchemaVersion() {
		t.Errorf("manifest SchemaVersion = %d, want %d", m.SchemaVersion, s.SchemaVersion())
	}
	if m.CreatedAtUnixMillis <= 0 {
		t.Errorf("manifest CreatedAtUnixMillis = %d, want a positive epoch millisecond", m.CreatedAtUnixMillis)
	}

	if entries[1].Name != DBEntryName {
		t.Fatalf("entries[1].Name = %q, want %q (database snapshot must be second)", entries[1].Name, DBEntryName)
	}
	dbPath := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(dbPath, entries[1].Data, 0o600); err != nil {
		t.Fatalf("write extracted db entry: %v", err)
	}
	restored, err := storage.Open(t.Context(), storage.Config{Path: dbPath, ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open(extracted db entry): %v", err)
	}
	defer func() {
		if err := restored.Close(); err != nil {
			t.Errorf("Close(restored): %v", err)
		}
	}()
	scope, err := restored.ForGroup(storage.MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA) on extracted db: %v", err)
	}
	if got, err := scope.Items().Get(t.Context(), "itemA1"); err != nil || got.ID != "itemA1" {
		t.Fatalf("Get(itemA1) on extracted db = (%+v, %v), want itemA1 present", got, err)
	}

	wantAttachments := map[string][]byte{
		"attachments/groupA/note.txt":  fileA,
		"attachments/groupB/photo.bin": fileB,
	}
	gotOrder := []string{entries[2].Name, entries[3].Name}
	wantOrder := []string{"attachments/groupA/note.txt", "attachments/groupB/photo.bin"}
	if gotOrder[0] != wantOrder[0] || gotOrder[1] != wantOrder[1] {
		t.Fatalf("attachment entry order = %v, want %v", gotOrder, wantOrder)
	}
	for _, e := range entries[2:] {
		want, ok := wantAttachments[e.Name]
		if !ok {
			t.Fatalf("unexpected attachment entry %q", e.Name)
		}
		if sha256Hex(e.Data) != sha256Hex(want) {
			t.Errorf("attachment %q content hash mismatch -- round trip did not preserve bytes exactly", e.Name)
		}
	}

	if stats.AttachmentFiles != 2 {
		t.Errorf("stats.AttachmentFiles = %d, want 2", stats.AttachmentFiles)
	}
	if want := int64(len(fileA) + len(fileB)); stats.AttachmentBytes != want {
		t.Errorf("stats.AttachmentBytes = %d, want %d", stats.AttachmentBytes, want)
	}
	if stats.AttachmentFilesSkipped != 0 {
		t.Errorf("stats.AttachmentFilesSkipped = %d, want 0", stats.AttachmentFilesSkipped)
	}
}

func TestWriteArchiveEveryEntryIsOwnerOnlyReadable(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "groupA"), "itemA1")

	dataDir := t.TempDir()
	writeAttachmentFile(t, dataDir, "groupA", "note.txt", []byte("hello from groupA"))

	var buf bytes.Buffer
	if _, err := WriteArchive(t.Context(), s, dataDir, &buf); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}

	entries := readArchiveEntries(t, &buf)
	if len(entries) != 3 {
		t.Fatalf("archive has %d entries, want exactly 3 (manifest + db + one attachment)", len(entries))
	}
	for _, e := range entries {
		if e.Mode != 0o600 {
			t.Errorf("entry %q tar header Mode = %#o, want exactly %#o (owner read/write only)", e.Name, e.Mode, 0o600)
		}
	}
}

func TestWriteArchiveWithEmptyAttachmentsTreeStillProducesAValidArchive(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "groupA"), "itemA1")

	dataDir := t.TempDir()

	var buf bytes.Buffer
	stats, err := WriteArchive(t.Context(), s, dataDir, &buf)
	if err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}

	entries := readArchiveEntries(t, &buf)
	if len(entries) != 2 {
		t.Fatalf("archive has %d entries, want exactly 2 (manifest + db)", len(entries))
	}
	if entries[0].Name != ManifestEntryName || entries[1].Name != DBEntryName {
		t.Fatalf("entries = [%s, %s], want [%s, %s]", entries[0].Name, entries[1].Name, ManifestEntryName, DBEntryName)
	}
	if stats.AttachmentFiles != 0 || stats.AttachmentBytes != 0 || stats.AttachmentFilesSkipped != 0 {
		t.Errorf("stats = %+v, want every count zero for an empty attachments tree", stats)
	}
}

func TestWriteArchiveRejectsInvalidArguments(t *testing.T) {
	validStore := newTestStorage(t)

	tests := []struct {
		name    string
		store   *storage.Storage
		dataDir string
		w       io.Writer
	}{
		{name: "nil store", store: nil, dataDir: "/tmp/does-not-matter", w: &bytes.Buffer{}},
		{name: "empty data directory", store: validStore, dataDir: "", w: &bytes.Buffer{}},
		{name: "nil writer", store: validStore, dataDir: "/tmp/does-not-matter", w: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := WriteArchive(t.Context(), tt.store, tt.dataDir, tt.w)
			if err == nil {
				t.Fatal("WriteArchive succeeded, want a validation error")
			}
		})
	}
}

func TestWriteAttachmentEntrySkipsAFileThatVanishesBeforeOpen(t *testing.T) {
	dir := t.TempDir()
	ghost := filepath.Join(dir, "ghost.bin")
	if err := os.WriteFile(ghost, []byte("here for a moment"), 0o600); err != nil {
		t.Fatalf("write ghost file: %v", err)
	}
	if err := os.Remove(ghost); err != nil {
		t.Fatalf("remove ghost file: %v", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	var stats ArchiveStats
	if err := writeAttachmentEntry(tw, ghost, "attachments/groupA/ghost.bin", &stats); err != nil {
		t.Fatalf("writeAttachmentEntry(vanished file): %v, want nil -- a mid-walk deletion is not a fault", err)
	}
	if stats.AttachmentFilesSkipped != 1 {
		t.Errorf("stats.AttachmentFilesSkipped = %d, want 1", stats.AttachmentFilesSkipped)
	}
	if stats.AttachmentFiles != 0 {
		t.Errorf("stats.AttachmentFiles = %d, want 0", stats.AttachmentFiles)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close after a skipped entry: %v, want nil -- a skip must not corrupt the stream", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close after a skipped entry: %v", err)
	}
	if entries := readArchiveEntries(t, &buf); len(entries) != 0 {
		t.Errorf("archive has %d entries after a skipped file, want 0", len(entries))
	}
}

func TestWriteAttachmentEntrySkipsAPathThatBecameADirectoryBeforeOpen(t *testing.T) {
	dir := t.TempDir()
	surprise := filepath.Join(dir, "was-a-file")
	if err := os.Mkdir(surprise, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	var stats ArchiveStats
	if err := writeAttachmentEntry(tw, surprise, "attachments/groupA/was-a-file", &stats); err != nil {
		t.Fatalf("writeAttachmentEntry(directory): %v, want nil", err)
	}
	if stats.AttachmentFilesSkipped != 1 {
		t.Errorf("stats.AttachmentFilesSkipped = %d, want 1", stats.AttachmentFilesSkipped)
	}
}

func TestWriteAttachmentsTreeStopsWhenContextIsAlreadyCancelled(t *testing.T) {
	dataDir := t.TempDir()
	writeAttachmentFile(t, dataDir, "groupA", "note.txt", []byte("content"))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	var stats ArchiveStats
	err := writeAttachmentsTree(ctx, tw, dataDir, &stats)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("writeAttachmentsTree with a cancelled context: err = %v, want context.Canceled", err)
	}
	if stats.AttachmentFiles != 0 {
		t.Errorf("stats.AttachmentFiles = %d, want 0 -- no entry should start after cancellation", stats.AttachmentFiles)
	}
}

func TestWriteArchiveManifestSchemaVersionMatchesStorage(t *testing.T) {
	s := newTestStorage(t)
	if s.SchemaVersion() == 0 {
		t.Fatalf("test fixture invariant broken: a freshly-migrated store reports SchemaVersion 0")
	}

	var buf bytes.Buffer
	if _, err := WriteArchive(t.Context(), s, t.TempDir(), &buf); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	entries := readArchiveEntries(t, &buf)
	var m ArchiveManifest
	if err := json.Unmarshal(entries[0].Data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.SchemaVersion != s.SchemaVersion() {
		t.Errorf("manifest SchemaVersion = %d, want %d", m.SchemaVersion, s.SchemaVersion())
	}
}

func TestWriteArchiveLeavesNoStagedSnapshotBehind(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "groupA"), "itemA1")
	dataDir := t.TempDir()

	var buf bytes.Buffer
	if _, err := WriteArchive(t.Context(), s, dataDir, &buf); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}

	tmpDir := filepath.Join(dataDir, "tmp")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir(tmp): %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("tmp/ contains %v after a successful WriteArchive, want empty -- a staged snapshot was left behind", names)
	}
}

func TestWriteArchiveAttachmentNamesUseForwardSlashesOnly(t *testing.T) {
	s := newTestStorage(t)
	dataDir := t.TempDir()
	writeAttachmentFile(t, dataDir, "groupA", "note.txt", []byte("x"))

	var buf bytes.Buffer
	if _, err := WriteArchive(t.Context(), s, dataDir, &buf); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	entries := readArchiveEntries(t, &buf)
	for _, e := range entries {
		if strings.Contains(e.Name, `\`) {
			t.Errorf("entry name %q contains a backslash, want forward-slash-only tar paths", e.Name)
		}
	}
}
