package attachments

import (
	"errors"
	"fmt"
)

const (
	CategoryImage    = "image"
	CategoryManual   = "manual"
	CategoryWarranty = "warranty"
	CategoryReceipt  = "receipt"
	CategoryGeneral  = "general"
)

var validCategories = map[string]bool{
	CategoryImage:    true,
	CategoryManual:   true,
	CategoryWarranty: true,
	CategoryReceipt:  true,
	CategoryGeneral:  true,
}

var ErrCategoryInvalid = errors.New("attachments: category must be one of image, manual, warranty, receipt, general")

func ValidateCategory(category string) error {
	if !validCategories[category] {
		return fmt.Errorf("%w (got %q)", ErrCategoryInvalid, category)
	}
	return nil
}
