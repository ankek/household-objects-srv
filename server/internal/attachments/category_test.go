package attachments

import (
	"errors"
	"testing"
)

func TestValidateCategoryRejectsAnUnknownToken(t *testing.T) {
	for _, category := range []string{"not-a-real-category", "", "IMAGE", "images", " image"} {
		t.Run(category, func(t *testing.T) {
			err := ValidateCategory(category)
			if !errors.Is(err, ErrCategoryInvalid) {
				t.Errorf("ValidateCategory(%q) = %v, want it to wrap ErrCategoryInvalid", category, err)
			}
		})
	}
}

func TestValidateCategoryAcceptsEveryDefinedToken(t *testing.T) {
	for _, category := range []string{CategoryImage, CategoryManual, CategoryWarranty, CategoryReceipt, CategoryGeneral} {
		t.Run(category, func(t *testing.T) {
			if err := ValidateCategory(category); err != nil {
				t.Errorf("ValidateCategory(%q) = %v, want nil", category, err)
			}
		})
	}
}
