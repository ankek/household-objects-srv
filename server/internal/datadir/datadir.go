package datadir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	DBSubdir          = "db"
	AttachmentsSubdir = "attachments"
	BackupsSubdir     = "backups"
	TmpSubdir         = "tmp"
)

const dirMode fs.FileMode = 0o700

func Ensure(root string) error {
	if root == "" {
		return errors.New("datadir: root is empty")
	}

	info, err := os.Stat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf(
			"datadir: %s does not exist; it is the volume mount point and this server will not create it "+
				"(a missing root usually means an unmounted volume or a wrong HHO_DATA_DIR, and creating it "+
				"would write the database into throwaway container storage instead)", root)
	case err != nil:
		return fmt.Errorf("datadir: stat %s: %w", root, err)
	case !info.IsDir():
		return fmt.Errorf("datadir: %s exists but is not a directory", root)
	}

	for _, sub := range []string{DBSubdir, AttachmentsSubdir, BackupsSubdir, TmpSubdir} {
		path := filepath.Join(root, sub)
		if err := os.MkdirAll(path, dirMode); err != nil {
			return fmt.Errorf("datadir: create %s: %w", path, err)
		}
	}

	if err := checkWritable(filepath.Join(root, DBSubdir)); err != nil {
		return err
	}

	return emptyTmp(filepath.Join(root, TmpSubdir))
}

func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".hho-writable-*")
	if err != nil {
		return fmt.Errorf(
			"datadir: %s is not writable by this process (uid %d); check the volume's ownership and that it is not mounted read-only: %w",
			dir, os.Getuid(), err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		return fmt.Errorf("datadir: close write probe in %s: %w", dir, err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("datadir: remove write probe %s: %w", name, err)
	}
	return nil
}

func emptyTmp(tmp string) error {
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return fmt.Errorf("datadir: read %s: %w", tmp, err)
	}
	for _, e := range entries {
		path := filepath.Join(tmp, e.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("datadir: remove stale staging file %s: %w", path, err)
		}
	}
	return nil
}
