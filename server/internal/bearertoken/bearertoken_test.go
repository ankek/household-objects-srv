package bearertoken

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNewProducesURLAndCookieSafeTokens(t *testing.T) {
	for i := 0; i < 20; i++ {
		token, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if token == "" {
			t.Fatal("New produced an empty token")
		}
		if strings.ContainsAny(token, "=+/ \t\r\n\";,\\") {
			t.Fatalf("New produced a token with an unsafe character: %q", token)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("token %q does not round-trip through RawURLEncoding: %v", token, err)
		}
		if len(decoded) != ByteLength {
			t.Fatalf("decoded token is %d bytes, want %d", len(decoded), ByteLength)
		}
	}
}

func TestNewIsHighEntropy(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a == b {
		t.Fatalf("two calls to New produced the same token: %q", a)
	}
}

func TestHashIsDeterministic(t *testing.T) {
	token, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first := Hash(token)
	second := Hash(token)
	if first != second {
		t.Fatalf("Hash(%q) = %q then %q, want the same digest both times", token, first, second)
	}
	if first == "" {
		t.Fatal("Hash produced an empty digest")
	}
}

func TestHashDistinguishesDifferentTokens(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if Hash(a) == Hash(b) {
		t.Fatalf("Hash collided for two distinct tokens %q and %q", a, b)
	}
}
