package labels

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"regexp"
)

var ErrVersionRequired = errors.New("labels: ExpectedVersion is required")

var ErrNameRequired = errors.New("labels: Name is required")

var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

var ErrColorInvalid = errors.New("labels: color must be a 6-digit hex triplet, e.g. #888888")

func ValidateColor(color string) error {
	if !colorPattern.MatchString(color) {
		return fmt.Errorf("%w (got %q)", ErrColorInvalid, color)
	}
	return nil
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("labels: generate id: %w", err)
	}
	return id.String(), nil
}
