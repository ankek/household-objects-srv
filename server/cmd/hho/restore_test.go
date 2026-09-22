package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/backup"
	"github.com/ankek/Household-Objects-Dev/server/internal/serverlock"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedAttachmentFile(t *testing.T, dataDir, groupID, name string, data []byte) {
	t.Helper()
	dir := filepath.Join(dataDir, "attachments", groupID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("seed attachment dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatalf("seed attachment file: %v", err)
	}
}

type archiveEntrySpec struct {
	name string
	data []byte
}

func buildArchive(t *testing.T, manifest backup.ArchiveManifest, entries []archiveEntrySpec) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	all := append([]archiveEntrySpec{{name: backup.ManifestEntryName, data: manifestData}}, entries...)
	for _, e := range all {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: tar.TypeReg,
			Mode:     0o600,
			Size:     int64(len(e.data)),
			ModTime:  time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header for %q: %v", e.name, err)
		}
		if _, err := tw.Write(e.data); err != nil {
			t.Fatalf("write data for %q: %v", e.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func realManifest(t *testing.T) backup.ArchiveManifest {
	t.Helper()
	store, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(t.TempDir(), "probe.db")})
	if err != nil {
		t.Fatalf("open schema probe: %v", err)
	}
	defer func() { _ = store.Close() }()
	return backup.ArchiveManifest{
		FormatVersion:       backup.ArchiveFormatVersion,
		CreatedAtUnixMillis: time.Now().UnixMilli(),
		SchemaVersion:       store.SchemaVersion(),
	}
}

func userExists(t *testing.T, dbPath, username string) bool {
	t.Helper()
	store, err := storage.Open(t.Context(), storage.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer func() { _ = store.Close() }()
	_, err = store.ResetPassword(t.Context(), username, "$argon2id$fake$probe", time.Now().UnixMilli())
	switch {
	case err == nil:
		return true
	case errors.Is(err, storage.ErrUserNotFound):
		return false
	default:
		t.Fatalf("ResetPassword(%q) probe: %v", username, err)
		return false
	}
}

func TestRunRestoreRoundTripFromFile(t *testing.T) {
	srcDataDir := t.TempDir()
	seedTestGroup(t, srcDataDir, "alice")
	attData := []byte("hello from an attachment")
	seedAttachmentFile(t, srcDataDir, "grp-1", "note.txt", attData)

	archivePath := filepath.Join(t.TempDir(), "hho-backup.tar.gz")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"backup", "--output", archivePath, "--data-dir", srcDataDir}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("hho backup: exit=%d stderr=%s", code, stderr.String())
	}

	restoreDataDir := t.TempDir()
	stdout.Reset()
	stderr.Reset()
	code := run([]string{"restore", "--input", archivePath, "--data-dir", restoreDataDir}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("hho restore: exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "restored 1 attachment file(s)") {
		t.Errorf("stdout = %q, want it to report exactly 1 restored attachment", stdout.String())
	}

	restoredDBPath := filepath.Join(restoreDataDir, "db", "hho.db")
	if !userExists(t, restoredDBPath, "alice") {
		t.Error("user alice not found in restored database")
	}

	got, err := os.ReadFile(filepath.Join(restoreDataDir, "attachments", "grp-1", "note.txt"))
	if err != nil {
		t.Fatalf("read restored attachment: %v", err)
	}
	if !bytes.Equal(got, attData) {
		t.Errorf("restored attachment bytes = %q, want %q", got, attData)
	}
}

func TestRunRestoreRoundTripFromStdin(t *testing.T) {
	srcDataDir := t.TempDir()
	seedTestGroup(t, srcDataDir, "alice")

	var backupOut, backupErr bytes.Buffer
	if code := run([]string{"backup", "--data-dir", srcDataDir}, nil, &backupOut, &backupErr); code != 0 {
		t.Fatalf("hho backup: exit=%d stderr=%s", code, backupErr.String())
	}

	stdinR, stdinW, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	go func() {
		_, _ = stdinW.Write(backupOut.Bytes())
		_ = stdinW.Close()
	}()

	restoreDataDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--data-dir", restoreDataDir}, stdinR, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("hho restore (stdin): exit=%d stderr=%s", code, stderr.String())
	}
	if !userExists(t, filepath.Join(restoreDataDir, "db", "hho.db"), "alice") {
		t.Error("user alice not found in restored database")
	}
}

