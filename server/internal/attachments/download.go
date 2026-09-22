package attachments

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path/filepath"
	"strings"
)

var ErrNoThumbnail = errors.New("attachments: this attachment has no thumbnail")

func InlineSafeContentType(contentType string) bool {
	return thumbnailContentTypes[contentType]
}

func OpenOriginal(root, groupID string, row storage.Attachment) (*os.File, error) {
	f, err := openUnderGroup(root, groupID, row.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("attachments: open original for %q: %w", row.ID, err)
	}
	return f, nil
}

func OpenThumbnail(root, groupID string, row storage.Attachment) (*os.File, error) {
	if !row.ThumbnailPath.Valid {
		return nil, ErrNoThumbnail
	}
	f, err := openUnderGroup(root, groupID, row.ThumbnailPath.String)
	if err != nil {
		return nil, fmt.Errorf("attachments: open thumbnail for %q: %w", row.ID, err)
	}
	return f, nil
}

func openUnderGroup(root, groupID, storagePath string) (*os.File, error) {
	candidate, err := resolveUnderGroup(root, groupID, storagePath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(candidate)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func resolveUnderGroup(root, groupID, storagePath string) (string, error) {
	if root == "" {
		return "", errors.New("attachments: resolveUnderGroup: root is empty")
	}
	if groupID == "" {
		return "", errors.New("attachments: resolveUnderGroup: groupID is empty")
	}
	if storagePath == "" {
		return "", errors.New("attachments: resolveUnderGroup: storagePath is empty")
	}

	boundary := filepath.Join(root, datadir.AttachmentsSubdir, groupID)
	candidate := filepath.Join(root, datadir.AttachmentsSubdir, storagePath)

	if candidate != boundary && !strings.HasPrefix(candidate, boundary+string(os.PathSeparator)) {
		return "", fmt.Errorf("attachments: storage path %q resolves outside group %q's own directory", storagePath, groupID)
	}
	return candidate, nil
}
