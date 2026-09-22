package backup

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ScheduledBackupPrefix          = "hho-scheduled-backup-"
	scheduledBackupSuffix          = ".tar.gz"
	scheduledBackupTimestampLayout = "20060102T150405Z"
)

func ScheduledBackupFilename(now time.Time) string {
	return ScheduledBackupPrefix + now.UTC().Format(scheduledBackupTimestampLayout) + scheduledBackupSuffix
}

func scheduledBackupTimestamp(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, ScheduledBackupPrefix) || !strings.HasSuffix(name, scheduledBackupSuffix) {
		return time.Time{}, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(name, ScheduledBackupPrefix), scheduledBackupSuffix)
	ts, err := time.Parse(scheduledBackupTimestampLayout, raw)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

type ScheduledResult struct {
	Path        string
	Stats       ArchiveStats
	Pruned      int
	PruneErrors []error
}

func RunScheduled(ctx context.Context, store *storage.Storage, dataDir string, retain int) (ScheduledResult, error) {
	var result ScheduledResult

	if store == nil {
		return result, errors.New("backup: scheduled: storage is nil")
	}
	if dataDir == "" {
		return result, errors.New("backup: scheduled: data directory is empty")
	}
	if retain <= 0 {
		return result, fmt.Errorf("backup: scheduled: retention count must be positive, got %d", retain)
	}

	dir := filepath.Join(dataDir, datadir.BackupsSubdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return result, fmt.Errorf("backup: scheduled: create backups directory %q: %w", dir, err)
	}

	destPath := filepath.Join(dir, ScheduledBackupFilename(time.Now()))
	stats, err := writeArchiveAtomically(ctx, store, dataDir, destPath)
	result.Stats = stats
	if err != nil {
		return result, fmt.Errorf("backup: scheduled: write archive: %w", err)
	}
	result.Path = destPath

	pruned, pruneErrs := pruneScheduledBackups(dir, retain)
	result.Pruned = pruned
	result.PruneErrors = pruneErrs
	return result, nil
}

func writeArchiveAtomically(ctx context.Context, store *storage.Storage, dataDir, destPath string) (ArchiveStats, error) {
	var stats ArchiveStats

	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".hho-scheduled-backup-*.tar.gz.tmp")
	if err != nil {
		return stats, fmt.Errorf("backup: scheduled: stage archive in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	stats, err = WriteArchive(ctx, store, dataDir, tmp)
	if err != nil {
		return stats, err
	}
	if err := tmp.Close(); err != nil {
		return stats, fmt.Errorf("backup: scheduled: close staged archive %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return stats, fmt.Errorf("backup: scheduled: move archive into place at %q: %w", destPath, err)
	}
	removeTmp = false

	if err := fsyncDir(dir); err != nil {
		return stats, fmt.Errorf("backup: scheduled: fsync backups directory %q: %w", dir, err)
	}
	return stats, nil
}

func pruneScheduledBackups(dir string, retain int) (int, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, []error{fmt.Errorf("backup: scheduled: list %q for retention: %w", dir, err)}
	}

	type candidate struct {
		name string
		ts   time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ts, ok := scheduledBackupTimestamp(e.Name())
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{name: e.Name(), ts: ts})
	}
	if len(candidates) <= retain {
		return 0, nil
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ts.Before(candidates[j].ts) })

	excess := candidates[:len(candidates)-retain]
	var errs []error
	pruned := 0
	for _, c := range excess {
		path := filepath.Join(dir, c.name)
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			errs = append(errs, fmt.Errorf("backup: scheduled: remove old backup %q: %w", c.name, err))
			continue
		}
		pruned++
	}
	return pruned, errs
}