func TestRunRestoreRefusesExistingDataWithoutForce(t *testing.T) {
	srcDataDir := t.TempDir()
	seedTestGroup(t, srcDataDir, "alice")
	archivePath := filepath.Join(t.TempDir(), "hho-backup.tar.gz")
	var buf bytes.Buffer
	if code := run([]string{"backup", "--output", archivePath, "--data-dir", srcDataDir}, nil, &buf, &buf); code != 0 {
		t.Fatalf("hho backup: exit=%d out=%s", code, buf.String())
	}

	targetDataDir := t.TempDir()
	seedTestGroup(t, targetDataDir, "bob")

	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--input", archivePath, "--data-dir", targetDataDir}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero when existing data is present without --force")
	}
	if !strings.Contains(stderr.String(), "--force") {
		t.Errorf("stderr = %q, want it to mention --force", stderr.String())
	}
	if !userExists(t, filepath.Join(targetDataDir, "db", "hho.db"), "bob") {
		t.Error("original user bob was disturbed even though the restore was refused")
	}
}

func TestRunRestoreForcePreservesExistingData(t *testing.T) {
	srcDataDir := t.TempDir()
	seedTestGroup(t, srcDataDir, "alice")
	archivePath := filepath.Join(t.TempDir(), "hho-backup.tar.gz")
	var buf bytes.Buffer
	if code := run([]string{"backup", "--output", archivePath, "--data-dir", srcDataDir}, nil, &buf, &buf); code != 0 {
		t.Fatalf("hho backup: exit=%d out=%s", code, buf.String())
	}

	targetDataDir := t.TempDir()
	seedTestGroup(t, targetDataDir, "bob")

	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--input", archivePath, "--data-dir", targetDataDir, "--force"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("hho restore --force: exit=%d stderr=%s", code, stderr.String())
	}
	if !userExists(t, filepath.Join(targetDataDir, "db", "hho.db"), "alice") {
		t.Error("restored user alice not found after --force restore")
	}

	entries, err := os.ReadDir(targetDataDir)
	if err != nil {
		t.Fatalf("ReadDir(targetDataDir): %v", err)
	}
	var preserved string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".hho-pre-restore-") {
			preserved = filepath.Join(targetDataDir, e.Name())
		}
	}
	if preserved == "" {
		t.Fatalf("no .hho-pre-restore-* directory found under %s, entries: %v", targetDataDir, entries)
	}
	if !strings.Contains(stdout.String(), preserved) {
		t.Errorf("stdout = %q, want it to name the preserved-data path %s", stdout.String(), preserved)
	}
	if !userExists(t, filepath.Join(preserved, "db", "hho.db"), "bob") {
		t.Error("preserved original user bob not found in the moved-aside database")
	}
}

func TestRunRestoreDetectsPathTraversal(t *testing.T) {
	targetDataDir := t.TempDir()
	sentinelParent := filepath.Dir(targetDataDir)

	archive := buildArchive(t, realManifest(t), []archiveEntrySpec{
		{name: backup.DBEntryName, data: []byte("not a real database, never opened -- extraction fails before verification")},
		{name: "attachments/../evil.txt", data: []byte("pwned")},
	})
	archivePath := filepath.Join(t.TempDir(), "malicious.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatalf("write malicious archive: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--input", archivePath, "--data-dir", targetDataDir}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for a path-traversal attachment entry")
	}
	if !strings.Contains(stderr.String(), "traversal") {
		t.Errorf("stderr = %q, want it to name the path traversal refusal", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(sentinelParent, "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("evil.txt escaped to %s: stat err = %v, want ErrNotExist", sentinelParent, err)
	}
	if _, err := os.Stat(filepath.Join(targetDataDir, "db")); !os.IsNotExist(err) {
		t.Errorf("targetDataDir/db was created despite the refused restore: stat err = %v", err)
	}
}

func TestSafeAttachmentPathRejectsTraversalShapes(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name  string
		entry string
		want  bool
	}{
		{"ordinary", "attachments/grp-1/photo.jpg", true},
		{"dotdot first segment", "attachments/../evil.txt", false},
		{"deep traversal", "attachments/../../../etc/passwd", false},
		{"dotdot filename", "attachments/grp-1/..", false},
		{"single dot filename", "attachments/grp-1/.", false},
		{"empty group", "attachments//photo.jpg", false},
		{"empty filename", "attachments/grp-1/", false},
		{"only group segment", "attachments/grp-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeAttachmentPath(root, tc.entry)
			if tc.want && err != nil {
				t.Fatalf("safeAttachmentPath(%q) = %v, want no error", tc.entry, err)
			}
			if !tc.want && err == nil {
				t.Fatalf("safeAttachmentPath(%q) = %q, nil, want a rejection", tc.entry, got)
			}
			if err == nil {
				if rel, relErr := filepath.Rel(root, got); relErr != nil || strings.HasPrefix(rel, "..") {
					t.Fatalf("safeAttachmentPath(%q) = %q escapes root %q", tc.entry, got, root)
				}
			}
		})
	}
}

