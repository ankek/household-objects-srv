package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/backup"
	"github.com/ankek/Household-Objects-Dev/server/internal/config"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/serverlock"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const restoreUsage = `usage: hho restore [--input path] [--data-dir dir] [--force]

Restores FR-133's whole backup archive -- the identical format 'hho backup' writes and
'GET /api/v1/backup' serves -- into the data directory, replacing its database and attachments
tree.

  --input     source: a file path, or "-" for stdin (default: "-", i.e. stdin)
  --data-dir  override the data directory (default: $HHO_DATA_DIR, or /data)
  --force     required when the data directory already holds a database or any attachment files;
              without it, a restore that would overwrite existing data is refused outright. With
              it, the EXISTING data is moved aside into a new ".hho-pre-restore-<random>" directory
              next to it, never deleted -- see this command's own return contract for why silent
              deletion of an operator's data is never acceptable here.

With no --input (or "--input -"), the archive is read from stdin, so a remote backup can be piped
straight in without ever touching this machine's disk as an intermediate file:

    hho restore < hho-backup.tar.gz
    ssh backup-host 'cat /backups/hho-backup.tar.gz' | hho restore

# Stop 'hho serve' first

This command refuses to run against a data directory a running 'hho serve' holds open, whether or
not that server happens to be writing at the exact instant this command runs (FU-15/T115a): 'hho
serve' holds its own advisory lock on the data directory for its entire lifetime (see package
serverlock), and this command checks that lock before touching anything on disk. --force does not
override this refusal -- it is permission to replace the data that is already there, not permission
to race a process that is still writing to it. A SEPARATE, narrower check (the same SQLITE_BUSY
condition 'hho backup' and 'hho user reset-password' already recognise) also still runs afterwards,
catching a server actively holding SQLite's own write lock even in the rare case the lock above
could not be checked (see "best-effort" below). Stopping 'hho serve' before restoring remains the
right operator habit either way; this command's checks are a backstop, not a substitute for it.

On a data directory whose filesystem does not support the advisory lock this relies on (most
plausibly a network share), this command cannot tell whether a server is running and says so with a
warning rather than silently assuming one is not -- stopping 'hho serve' yourself is the only way
to be sure in that case.

# What is validated before anything on disk is touched

The archive's manifest (its own first entry) is read and checked first: an archive whose format
this binary does not understand, or whose database schema is NEWER than this binary's own latest
migration, is refused before a single byte of the (potentially much larger) database or attachments
entries that follow it is read. An archive whose schema is OLDER is accepted -- opening the restored
database applies this binary's own forward-only migrations to it exactly as 'hho serve' would on its
own next start (FR-131).

