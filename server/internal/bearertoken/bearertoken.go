package bearertoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const ByteLength = 32

func New() (string, error) {
	raw := make([]byte, ByteLength)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("bearertoken: draw a token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
