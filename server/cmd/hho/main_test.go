package main

import (
	"bytes"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPrintsVersionWithNoArguments(t *testing.T) {
	stdin, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := run(nil, stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(nil) exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "hho ") {
		t.Errorf("run(nil) stdout = %q, want it to start with \"hho \"", stdout.String())
	}
}

func TestRunRejectsAnUnknownTopLevelCommand(t *testing.T) {
	stdin, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"frobnicate"}, stdin, &stdout, &stderr)
	if code == 0 {
		t.Fatal("run([\"frobnicate\"]) exit code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), `unknown command "frobnicate"`) {
		t.Errorf("stderr = %q, want it to name the unknown command", stderr.String())
	}
}

func TestRunUserRejectsAnUnknownSubcommand(t *testing.T) {
	stdin, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"user", "frobnicate"}, stdin, &stdout, &stderr)
	if code == 0 {
		t.Fatal(`run(["user", "frobnicate"]) exit code = 0, want non-zero`)
	}
	if !strings.Contains(stderr.String(), `unknown "user" subcommand "frobnicate"`) {
		t.Errorf("stderr = %q, want it to name the unknown subcommand", stderr.String())
	}
}

func TestRunUserResetPasswordEndToEnd(t *testing.T) {
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
	code := run([]string{"user", "reset-password", "--username", "alice", "--data-dir", dataDir}, stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `Password reset for user "alice"`) {
		t.Errorf("stdout = %q, want the success message", stdout.String())
	}
}

func TestResetPasswordCommandRevokesCredentialsEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	groupID, userID := seedTestGroup(t, dataDir, "alice")

	dbPath := filepath.Join(dataDir, "db", "hho.db")
	store, err := storage.Open(t.Context(), storage.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("re-open storage to seed credentials: %v", err)
	}
	if err := store.CreateSession(t.Context(), storage.SessionParams{
		ID: "ses-1", GroupID: groupID, UserID: userID,
		TokenHash: "hash-1", ExpiresAt: 1_700_000_100_000, Now: 1_700_000_000_000,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.CreateDeviceToken(t.Context(), storage.DeviceTokenParams{
		ID: "dvc-1", GroupID: groupID, UserID: userID,
		TokenHash: "hash-1", DeviceLabel: "Alice's Phone", Now: 1_700_000_000_000,
	}); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

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
	if !strings.Contains(stdout.String(), "Revoked 1 session(s) and 1 device token(s)") {
		t.Errorf("stdout = %q, want it to report exactly one session and one device token revoked", stdout.String())
	}

	store, err = storage.Open(t.Context(), storage.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("re-open storage to verify: %v", err)
	}
	defer func() { _ = store.Close() }()
	sessions, err := store.ListSessions(t.Context(), groupID, userID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Revoked {
		t.Fatalf("ListSessions = %+v, want the one session Revoked=true", sessions)
	}
	devices, err := store.ListDeviceTokens(t.Context(), groupID, userID)
	if err != nil {
		t.Fatalf("ListDeviceTokens: %v", err)
	}
	if len(devices) != 1 || !devices[0].Revoked {
		t.Fatalf("ListDeviceTokens = %+v, want the one device token Revoked=true", devices)
	}
}
