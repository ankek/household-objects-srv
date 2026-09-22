package attachments

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path"
	"path/filepath"
	"time"
)

type Report struct {
	FilesRemoved      int
	RowsTombstoned    int
	ThumbnailsCleared int
	Errors            []error
}

func Reclaim(ctx context.Context, store *storage.Storage, dataDir string) (Report, error) {
	var report Report

	groupIDs, err := store.GroupIDs(ctx)
	if err != nil {
		return report, fmt.Errorf("attachments: reclaim: list groups: %w", err)
	}

	for _, gid := range groupIDs {
		if err := ctx.Err(); err != nil {
			return report, nil
		}

		scope, err := store.ForGroup(gid)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: bind group %q: %w", gid.String(), err))
			continue
		}
		reclaimGroup(ctx, scope, gid.String(), dataDir, &report)
	}

	return report, nil
}

func reclaimGroup(ctx context.Context, scope storage.Scope, groupID, dataDir string, report *Report) {
	rows, err := scope.Attachments().ListAll(ctx)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: list attachments for group %q: %w", groupID, err))
		return
	}

	live := make([]storage.Attachment, 0, len(rows))
	referenced := make(map[string]bool, len(rows)*2)
	for _, row := range rows {
		if row.DeletedAt.Valid {
			continue
		}
		live = append(live, row)
		referenced[path.Base(row.StoragePath)] = true
		if row.ThumbnailPath.Valid {
			referenced[path.Base(row.ThumbnailPath.String)] = true
		}
	}

	reclaimOrphanedFiles(groupID, dataDir, referenced, report)

	for _, row := range live {
		if err := ctx.Err(); err != nil {
			return
		}
		reclaimRow(ctx, scope, dataDir, groupID, row, report)
	}
}

const minOrphanFileAge = time.Hour

func reclaimOrphanedFiles(groupID, dataDir string, referenced map[string]bool, report *Report) {
	dir := filepath.Join(dataDir, datadir.AttachmentsSubdir, groupID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: read attachment directory for group %q: %w", groupID, err))
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if referenced[name] {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: stat orphan candidate %q in group %q: %w", name, groupID, err))
			continue
		}
		if time.Since(info.ModTime()) < minOrphanFileAge {
			continue
		}

		if err := removeUnderGroup(dataDir, groupID, path.Join(groupID, name)); err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: remove orphaned file %q in group %q: %w", name, groupID, err))
			continue
		}
		report.FilesRemoved++
	}
}

func reclaimRow(ctx context.Context, scope storage.Scope, dataDir, groupID string, row storage.Attachment, report *Report) {
	originalPath, err := resolveUnderGroup(dataDir, groupID, row.StoragePath)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: resolve original for attachment %q on item %q: %w", row.ID, row.ItemID, err))
		return
	}

	if _, err := os.Stat(originalPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: stat original for attachment %q on item %q: %w", row.ID, row.ItemID, err))
			return
		}
		if delErr := scope.Attachments().Delete(ctx, row.ItemID, row.ID, time.Now().UnixMilli()); delErr != nil {
			if errors.Is(delErr, storage.ErrNotFound) {
				return
			}
			report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: tombstone attachment %q on item %q with a missing original: %w", row.ID, row.ItemID, delErr))
			return
		}
		report.RowsTombstoned++
		return
	}

	if !row.ThumbnailPath.Valid {
		return
	}

	thumbnailPath, err := resolveUnderGroup(dataDir, groupID, row.ThumbnailPath.String)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: resolve thumbnail for attachment %q on item %q: %w", row.ID, row.ItemID, err))
		return
	}

	if _, err := os.Stat(thumbnailPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: stat thumbnail for attachment %q on item %q: %w", row.ID, row.ItemID, err))
			return
		}
		if clearErr := scope.Attachments().ClearThumbnailPath(ctx, row.ItemID, row.ID, time.Now().UnixMilli()); clearErr != nil {
			if errors.Is(clearErr, storage.ErrNotFound) {
				return
			}
			report.Errors = append(report.Errors, fmt.Errorf("attachments: reclaim: clear thumbnail path for attachment %q on item %q with a missing thumbnail file: %w", row.ID, row.ItemID, clearErr))
			return
		}
		report.ThumbnailsCleared++
	}
}
