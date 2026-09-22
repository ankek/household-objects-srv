package groups

import (
	"database/sql"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	_ "modernc.org/sqlite"
	"path/filepath"
	"strings"
	"testing"
)

type testService struct {
	*Service
	dbPath string
}

func (ts testService) row(t *testing.T, query string, args ...any) *sql.Row {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+ts.dbPath)
	if err != nil {
		t.Fatalf("open independent connection to %s: %v", ts.dbPath, err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close independent connection: %v", err)
		}
	})
	return conn.QueryRowContext(t.Context(), query, args...)
}

func newTestService(t *testing.T) testService {
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

	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}

	svc, err := NewService(store, hasher)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return testService{Service: svc, dbPath: path}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}
	store, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(t.TempDir(), "db", "hho.db")})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	if _, err := NewService(nil, hasher); err == nil {
		t.Error("NewService accepted a nil *storage.Storage")
	}
	if _, err := NewService(store, nil); err == nil {
		t.Error("NewService accepted a nil *auth.Hasher")
	}
}

func TestRegisterCreatesTheOwner(t *testing.T) {
	svc := newTestService(t)

	got, err := svc.Register(t.Context(), RegisterRequest{
		Username: "  alice  ",
		Password: []byte("correct horse battery staple"),
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got.Username != "alice" {
		t.Errorf("Registered.Username = %q, want the trimmed %q", got.Username, "alice")
	}
	if got.GroupID == "" || got.UserID == "" {
		t.Fatalf("Registered has an empty id: %+v", got)
	}

	var storedHash, role, username string
	if err := svc.row(t, "SELECT username, password_hash, role FROM users WHERE id = ?", got.UserID).
		Scan(&username, &storedHash, &role); err != nil {
		t.Fatalf("read created user: %v", err)
	}
	if username != "alice" {
		t.Errorf("stored username = %q, want %q", username, "alice")
	}
	if role != "owner" {
		t.Errorf("stored role = %q, want %q", role, "owner")
	}
	if storedHash == "correct horse battery staple" {
		t.Fatal("password_hash equals the plaintext password; it was never hashed")
	}

	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}
	if err := hasher.Verify(t.Context(), storedHash, []byte("correct horse battery staple")); err != nil {
		t.Errorf("Verify(correct password) = %v, want nil", err)
	}
	if err := hasher.Verify(t.Context(), storedHash, []byte("wrong password")); !errors.Is(err, auth.ErrMismatch) {
		t.Errorf("Verify(wrong password) = %v, want ErrMismatch", err)
	}

	var groupName string
	if err := svc.row(t, "SELECT name FROM groups WHERE id = ?", got.GroupID).Scan(&groupName); err != nil {
		t.Fatalf("read created group: %v", err)
	}
	if groupName != DefaultGroupName {
		t.Errorf("group name = %q, want the default %q", groupName, DefaultGroupName)
	}
}

func TestRegisterZeroesThePasswordSlice(t *testing.T) {
	svc := newTestService(t)
	password := []byte("correct horse battery staple")

	if _, err := svc.Register(t.Context(), RegisterRequest{Username: "bob", Password: password}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for i, b := range password {
		if b != 0 {
			t.Fatalf("password[%d] = %d, want 0; Register did not zero its input", i, b)
		}
	}
}

func TestRegisterRejectsInvalidUsername(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
	}{
		{"empty", ""},
		{"only whitespace", "   "},
		{"too long", strings.Repeat("a", maxUsernameLen+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			_, err := svc.Register(t.Context(), RegisterRequest{
				Username: tc.username,
				Password: []byte("correct horse battery staple"),
			})
			if !errors.Is(err, ErrUsernameInvalid) {
				t.Fatalf("Register(username=%q) error = %v, want ErrUsernameInvalid", tc.username, err)
			}
		})
	}
}

func TestRegisterAcceptsUsernameBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
	}{
		{"minimum length", "a"},
		{"maximum length", strings.Repeat("a", maxUsernameLen)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			if _, err := svc.Register(t.Context(), RegisterRequest{
				Username: tc.username,
				Password: []byte("correct horse battery staple"),
			}); err != nil {
				t.Fatalf("Register(username=%q): %v", tc.username, err)
			}
		})
	}
}

func TestRegisterRejectsInvalidPassword(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password []byte
	}{
		{"empty", []byte("")},
		{"one under minimum", []byte(strings.Repeat("a", minPasswordLen-1))},
		{"too long", []byte(strings.Repeat("a", maxPasswordLen+1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			_, err := svc.Register(t.Context(), RegisterRequest{Username: "alice", Password: tc.password})
			if !errors.Is(err, ErrPasswordInvalid) {
				t.Fatalf("Register(password=%d bytes) error = %v, want ErrPasswordInvalid", len(tc.password), err)
			}
		})
	}
}

func TestRegisterRejectsSecondRegistration(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Register(t.Context(), RegisterRequest{
		Username: "alice", Password: []byte("correct horse battery staple"),
	}); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, err := svc.Register(t.Context(), RegisterRequest{
		Username: "bob", Password: []byte("another good password"),
	})
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("second Register error = %v, want ErrRegistrationClosed", err)
	}
	if !errors.Is(err, storage.ErrRegistrationClosed) {
		t.Fatalf("second Register error = %v, want it to also match storage.ErrRegistrationClosed", err)
	}
}

func TestRegisterMintsDistinctUUIDv7Ids(t *testing.T) {
	svcA, svcB := newTestService(t), newTestService(t)

	a, err := svcA.Register(t.Context(), RegisterRequest{Username: "alice", Password: []byte("correct horse battery staple")})
	if err != nil {
		t.Fatalf("Register (A): %v", err)
	}
	b, err := svcB.Register(t.Context(), RegisterRequest{Username: "alice", Password: []byte("correct horse battery staple")})
	if err != nil {
		t.Fatalf("Register (B): %v", err)
	}

	for _, id := range []string{a.GroupID, a.UserID, b.GroupID, b.UserID} {
		if len(id) != 36 || id[14] != '7' {
			t.Errorf("id %q does not look like a UUIDv7 (want 36 chars, version nibble 7 at index 14)", id)
		}
	}
	if a.GroupID == b.GroupID {
		t.Error("two independent registrations minted the same group id")
	}
	if a.UserID == b.UserID {
		t.Error("two independent registrations minted the same user id")
	}
}
