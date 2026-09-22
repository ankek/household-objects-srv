package main

import (
	"bytes"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"path/filepath"
	"strings"
	"testing"
)

func seedTestGroup(t *testing.T, dataDir, username string) (groupID, userID string) {
	t.Helper()
	dbPath := filepath.Join(dataDir, "db", "hho.db")
	store, err := storage.Open(t.Context(), storage.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("seed: storage.Open: %v", err)
	}
	groupID, userID = "grp-1", "usr-1"
	if err := store.RegisterFirstUser(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    "Household",
		UserID:       userID,
		Username:     username,
		PasswordHash: "$argon2id$fake$original",
		Now:          1_700_000_000_000,
	}); err != nil {
		t.Fatalf("seed: RegisterFirstUser: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("seed: Close: %v", err)
	}
	return groupID, userID
}

func fakeDeps(now int64) resetPasswordDeps {
	return resetPasswordDeps{
		now:          func() int64 { return now },
		readPassword: readPassword,
		isTerminal:   func(uintptr) bool { return false },
	}
}

func TestResetPasswordCommandHappyPath(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	stdin, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString("new-correct-horse-battery\n"); err != nil {
		t.Fatalf("write password to pipe: %v", err)
	}
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := runResetPasswordWithDeps(
		[]string{"--username", "alice", "--data-dir", dataDir},
		stdin, &stdout, &stderr, fakeDeps(1_700_000_500_000),
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `Password reset for user "alice"`) {
		t.Errorf("stdout = %q, want it to report the reset", stdout.String())
	}

	store, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(dataDir, "db", "hho.db")})
	if err != nil {
		t.Fatalf("re-open storage: %v", err)
	}
	defer func() { _ = store.Close() }()
	u, err := store.UserForLogin(t.Context(), "alice")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	if u.PasswordHash == "$argon2id$fake$original" {
		t.Error("password_hash is unchanged after a successful reset")
	}
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") {
		t.Errorf("password_hash = %q, want a real argon2id PHC string (internal/auth.Hasher.Hash's output), not a hand-rolled encoding", u.PasswordHash)
	}
}

func TestResetPasswordCommandFailsLoudlyForUnknownUser(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	stdin, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString("new-correct-horse-battery\n"); err != nil {
		t.Fatalf("write password to pipe: %v", err)
	}
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := runResetPasswordWithDeps(
		[]string{"--username", "does-not-exist", "--data-dir", dataDir},
		stdin, &stdout, &stderr, fakeDeps(1_700_000_500_000),
	)
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an unknown username; stdout: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), `no such user "does-not-exist"`) {
		t.Errorf("stderr = %q, want it to name the unknown username", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing printed on failure", stdout.String())
	}
}

func TestResetPasswordCommandRejectsMissingUsername(t *testing.T) {
	dataDir := t.TempDir()
	stdin, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := runResetPasswordWithDeps([]string{"--data-dir", dataDir}, stdin, &stdout, &stderr, fakeDeps(1_700_000_500_000))
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr.String(), "--username is required") {
		t.Errorf("stderr = %q, want a message about the missing --username", stderr.String())
	}
}

func TestResetPasswordCommandRejectsAPasswordFlagOrPositionalArgument(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"unknown --password flag", []string{"--username", "alice", "--password", "hunter2345678", "--data-dir", dataDir}},
		{"password as a positional argument", []string{"--username", "alice", "hunter2", "--data-dir", dataDir}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdin, w, err := osPipe(t)
			if err != nil {
				t.Fatalf("pipe: %v", err)
			}
			_ = w.Close()

			var stdout, stderr bytes.Buffer
			code := runResetPasswordWithDeps(tc.args, stdin, &stdout, &stderr, fakeDeps(1_700_000_500_000))
			if code == 0 {
				t.Fatalf("exit code = 0 for args %v, want a usage error; stdout: %s stderr: %s", tc.args, stdout.String(), stderr.String())
			}
		})
	}
}

func TestResetPasswordCommandRejectsAnUndersizedPassword(t *testing.T) {
	dataDir := t.TempDir()
	seedTestGroup(t, dataDir, "alice")

	stdin, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString("short\n"); err != nil {
		t.Fatalf("write password to pipe: %v", err)
	}
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := runResetPasswordWithDeps(
		[]string{"--username", "alice", "--data-dir", dataDir},
		stdin, &stdout, &stderr, fakeDeps(1_700_000_500_000),
	)
	if code == 0 {
		t.Fatalf("exit code = 0 for a 5-byte password, want a rejection")
	}
	if !strings.Contains(stderr.String(), "8-256 bytes") {
		t.Errorf("stderr = %q, want it to state the length bound", stderr.String())
	}
}
