package locations

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
)

var ErrNameRequired = errors.New("locations: Name is required")

var ErrVersionRequired = errors.New("locations: ExpectedVersion is required")

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("locations: generate id: %w", err)
	}
	return id.String(), nil
}