A one-line summary (attachment files restored, and where any preserved prior data was moved to, if
any) is printed to stdout on success.
`

const restoreManifestMaxBytes = 64 * 1024

const restoreSchemaProbeSubdir = "restore-schema-probe"

func runRestore(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { _, _ = fmt.Fprint(stderr, restoreUsage) }
	input := flags.String("input", "-", `source: a file path, or "-" for stdin`)
	dataDirFlag := flags.String("data-dir", "", "override the data directory (default: $HHO_DATA_DIR, or /data)")
	force := flags.Bool("force", false, "replace existing data, moving it aside rather than deleting it")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "hho restore: unexpected argument(s) %v\n", flags.Args())
		flags.Usage()
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho restore: %v\n", err)
		return 1
	}
	if *dataDirFlag != "" {
		cfg.DataDir = *dataDirFlag
	}

	src, closeSrc, err := openRestoreSource(*input, stdin)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho restore: %v\n", err)
		return 1
	}
	defer func() { _ = closeSrc() }()

	ctx := context.Background()
	summary, err := restore(ctx, cfg.DataDir, src, *force)
	if err != nil {
		switch {
		case serverlock.IsHeld(err):
			_, _ = fmt.Fprintf(stderr, "hho restore: \"hho serve\" is running against %s: %v\n", cfg.DataDir, err)
		case isDatabaseLocked(err):
			_, _ = fmt.Fprintf(stderr, "hho restore: the database is locked -- is \"hho serve\" already running against %s? stop it before restoring: %v\n", cfg.DataDir, err)
		default:
			_, _ = fmt.Fprintf(stderr, "hho restore: %v\n", err)
		}
		return 1
	}

	if summary.ServerLockWarning != "" {
		_, _ = fmt.Fprintf(stderr, "hho restore: warning: %s\n", summary.ServerLockWarning)
	}
	_, _ = fmt.Fprintf(stdout, "hho restore: restored %d attachment file(s), %d byte(s); database schema version %d\n",
		summary.AttachmentFiles, summary.AttachmentBytes, summary.SchemaVersion)
	if summary.PreservedPath != "" {
		_, _ = fmt.Fprintf(stdout, "hho restore: existing data was preserved, not deleted, at %s\n", summary.PreservedPath)
	}
	return 0
}

func openRestoreSource(input string, stdin *os.File) (io.Reader, func() error, error) {
	if input == "" || input == "-" {
		return stdin, func() error { return nil }, nil
	}
	f, err := os.Open(input)
	if err != nil {
		return nil, nil, fmt.Errorf("open --input %q: %w", input, err)
	}
	return f, f.Close, nil
}

type restoreSummary struct {
	AttachmentFiles   int
	AttachmentBytes   int64
	SchemaVersion     int64
	PreservedPath     string
	ServerLockWarning string
}

func restore(ctx context.Context, dataDir string, src io.Reader, force bool) (restoreSummary, error) {
	var summary restoreSummary

	tmpDir := filepath.Join(dataDir, datadir.TmpSubdir)
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return summary, fmt.Errorf("create staging directory %q: %w", tmpDir, err)
	}

	gz, err := gzip.NewReader(src)
	if err != nil {
		return summary, fmt.Errorf("read archive: not a valid gzip stream: %w", err)
	}
	tr := tar.NewReader(gz)

	manifest, err := readManifest(tr)
	if err != nil {
		return summary, err
	}
	if manifest.FormatVersion != backup.ArchiveFormatVersion {
		return summary, fmt.Errorf(
			"archive format version %d is not the version this hho binary understands (%d); "+
				"restore it with a compatible hho version instead",
			manifest.FormatVersion, backup.ArchiveFormatVersion)
	}
	latestSchema, err := latestSupportedSchemaVersion(ctx, tmpDir)
	if err != nil {
		return summary, fmt.Errorf("determine this binary's own supported schema version: %w", err)
	}
	if manifest.SchemaVersion > latestSchema {
		return summary, fmt.Errorf(
			"archive database schema version %d is newer than this hho binary supports (%d); "+
				"upgrade hho before restoring this archive",
			manifest.SchemaVersion, latestSchema)
	}

	dbDir := filepath.Join(dataDir, datadir.DBSubdir)
	dbPath := filepath.Join(dbDir, "hho.db")
	attachmentsDir := filepath.Join(dataDir, datadir.AttachmentsSubdir)

	hadExisting, err := dataDirHasExistingData(dbPath, attachmentsDir)
	if err != nil {
		return summary, fmt.Errorf("check data directory for existing data: %w", err)
	}
	if hadExisting && !force {
		return summary, fmt.Errorf(
			"%s already holds a database or attachments; rerun with --force to replace it "+
				"(the existing data is moved aside, never deleted)", dataDir)
	}

	warning, err := probeServerNotRunning(dataDir)
	if err != nil {
		return summary, err
	}
	summary.ServerLockWarning = warning

	if err := probeDatabaseNotLive(ctx, dbPath); err != nil {
		return summary, err
	}

	stagingRoot, err := os.MkdirTemp(tmpDir, "restore-staging-*")
	if err != nil {
		return summary, fmt.Errorf("create restore staging area under %q: %w", tmpDir, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stagingRoot)
		}
	}()

	stagedDBPath := filepath.Join(stagingRoot, "hho.db")
	stagedAttachmentsRoot := filepath.Join(stagingRoot, "attachments")
	if err := extractArchiveBody(tr, stagedDBPath, stagedAttachmentsRoot, &summary); err != nil {
		return summary, err
	}

	verifyStore, err := storage.Open(ctx, storage.Config{Path: stagedDBPath})
	if err != nil {
		return summary, fmt.Errorf("open restored database for verification (it may be corrupt, or not a valid hho database): %w", err)
	}
	summary.SchemaVersion = verifyStore.SchemaVersion()
	if err := verifyStore.Close(); err != nil {
		return summary, fmt.Errorf("close verified restored database: %w", err)
	}

	if hadExisting {
		preserved, err := os.MkdirTemp(dataDir, ".hho-pre-restore-*")
		if err != nil {
			return summary, fmt.Errorf("create directory to preserve existing data: %w", err)
		}
		if err := preserveIfExists(dbDir, filepath.Join(preserved, datadir.DBSubdir)); err != nil {
			return summary, err
		}
		if err := preserveIfExists(attachmentsDir, filepath.Join(preserved, datadir.AttachmentsSubdir)); err != nil {
			return summary, err
		}
		summary.PreservedPath = preserved
	}

	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		return summary, fmt.Errorf("create %q: %w", dbDir, err)
	}
	if err := os.Rename(stagedDBPath, dbPath); err != nil {
		return summary, fmt.Errorf("move restored database into place at %q: %w", dbPath, err)
	}
	if _, err := os.Stat(stagedAttachmentsRoot); err == nil {
		if err := os.Rename(stagedAttachmentsRoot, attachmentsDir); err != nil {
			return summary, fmt.Errorf("move restored attachments into place at %q: %w", attachmentsDir, err)
		}
	} else if err := os.MkdirAll(attachmentsDir, 0o700); err != nil {
		return summary, fmt.Errorf("create %q: %w", attachmentsDir, err)
	}

	for _, dir := range []string{dbDir, attachmentsDir, dataDir} {
		if err := fsyncDir(dir); err != nil {
			return summary, fmt.Errorf("fsync %q: %w", dir, err)
		}
	}

	committed = true
	_ = os.RemoveAll(stagingRoot)
	return summary, nil
}

func readManifest(tr *tar.Reader) (backup.ArchiveManifest, error) {
	var manifest backup.ArchiveManifest

	hdr, err := tr.Next()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return manifest, errors.New("archive is empty")
		}
		return manifest, fmt.Errorf("read first archive entry: %w", err)
	}
	if hdr.Name != backup.ManifestEntryName || hdr.Typeflag != tar.TypeReg {
		return manifest, fmt.Errorf(
			"archive's first entry is %q, want %q -- this does not look like an hho backup archive",
			hdr.Name, backup.ManifestEntryName)
	}
	if hdr.Size > restoreManifestMaxBytes {
		return manifest, fmt.Errorf(
			"%s declares %d bytes, more than %d -- refusing a suspiciously large manifest",
			backup.ManifestEntryName, hdr.Size, restoreManifestMaxBytes)
	}

	data, err := io.ReadAll(tr)
	if err != nil {
		return manifest, fmt.Errorf("read %s: %w", backup.ManifestEntryName, err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("decode %s: %w", backup.ManifestEntryName, err)
	}
	return manifest, nil
}

func latestSupportedSchemaVersion(ctx context.Context, scratchParent string) (int64, error) {
	dir, err := os.MkdirTemp(scratchParent, restoreSchemaProbeSubdir+"-*")
	if err != nil {
		return 0, fmt.Errorf("create schema probe directory under %q: %w", scratchParent, err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	probePath := filepath.Join(dir, "probe.db")
	store, err := storage.Open(ctx, storage.Config{Path: probePath})
	if err != nil {
		return 0, fmt.Errorf("open schema probe database: %w", err)
	}
	defer func() { _ = store.Close() }()
	return store.SchemaVersion(), nil
}

func dataDirHasExistingData(dbPath, attachmentsDir string) (bool, error) {
	if _, err := os.Stat(dbPath); err == nil {
		return true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("stat %q: %w", dbPath, err)
	}

	entries, err := os.ReadDir(attachmentsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read %q: %w", attachmentsDir, err)
	}
	return len(entries) > 0, nil
}

func probeServerNotRunning(dataDir string) (warning string, err error) {
	switch lockErr := serverlock.Probe(dataDir); {
	case lockErr == nil:
		return "", nil
	case serverlock.IsHeld(lockErr):
		return "", fmt.Errorf(
			"%s -- stop it before restoring (hho restore --force replaces existing data, "+
				"it is not permission to race a server that is still writing to it)", lockErr)
	case errors.Is(lockErr, serverlock.ErrUnsupported):
		return fmt.Sprintf(
			"could not verify that no \"hho serve\" is running against %s (%v) -- this data "+
				"directory's filesystem does not support the advisory lock this check relies on; "+
				"stop hho serve yourself before restoring",
			dataDir, lockErr), nil
	default:
		return "", fmt.Errorf("check whether a server is running against %s: %w", dataDir, lockErr)
	}
}

func probeDatabaseNotLive(ctx context.Context, dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %q: %w", dbPath, err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open %q to check for a live server: %w", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 2000"); err != nil {
		return fmt.Errorf("set busy_timeout probing %q: %w", dbPath, err)
	}
	if _, err := db.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "ROLLBACK"); err != nil {
		return fmt.Errorf("release probe lock on %q: %w", dbPath, err)
	}
	return nil
}

func extractArchiveBody(tr *tar.Reader, stagedDBPath, stagedAttachmentsRoot string, summary *restoreSummary) error {
	sawDB := false

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}

		switch {
		case hdr.Name == backup.DBEntryName:
			if sawDB {
				return fmt.Errorf("archive contains %s more than once", backup.DBEntryName)
			}
			if hdr.Typeflag != tar.TypeReg {
				return fmt.Errorf("%s is not a regular file entry (type %v)", backup.DBEntryName, hdr.Typeflag)
			}
			if err := extractRegularFile(tr, stagedDBPath, hdr.Size, 0o600); err != nil {
				return fmt.Errorf("extract %s: %w", backup.DBEntryName, err)
			}
			sawDB = true

		case strings.HasPrefix(hdr.Name, "attachments/"):
			if hdr.Typeflag != tar.TypeReg {
				return fmt.Errorf("attachment entry %q is not a regular file (type %v); refusing", hdr.Name, hdr.Typeflag)
			}
			destPath, err := safeAttachmentPath(stagedAttachmentsRoot, hdr.Name)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
				return fmt.Errorf("create attachment group directory for %q: %w", hdr.Name, err)
			}
			if err := extractRegularFile(tr, destPath, hdr.Size, 0o600); err != nil {
				return fmt.Errorf("extract attachment %q: %w", hdr.Name, err)
			}
			summary.AttachmentFiles++
			summary.AttachmentBytes += hdr.Size

		default:
			return fmt.Errorf("archive contains unexpected entry %q; refusing to restore an archive that does not match the documented layout", hdr.Name)
		}
	}

	if !sawDB {
		return fmt.Errorf("archive never contained %s; refusing an incomplete archive", backup.DBEntryName)
	}
	return nil
}

func safeAttachmentPath(root, entryName string) (string, error) {
	rel := strings.TrimPrefix(entryName, "attachments/")
	segs := strings.Split(rel, "/")
	if len(segs) != 2 || segs[0] == "" || segs[1] == "" {
		return "", fmt.Errorf("attachment entry %q does not have the documented <group_id>/<filename> shape", entryName)
	}
	for _, seg := range segs {
		if seg == "." || seg == ".." || strings.ContainsRune(seg, '\\') {
			return "", fmt.Errorf("attachment entry %q contains a path traversal segment", entryName)
		}
	}

	joined := filepath.Join(root, filepath.FromSlash(rel))
	relCheck, err := filepath.Rel(root, joined)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) || filepath.IsAbs(relCheck) {
		return "", fmt.Errorf("attachment entry %q escapes the attachments directory", entryName)
	}
	return joined, nil
}

func extractRegularFile(r io.Reader, destPath string, declaredSize int64, mode os.FileMode) error {
	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %q: %w", destPath, err)
	}

	written, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("write %q: %w", destPath, copyErr)
	}
	if written != declaredSize {
		return fmt.Errorf("short read extracting %q: wrote %d of %d declared bytes", destPath, written, declaredSize)
	}
	if closeErr != nil {
		return fmt.Errorf("close %q: %w", destPath, closeErr)
	}
	return nil
}

func preserveIfExists(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %q: %w", src, err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("move %q aside to %q: %w", src, dst, err)
	}
	return nil
}
