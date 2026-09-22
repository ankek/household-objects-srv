package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"
)

const DefaultMaxUploadBytes int64 = 25 << 20

const sniffLen = 512

var ErrFilenameRequired = errors.New("attachments: a file part with a filename is required")

var ErrTooLarge = errors.New("attachments: file exceeds the upload limit")

type UploadParams struct {
	ItemID string

	GroupID string

	Category string

	OriginalFilename string
}

func (p UploadParams) validate() error {
	switch {
	case p.ItemID == "":
		return errors.New("attachments: UploadParams: ItemID is empty")
	case p.GroupID == "":
		return errors.New("attachments: UploadParams: GroupID is empty")
	case p.OriginalFilename == "":
		return ErrFilenameRequired
	}
	return ValidateCategory(p.Category)
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("attachments: generate id: %w", err)
	}
	return id.String(), nil
}

func Upload(ctx context.Context, items storage.ItemRepository, repo storage.AttachmentRepository, root string, maxBytes int64, p UploadParams, src io.Reader) (storage.Attachment, error) {
	if items == nil {
		return storage.Attachment{}, errors.New("attachments: Upload needs a non-nil storage.ItemRepository")
	}
	if repo == nil {
		return storage.Attachment{}, errors.New("attachments: Upload needs a non-nil storage.AttachmentRepository")
	}
	if root == "" {
		return storage.Attachment{}, errors.New("attachments: Upload needs a non-empty data-directory root")
	}
	if maxBytes <= 0 {
		return storage.Attachment{}, errors.New("attachments: Upload needs a positive maxBytes limit")
	}
	if err := p.validate(); err != nil {
		return storage.Attachment{}, err
	}

	if _, err := items.Get(ctx, p.ItemID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return storage.Attachment{}, fmt.Errorf("attachments: item %q: %w", p.ItemID, storage.ErrNotFound)
		}
		return storage.Attachment{}, fmt.Errorf("attachments: get item %q for upload: %w", p.ItemID, err)
	}

	id, err := newID()
	if err != nil {
		return storage.Attachment{}, err
	}

	tmpDir := filepath.Join(root, datadir.TmpSubdir)
	tmpFile, err := os.CreateTemp(tmpDir, "attachment-*.tmp")
	if err != nil {
		return storage.Attachment{}, fmt.Errorf("attachments: create staging file: %w", err)
	}
	tmpPath := tmpFile.Name()
	removeTmpOnReturn := true
	defer func() {
		if removeTmpOnReturn {
			_ = os.Remove(tmpPath)
		}
	}()

	head := make([]byte, sniffLen)
	n, err := io.ReadFull(src, head)
	switch {
	case err == nil, errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
	default:
		return storage.Attachment{}, fmt.Errorf("attachments: read upload for content-type sniffing: %w", err)
	}
	contentType := http.DetectContentType(head[:n])
	src = io.MultiReader(bytes.NewReader(head[:n]), src)

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmpFile, hasher), io.LimitReader(src, maxBytes+1))
	if copyErr != nil {
		_ = tmpFile.Close()
		return storage.Attachment{}, fmt.Errorf("attachments: write upload to staging file: %w", copyErr)
	}
	if written > maxBytes {
		_ = tmpFile.Close()
		return storage.Attachment{}, fmt.Errorf("attachments: file exceeds the %d-byte upload limit: %w", maxBytes, ErrTooLarge)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return storage.Attachment{}, fmt.Errorf("attachments: fsync staging file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return storage.Attachment{}, fmt.Errorf("attachments: close staging file: %w", err)
	}

	finalDir := filepath.Join(root, datadir.AttachmentsSubdir, p.GroupID)
	if err := os.MkdirAll(finalDir, 0o700); err != nil {
		return storage.Attachment{}, fmt.Errorf("attachments: create %s: %w", finalDir, err)
	}
	finalPath := filepath.Join(finalDir, id)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return storage.Attachment{}, fmt.Errorf("attachments: rename staged upload into place: %w", err)
	}
	removeTmpOnReturn = false

	if err := fsyncDir(finalDir); err != nil {
		return storage.Attachment{}, err
	}

	created, err := repo.Create(ctx, storage.CreateAttachmentParams{
		ID:               id,
		ItemID:           p.ItemID,
		Category:         p.Category,
		OriginalFilename: p.OriginalFilename,
		ContentType:      contentType,
		SizeBytes:        written,
		StoragePath:      path.Join(p.GroupID, id),
		SHA256:           hex.EncodeToString(hasher.Sum(nil)),
		Now:              time.Now().UnixMilli(),
	})
	if err != nil {
		_ = os.Remove(finalPath)
		return storage.Attachment{}, err
	}

	if updated, ok := generateAndRecordThumbnail(ctx, repo, root, p.GroupID, p.ItemID, id, contentType); ok {
		created = updated
	}
	return created, nil
}

func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("attachments: open %s for fsync: %w", dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("attachments: fsync %s: %w", dir, err)
	}
	return nil
}