func TestRunRestoreRejectsUnsupportedFormatVersion(t *testing.T) {
	manifest := realManifest(t)
	manifest.FormatVersion = 999999
	archive := buildArchive(t, manifest, []archiveEntrySpec{
		{name: backup.DBEntryName, data: []byte("never read")},
	})
	archivePath := filepath.Join(t.TempDir(), "future-format.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	targetDataDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--input", archivePath, "--data-dir", targetDataDir}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for an unsupported format version")
	}
	if !strings.Contains(stderr.String(), "format version") {
		t.Errorf("stderr = %q, want it to name the format version mismatch", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(targetDataDir, "db")); !os.IsNotExist(err) {
		t.Errorf("targetDataDir/db was created despite the refused restore: stat err = %v", err)
	}
}

func TestRunRestoreRejectsNewerSchemaVersion(t *testing.T) {
	manifest := realManifest(t)
	manifest.SchemaVersion += 1_000_000
	archive := buildArchive(t, manifest, []archiveEntrySpec{
		{name: backup.DBEntryName, data: []byte("never read")},
	})
	archivePath := filepath.Join(t.TempDir(), "future-schema.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	targetDataDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--input", archivePath, "--data-dir", targetDataDir}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for a newer-than-supported schema version")
	}
	if !strings.Contains(stderr.String(), "schema") {
		t.Errorf("stderr = %q, want it to name the schema version mismatch", stderr.String())
	}
}

func TestRunRestoreDetectsALiveDatabaseConnection(t *testing.T) {
	srcDataDir := t.TempDir()
	seedTestGroup(t, srcDataDir, "alice")
	archivePath := filepath.Join(t.TempDir(), "hho-backup.tar.gz")
	var buf bytes.Buffer
	if code := run([]string{"backup", "--output", archivePath, "--data-dir", srcDataDir}, nil, &buf, &buf); code != 0 {
		t.Fatalf("hho backup: exit=%d out=%s", code, buf.String())
	}

	targetDataDir := t.TempDir()
	seedTestGroup(t, targetDataDir, "bob")
	dbPath := filepath.Join(targetDataDir, "db", "hho.db")

	holder, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open holder connection: %v", err)
	}
	defer func() { _ = holder.Close() }()
	holder.SetMaxOpenConns(1)
	if _, err := holder.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("acquire write lock: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--input", archivePath, "--data-dir", targetDataDir, "--force"}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero while a connection holds the write lock")
	}
	if !strings.Contains(stderr.String(), "hho serve") {
		t.Errorf("stderr = %q, want it to point at hho serve", stderr.String())
	}

	if _, err := holder.ExecContext(t.Context(), "ROLLBACK"); err != nil {
		t.Fatalf("release write lock: %v", err)
	}
	if !userExists(t, dbPath, "bob") {
		t.Error("original user bob was disturbed even though the restore was refused")
	}
}

func TestRunRestoreDetectsAnIdleServerLock(t *testing.T) {
	srcDataDir := t.TempDir()
	seedTestGroup(t, srcDataDir, "alice")
	archivePath := filepath.Join(t.TempDir(), "hho-backup.tar.gz")
	var buf bytes.Buffer
	if code := run([]string{"backup", "--output", archivePath, "--data-dir", srcDataDir}, nil, &buf, &buf); code != 0 {
		t.Fatalf("hho backup: exit=%d out=%s", code, buf.String())
	}

	targetDataDir := t.TempDir()
	seedTestGroup(t, targetDataDir, "bob")
	dbPath := filepath.Join(targetDataDir, "db", "hho.db")

	lock, err := serverlock.Acquire(targetDataDir)
	if err != nil {
		t.Fatalf("serverlock.Acquire: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--input", archivePath, "--data-dir", targetDataDir, "--force"}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero while an idle server holds the lock")
	}
	if !strings.Contains(stderr.String(), "hho serve") {
		t.Errorf("stderr = %q, want it to point at hho serve", stderr.String())
	}
	if !userExists(t, dbPath, "bob") {
		t.Error("original user bob was disturbed even though the restore was refused")
	}

	if err := lock.Close(); err != nil {
		t.Fatalf("release serverlock: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"restore", "--input", archivePath, "--data-dir", targetDataDir, "--force"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("restore after the lock was released: exit=%d stderr=%s", code, stderr.String())
	}
	if !userExists(t, dbPath, "alice") {
		t.Error("restore after the lock was released did not actually restore alice's data")
	}
}

func TestRunRestoreRejectsAnUnexpectedArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "extra-argument"}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for an unexpected positional argument")
	}
}
