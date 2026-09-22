package customfields

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
)

var ErrVersionRequired = errors.New("customfields: ExpectedVersion is required")

var ErrNameRequired = errors.New("customfields: Name is required")

var fieldTypes = map[string]bool{
	"text":    true,
	"number":  true,
	"boolean": true,
	"date":    true,
}

var ErrFieldTypeInvalid = errors.New("customfields: field_type must be one of text, number, boolean, date")

func ValidateFieldType(fieldType string) error {
	if !fieldTypes[fieldType] {
		return fmt.Errorf("%w (got %q)", ErrFieldTypeInvalid, fieldType)
	}
	return nil
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("customfields: generate id: %w", err)
	}
	return id.String(), nil
}
