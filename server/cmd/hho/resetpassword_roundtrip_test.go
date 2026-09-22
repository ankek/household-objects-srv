package main

import (
	"bytes"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"path/filepath"
	"strings"
	"testing"
)

func TestResetPasswordRoundTripsThroughVerify(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	stdin, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	const newPassword = "new-correct-horse-battery"
	if _, err := w.WriteString(newPassword + "\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"user", "reset-password", "--username", "alice", "--data-dir", dataDir}, stdin, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, stderr.String())
	}

	store, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(dataDir, "db", "hho.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()
	u, err := store.UserForLogin(t.Context(), "alice")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	t.Logf("stored hash prefix: %s", strings.SplitN(u.PasswordHash, "$m=", 2)[0])

	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	if err := hasher.Verify(t.Context(), u.PasswordHash, []byte(newPassword)); err != nil {
		t.Fatalf("the password the operator just set does NOT authenticate: %v", err)
	}
	if err := hasher.Verify(t.Context(), u.PasswordHash, []byte("wrong-password-entirely")); err == nil {
		t.Fatal("a WRONG password authenticates against the reset hash")
	}
}
