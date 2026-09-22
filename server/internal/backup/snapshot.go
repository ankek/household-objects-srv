package backup

import (
	"context"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path/filepath"
)

const snapshotFileMode = 0o600

func Snapshot(ctx context.Context, store *storage.Storage, dstPath string) error {
	if dstPath == "" {
		return errors.New("backup: snapshot destination path is empty")
	}
	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("backup: create snapshot directory %q: %w", dir, err)
	}

	tmpPath, err := stageName(dir)
	if err != nil {
		return fmt.Errorf("backup: stage snapshot temp name: %w", err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := store.VacuumInto(ctx, tmpPath); err != nil {
		return fmt.Errorf("backup: vacuum into %q: %w", tmpPath, err)
	}

	if err := os.Chmod(tmpPath, snapshotFileMode); err != nil {
		return fmt.Errorf("backup: restrict snapshot mode on %q: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("backup: move snapshot into place at %q: %w", dstPath, err)
	}
	removeTmp = false

	if err := fsyncDir(dir); err != nil {
		return fmt.Errorf("backup: fsync snapshot directory %q: %w", dir, err)
	}
	return nil
}

func stageName(dir string) (string, error) {
	f, err := os.CreateTemp(dir, "snapshot-*.db.tmp")
	if err != nil {
		return "", err
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return "", closeErr
	}
	if removeErr != nil {
		return "", removeErr
	}
	return name, nil
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
