package items

import (
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/google/uuid"
)

var ErrNameRequired = errors.New("items: Name is required")

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("items: generate id: %w", err)
	}
	return id.String(), nil
}

const shortCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

const shortCodeLength = 8

func newShortCode() (string, error) {
	raw := make([]byte, shortCodeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("items: draw a short code: %w", err)
	}
	out := make([]byte, shortCodeLength)
	for i, b := range raw {
		out[i] = shortCodeAlphabet[int(b)%len(shortCodeAlphabet)]
	}
	return string(out), nil
}
