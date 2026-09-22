package devicetoken

import (
	"database/sql"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testFixture struct {
	*Service
	storage *storage.Storage
	dbPath  string
	groupID string
	userID  string
}

func newFixture(t *testing.T) testFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db", "hho.db")
	store, err := storage.Open(t.Context(), storage.Config{Path: path})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	groupID, userID := mustUUID(t), mustUUID(t)
	if err := store.RegisterFirstUser(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    "Household",
		UserID:       userID,
		Username:     "alice",
		PasswordHash: "$argon2id$irrelevant-to-this-package$",
		Now:          time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	svc, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return testFixture{Service: svc, storage: store, dbPath: path, groupID: groupID, userID: userID}
}

func (f testFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+f.dbPath)
	if err != nil {
		t.Fatalf("open independent connection to %s: %v", f.dbPath, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close independent connection: %v", err)
		}
	}()
	if _, err := conn.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func mustUUID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return id.String()
}

func TestNewServiceRejectsNilStorage(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Error("NewService accepted a nil *storage.Storage")
	}
}

func TestIssueWithValidRequestSucceeds(t *testing.T) {
	f := newFixture(t)

	got, err := f.Issue(t.Context(), IssueRequest{
		GroupID:     f.groupID,
		UserID:      f.userID,
		DeviceLabel: "Sarah's Pixel",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if got.ID == "" {
		t.Error("ID is empty")
	}
	if got.Token == "" {
		t.Fatal("Token is empty")
	}
	if got.DeviceLabel != "Sarah's Pixel" {
		t.Errorf("DeviceLabel = %q, want %q", got.DeviceLabel, "Sarah's Pixel")
	}

	auth, err := f.storage.DeviceTokenAuth(t.Context(), bearertoken.Hash(got.Token))
	if err != nil {
		t.Fatalf("DeviceTokenAuth on the minted token: %v", err)
	}
	if auth.GroupID != f.groupID {
		t.Errorf("GroupID = %q, want %q", auth.GroupID, f.groupID)
	}
	if auth.UserID != f.userID {
		t.Errorf("UserID = %q, want %q", auth.UserID, f.userID)
	}
	if auth.Role != "owner" {
		t.Errorf("Role = %q, want %q", auth.Role, "owner")
	}
	if auth.Revoked {
		t.Error("Revoked = true for a freshly minted device token, want false")
	}
}

func TestIssueTrimsDeviceLabel(t *testing.T) {
	f := newFixture(t)
	got, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: "  Kitchen tablet  "})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if got.DeviceLabel != "Kitchen tablet" {
		t.Errorf("DeviceLabel = %q, want %q", got.DeviceLabel, "Kitchen tablet")
	}
}

func TestIssueRejectsEmptyDeviceLabel(t *testing.T) {
	f := newFixture(t)
	for _, label := range []string{"", "   ", "\t\n"} {
		if _, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: label}); !errors.Is(err, ErrDeviceLabelRequired) {
			t.Errorf("Issue(DeviceLabel=%q): error = %v, want ErrDeviceLabelRequired", label, err)
		}
	}
}

func TestIssueRejectsOverlongDeviceLabel(t *testing.T) {
	f := newFixture(t)
	long := strings.Repeat("x", maxDeviceLabelLength+1)
	if _, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, UserID: f.userID, DeviceLabel: long}); !errors.Is(err, ErrDeviceLabelTooLong) {
		t.Errorf("Issue(overlong label): error = %v, want ErrDeviceLabelTooLong", err)
	}
}

func TestIssueRejectsMissingIdentity(t *testing.T) {
	f := newFixture(t)
	cases := []IssueRequest{
		{GroupID: "", UserID: f.userID, DeviceLabel: "x"},
		{GroupID: f.groupID, UserID: "", DeviceLabel: "x"},
	}
	for _, req := range cases {
		if _, err := f.Issue(t.Context(), req); err == nil {
			t.Errorf("Issue(%+v) succeeded, want an error", req)
		}
	}
}
