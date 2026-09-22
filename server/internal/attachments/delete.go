package attachments

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
)

func Delete(root, groupID string, row storage.Attachment) []error {
	var errs []error

	if err := removeUnderGroup(root, groupID, row.StoragePath); err != nil {
		errs = append(errs, fmt.Errorf("attachments: remove original for %q: %w", row.ID, err))
	}
	if row.ThumbnailPath.Valid {
		if err := removeUnderGroup(root, groupID, row.ThumbnailPath.String); err != nil {
			errs = append(errs, fmt.Errorf("attachments: remove thumbnail for %q: %w", row.ID, err))
		}
	}
	return errs
}

func removeUnderGroup(root, groupID, storagePath string) error {
	candidate, err := resolveUnderGroup(root, groupID, storagePath)
	if err != nil {
		return err
	}
	if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
