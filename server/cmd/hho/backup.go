package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/backup"
	"github.com/ankek/Household-Objects-Dev/server/internal/config"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const backupUsage = `usage: hho backup [--output path] [--data-dir dir]

Writes FR-132's whole backup archive -- a compacted database snapshot plus the attachments tree --
as a single gzip-compressed tar stream. It is the same archive GET /api/v1/backup serves, built
from the same underlying primitive; this is the offline path to it, for a cron job or an operator
at a terminal rather than an authenticated admin browsing to the endpoint.

  --output    destination: a file path, a directory, or "-" for stdout (default: "-", i.e. stdout)
  --data-dir  override the data directory (default: $HHO_DATA_DIR, or /data)

With no --output (or "--output -"), the archive is written to stdout and nothing else -- every
other message this command has to say goes to stderr instead -- so it can be piped straight into
another tool without that tool ever seeing anything but archive bytes:

    hho backup | gpg --symmetric --output hho-backup.tar.gz.gpg
    hho backup | ssh backup-host 'cat > /backups/hho-backup.tar.gz'

Given a file path, the archive is written atomically: staged next to the destination, then renamed
into place once complete, so a reader (or a second backup run racing this one at the same path)
never observes a partially-written archive under the final name. Given an existing directory, a
timestamped filename in the same shape GET /api/v1/backup suggests is generated inside it.

A one-line summary (attachment files included, and how many were skipped because they were deleted
while the archive was being built -- both ordinary, not a sign anything went wrong) is always
printed to stderr on success.
`

func backupFilename(now time.Time) string {
	return "hho-backup-" + now.UTC().Format("20060102T150405Z") + ".tar.gz"
}

func runBackup(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { _, _ = fmt.Fprint(stderr, backupUsage) }
	output := flags.String("output", "-", `destination: a file path, a directory, or "-" for stdout`)
	dataDirFlag := flags.String("data-dir", "", "override the data directory (default: $HHO_DATA_DIR, or /data)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "hho backup: unexpected argument(s) %v\n", flags.Args())
		flags.Usage()
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho backup: %v\n", err)
		return 1
	}
	if *dataDirFlag != "" {
		cfg.DataDir = *dataDirFlag
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbPath := cfg.DatabasePath()
	store, err := storage.Open(ctx, storage.Config{Path: dbPath})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho backup: open database at %s: %v\n", dbPath, err)
		return 1
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			_, _ = fmt.Fprintf(stderr, "hho backup: close database: %v\n", cerr)
		}
	}()

	dest := *output
	var (
		stats     backup.ArchiveStats
		destLabel string
	)
	if dest == "" || dest == "-" {
		destLabel = "stdout"
		stats, err = backup.WriteArchive(ctx, store, cfg.DataDir, stdout)
	} else {
		resolved, rerr := resolveOutputPath(dest)
		if rerr != nil {
			_, _ = fmt.Fprintf(stderr, "hho backup: resolve --output %q: %v\n", dest, rerr)
			return 1
		}
		destLabel = resolved
		stats, err = writeArchiveToFile(ctx, store, cfg.DataDir, resolved)
	}

	if err != nil {
		switch {
		case isDatabaseLocked(err):
			_, _ = fmt.Fprintf(stderr, "hho backup: the database is locked -- is \"hho serve\" already running against %s? the snapshot timed out waiting for the write connection; try again: %v\n", dbPath, err)
		default:
			_, _ = fmt.Fprintf(stderr, "hho backup: %v\n", err)
		}
		return 1
	}

	_, _ = fmt.Fprintf(stderr, "hho backup: wrote %s (%d attachment file(s), %d byte(s); %d skipped mid-walk)\n",
		destLabel, stats.AttachmentFiles, stats.AttachmentBytes, stats.AttachmentFilesSkipped)
	return 0
}

func resolveOutputPath(path string) (string, error) {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return filepath.Join(path, backupFilename(time.Now())), nil
	case err == nil, errors.Is(err, fs.ErrNotExist):
		return path, nil
	default:
		return "", err
	}
}

func writeArchiveToFile(ctx context.Context, store *storage.Storage, dataDir, destPath string) (backup.ArchiveStats, error) {
	var stats backup.ArchiveStats

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return stats, fmt.Errorf("create backup destination directory %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".hho-backup-*.tar.gz.tmp")
	if err != nil {
		return stats, fmt.Errorf("stage backup archive in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	stats, err = backup.WriteArchive(ctx, store, dataDir, tmp)
	if err != nil {
		return stats, err
	}
	if err := tmp.Close(); err != nil {
		return stats, fmt.Errorf("close staged backup archive %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return stats, fmt.Errorf("move backup archive into place at %q: %w", destPath, err)
	}
	removeTmp = false

	if err := fsyncDir(dir); err != nil {
		return stats, fmt.Errorf("fsync backup destination directory %q: %w", dir, err)
	}
	return stats, nil
}

func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open %s for fsync: %w", dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", dir, err)
	}
	return nil
}
