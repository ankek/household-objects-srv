package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func readBackupArchive(t *testing.T, data []byte) (names []string, manifest []byte) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar Next: %v", err)
		}
		names = append(names, hdr.Name)
		if hdr.Name == "manifest.json" {
			manifest, err = io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read manifest.json: %v", err)
			}
		}
	}
	return names, manifest
}

func requireArchiveShape(t *testing.T, names []string, manifestData []byte) {
	t.Helper()
	if len(names) < 2 || names[0] != "manifest.json" || names[1] != "db/hho.db" {
		t.Fatalf("archive entries = %v, want manifest.json then db/hho.db first", names)
	}
	var manifest struct {
		FormatVersion       int   `json:"format_version"`
		CreatedAtUnixMillis int64 `json:"created_at_unix_ms"`
		SchemaVersion       int64 `json:"schema_version"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("unmarshal manifest.json: %v", err)
	}
	if manifest.FormatVersion == 0 {
		t.Errorf("manifest FormatVersion = 0, want a real format version")
	}
	if manifest.SchemaVersion == 0 {
		t.Errorf("manifest SchemaVersion = 0, want the real schema version of the seeded database")
	}
}

func TestRunBackupToFileProducesAValidArchive(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	destPath := filepath.Join(t.TempDir(), "hho-backup.tar.gz")

	var stdout, stderr bytes.Buffer
	code := run([]string{"backup", "--output", destPath, "--data-dir", dataDir}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty -- a file destination must not also write the archive to stdout", stdout.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("wrote "+destPath)) {
		t.Errorf("stderr = %q, want a completion summary naming %s", stderr.String(), destPath)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read backup archive at %s: %v", destPath, err)
	}
	names, manifest := readBackupArchive(t, data)
	requireArchiveShape(t, names, manifest)

	entries, err := os.ReadDir(filepath.Dir(destPath))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("destination directory entries = %v, want exactly the one final archive, no leftover staging file", entries)
	}
}

func TestRunBackupToFileIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on windows")
	}

	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")
	destPath := filepath.Join(t.TempDir(), "hho-backup.tar.gz")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"backup", "--output", destPath, "--data-dir", dataDir}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, stderr.String())
	}

	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("backup archive mode = %o, want 0600 (owner-only)", perm)
	}
}

func TestRunBackupToDirectoryGeneratesATimestampedName(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")
	destDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"backup", "--output", destDir, "--data-dir", dataDir}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, stderr.String())
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("destination directory entries = %v, want exactly one generated archive", entries)
	}
	name := entries[0].Name()
	if !bytes.HasPrefix([]byte(name), []byte("hho-backup-")) || filepath.Ext(name) != ".gz" {
		t.Errorf("generated name = %q, want the hho-backup-<timestamp>.tar.gz shape GET /api/v1/backup also uses", name)
	}
}

func TestRunBackupToStdoutStreamsTheArchive(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	var stdout, stderr bytes.Buffer
	code := run([]string{"backup", "--data-dir", dataDir}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, stderr.String())
	}

	names, manifest := readBackupArchive(t, stdout.Bytes())
	requireArchiveShape(t, names, manifest)

	if !bytes.Contains(stderr.Bytes(), []byte("wrote stdout")) {
		t.Errorf("stderr = %q, want the completion summary to name stdout as the destination", stderr.String())
	}
}

func TestRunBackupToExplicitStdoutFlagStreamsTheArchive(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	var stdout, stderr bytes.Buffer
	code := run([]string{"backup", "--output", "-", "--data-dir", dataDir}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, stderr.String())
	}
	names, manifest := readBackupArchive(t, stdout.Bytes())
	requireArchiveShape(t, names, manifest)
}

func TestRunBackupReportsSkippedAttachments(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"backup", "--data-dir", dataDir}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("0 skipped mid-walk")) {
		t.Errorf("stderr = %q, want it to report zero attachments skipped for a fresh seed with no attachments", stderr.String())
	}
}

func TestRunBackupFailsLoudlyOnAnUnwritableDestination(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed blocking file: %v", err)
	}
	destPath := filepath.Join(blocker, "hho-backup.tar.gz")

	var stdout, stderr bytes.Buffer
	code := run([]string{"backup", "--output", destPath, "--data-dir", dataDir}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero for an unwritable destination; stdout: %s", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty, want an error naming the destination problem")
	}
}

func TestRunBackupRejectsAnUnexpectedArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"backup", "extra-argument"}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for an unexpected positional argument")
	}
}
