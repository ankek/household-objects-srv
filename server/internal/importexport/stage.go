package importexport

import (
	"encoding/csv"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"io"
	"os"
	"path/filepath"
)

const importsSubdir = "imports"

const DefaultMaxUploadBytes int64 = 10 << 20

var ErrUploadTooLarge = errors.New("importexport: file exceeds the upload limit")

var ErrInvalidCSV = errors.New("importexport: not a valid native CSV import")

func StagingPath(dataDir, groupID, importID string) string {
	return filepath.Join(dataDir, datadir.TmpSubdir, importsSubdir, groupID, importID+".csv")
}

func Stage(dataDir, groupID, importID string, r io.Reader, maxBytes int64) error {
	if dataDir == "" {
		return errors.New("importexport: Stage needs a non-empty data directory root")
	}
	if groupID == "" {
		return errors.New("importexport: Stage needs a non-empty groupID")
	}
	if importID == "" {
		return errors.New("importexport: Stage needs a non-empty importID")
	}
	if maxBytes <= 0 {
		return errors.New("importexport: Stage needs a positive maxBytes limit")
	}

	tmpDir := filepath.Join(dataDir, datadir.TmpSubdir)
	tmpFile, err := os.CreateTemp(tmpDir, "import-*.tmp")
	if err != nil {
		return fmt.Errorf("importexport: create staging file: %w", err)
	}
	tmpPath := tmpFile.Name()
	removeTmpOnReturn := true
	defer func() {
		if removeTmpOnReturn {
			_ = os.Remove(tmpPath)
		}
	}()

	written, copyErr := io.Copy(tmpFile, io.LimitReader(r, maxBytes+1))
	if copyErr != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("importexport: write staged upload: %w", copyErr)
	}
	if written > maxBytes {
		_ = tmpFile.Close()
		return fmt.Errorf("importexport: file exceeds the %d-byte upload limit: %w", maxBytes, ErrUploadTooLarge)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("importexport: fsync staging file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("importexport: close staging file: %w", err)
	}

	if err := validateStagedHeader(tmpPath); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCSV, err)
	}

	finalDir := filepath.Join(dataDir, datadir.TmpSubdir, importsSubdir, groupID)
	if err := os.MkdirAll(finalDir, 0o700); err != nil {
		return fmt.Errorf("importexport: create %s: %w", finalDir, err)
	}
	finalPath := filepath.Join(finalDir, importID+".csv")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("importexport: rename staged upload into place: %w", err)
	}
	removeTmpOnReturn = false

	return fsyncDir(finalDir)
}

func validateStagedHeader(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reopen staged upload for header validation: %w", err)
	}
	defer func() { _ = f.Close() }()

	_, err = readHeaderLayout(csv.NewReader(f))
	return err
}

func RemoveStaged(dataDir, groupID, importID string) error {
	if err := os.Remove(StagingPath(dataDir, groupID, importID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("importexport: remove staged upload: %w", err)
	}
	return nil
}

func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("importexport: open %s for fsync: %w", dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("importexport: fsync %s: %w", dir, err)
	}
	return nil
}
