package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
)

const ArchiveFormatVersion = 1

const (
	ManifestEntryName = "manifest.json"
	DBEntryName       = "db/hho.db"
)

type ArchiveManifest struct {
	FormatVersion       int   `json:"format_version"`
	CreatedAtUnixMillis int64 `json:"created_at_unix_ms"`
	SchemaVersion       int64 `json:"schema_version"`
}

type ArchiveStats struct {
	AttachmentFiles        int
	AttachmentBytes        int64
	AttachmentFilesSkipped int
}

func WriteArchive(ctx context.Context, store *storage.Storage, dataDir string, w io.Writer) (ArchiveStats, error) {
	var stats ArchiveStats

	if store == nil {
		return stats, errors.New("backup: write archive: storage is nil")
	}
	if dataDir == "" {
		return stats, errors.New("backup: write archive: data directory is empty")
	}
	if w == nil {
		return stats, errors.New("backup: write archive: destination writer is nil")
	}

	stagedDB, err := stageSnapshotForArchive(ctx, store, dataDir)
	if err != nil {
		return stats, err
	}
	defer func() { _ = os.Remove(stagedDB) }()

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	if err := writeManifestEntry(tw, store); err != nil {
		return stats, err
	}
	if err := writeDBEntry(tw, stagedDB); err != nil {
		return stats, err
	}
	if err := writeAttachmentsTree(ctx, tw, dataDir, &stats); err != nil {
		return stats, err
	}

	if err := tw.Close(); err != nil {
		return stats, fmt.Errorf("backup: close tar stream: %w", err)
	}
	if err := gz.Close(); err != nil {
		return stats, fmt.Errorf("backup: close gzip stream: %w", err)
	}
	return stats, nil
}

func stageSnapshotForArchive(ctx context.Context, store *storage.Storage, dataDir string) (string, error) {
	tmpDir := filepath.Join(dataDir, datadir.TmpSubdir)
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", fmt.Errorf("backup: create staging directory %q: %w", tmpDir, err)
	}
	name, err := stageName(tmpDir)
	if err != nil {
		return "", fmt.Errorf("backup: stage archive snapshot temp name: %w", err)
	}
	if err := Snapshot(ctx, store, name); err != nil {
		return "", fmt.Errorf("backup: snapshot database for archive: %w", err)
	}
	return name, nil
}

func writeManifestEntry(tw *tar.Writer, store *storage.Storage) error {
	m := ArchiveManifest{
		FormatVersion:       ArchiveFormatVersion,
		CreatedAtUnixMillis: time.Now().UnixMilli(),
		SchemaVersion:       store.SchemaVersion(),
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("backup: marshal archive manifest: %w", err)
	}
	hdr := &tar.Header{
		Name:     ManifestEntryName,
		Typeflag: tar.TypeReg,
		Mode:     0o600,
		Size:     int64(len(data)),
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write manifest tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("backup: write manifest entry: %w", err)
	}
	return nil
}

func writeDBEntry(tw *tar.Writer, stagedDB string) error {
	f, err := os.Open(stagedDB)
	if err != nil {
		return fmt.Errorf("backup: open staged database snapshot: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("backup: stat staged database snapshot: %w", err)
	}

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("backup: build tar header for database snapshot: %w", err)
	}
	hdr.Name = DBEntryName
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write database tar header: %w", err)
	}

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("backup: stream database snapshot into archive: %w", err)
	}
	return nil
}

func writeAttachmentsTree(ctx context.Context, tw *tar.Writer, dataDir string, stats *ArchiveStats) error {
	root := filepath.Join(dataDir, datadir.AttachmentsSubdir)
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("backup: stat attachments root %q: %w", root, err)
	}

	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("backup: walk attachments tree at %q: %w", p, err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("backup: relativize attachment path %q: %w", p, err)
		}
		name := path.Join("attachments", filepath.ToSlash(rel))

		return writeAttachmentEntry(tw, p, name, stats)
	})
}

func writeAttachmentEntry(tw *tar.Writer, diskPath, name string, stats *ArchiveStats) error {
	f, err := os.Open(diskPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			stats.AttachmentFilesSkipped++
			return nil
		}
		return fmt.Errorf("backup: open attachment %q: %w", diskPath, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("backup: stat opened attachment %q: %w", diskPath, err)
	}
	if !info.Mode().IsRegular() {
		stats.AttachmentFilesSkipped++
		return nil
	}

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("backup: build tar header for attachment %q: %w", diskPath, err)
	}
	hdr.Name = name

	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write tar header for attachment %q: %w", diskPath, err)
	}

	written, err := io.Copy(tw, f)
	if err != nil {
		return fmt.Errorf("backup: stream attachment %q into archive: %w", diskPath, err)
	}
	if written != info.Size() {
		return fmt.Errorf("backup: short read streaming attachment %q into archive: wrote %d of %d declared bytes",
			diskPath, written, info.Size())
	}

	stats.AttachmentFiles++
	stats.AttachmentBytes += written
	return nil
}
