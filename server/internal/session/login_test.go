package session

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
	"time"
)

var cheapHasherConfig = auth.Config{
	Params: auth.Params{MemoryKiB: 64, Time: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16},
}

const testPassword = "correct horse battery staple"

type testFixture struct {
	*Service
	dbPath   string
	hasher   *auth.Hasher
	groupID  string
	userID   string
	username string
}

func (f testFixture) row(t *testing.T, query string, args ...any) *sql.Row {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+f.dbPath)
	if err != nil {
		t.Fatalf("open independent connection to %s: %v", f.dbPath, err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close independent connection: %v", err)
		}
	})
	return conn.QueryRowContext(t.Context(), query, args...)
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

	hasher, err := auth.NewHasher(cheapHasherConfig)
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}

	hash, err := hasher.Hash(t.Context(), []byte(testPassword))
	if err != nil {
		t.Fatalf("Hash seed password: %v", err)
	}

	groupID, userID := mustUUID(t), mustUUID(t)
	if err := store.RegisterFirstUser(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    "Household",
		UserID:       userID,
		Username:     "alice",
		PasswordHash: hash,
		Now:          time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	svc, err := NewService(store, hasher)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return testFixture{Service: svc, dbPath: path, hasher: hasher, groupID: groupID, userID: userID, username: "alice"}
}

func mustUUID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return id.String()
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	hasher, err := auth.NewHasher(cheapHasherConfig)
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

func TestLoginWithCorrectCredentialsSucceeds(t *testing.T) {
	f := newFixture(t)
	before := time.Now()

	got, err := f.Login(t.Context(), LoginRequest{
		Username:   "alice",
		Password:   []byte(testPassword),
		UserAgent:  "test-agent/1.0",
		RemoteAddr: "203.0.113.5",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if got.GroupID != f.groupID {
		t.Errorf("GroupID = %q, want %q", got.GroupID, f.groupID)
	}
	if got.UserID != f.userID {
		t.Errorf("UserID = %q, want %q", got.UserID, f.userID)
	}
	if got.Username != "alice" {
		t.Errorf("Username = %q, want %q", got.Username, "alice")
	}
	if got.Role != "owner" {
		t.Errorf("Role = %q, want %q", got.Role, "owner")
	}
	if got.Token == "" {
		t.Fatal("Token is empty")
	}

	wantExpiry := before.Add(DefaultSessionTTL)
	if diff := got.ExpiresAt.Sub(wantExpiry); diff < -time.Minute || diff > time.Minute {
		t.Errorf("ExpiresAt = %v, want approximately %v (DefaultSessionTTL out from login time)", got.ExpiresAt, wantExpiry)
	}

	sum := sha256.Sum256([]byte(got.Token))
	wantHash := hex.EncodeToString(sum[:])

	var storedHash, storedUserAgent, storedIP string
	if err := f.row(t, "SELECT token_hash, user_agent, created_from_ip FROM sessions WHERE user_id = ?", f.userID).
		Scan(&storedHash, &storedUserAgent, &storedIP); err != nil {
		t.Fatalf("read created session: %v", err)
	}
	if storedHash != wantHash {
		t.Errorf("stored token_hash = %q, want SHA-256(Token) = %q", storedHash, wantHash)
	}
	if storedHash == got.Token {
		t.Fatal("stored token_hash equals the raw token; the token was never hashed")
	}
	if storedUserAgent != "test-agent/1.0" {
		t.Errorf("stored user_agent = %q, want %q", storedUserAgent, "test-agent/1.0")
	}
	if storedIP != "203.0.113.5" {
		t.Errorf("stored created_from_ip = %q, want %q", storedIP, "203.0.113.5")
	}
}

func TestLoginZeroesThePasswordSlice(t *testing.T) {
	f := newFixture(t)
	password := []byte(testPassword)

	if _, err := f.Login(t.Context(), LoginRequest{Username: "alice", Password: password}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	for i, b := range password {
		if b != 0 {
			t.Fatalf("password[%d] = %d, want 0; Login did not zero its input", i, b)
		}
	}
}

func TestLoginRefusesWrongPasswordAndUnknownUsernameIdentically(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
		password string
	}{
		{"wrong password", "alice", "not the password"},
		{"unknown username", "does-not-exist", testPassword},
		{"unknown username and wrong password", "does-not-exist", "not the password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.Login(t.Context(), LoginRequest{Username: tc.username, Password: []byte(tc.password)})
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login(%q, %q) error = %v, want ErrInvalidCredentials", tc.username, tc.password, err)
			}
		})
	}
}

func TestLoginRefusesWrongPasswordCreatesNoSession(t *testing.T) {
	f := newFixture(t)
	if _, err := f.Login(t.Context(), LoginRequest{Username: "alice", Password: []byte("wrong")}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login: error = %v, want ErrInvalidCredentials", err)
	}

	var n int
	if err := f.row(t, "SELECT count(*) FROM sessions").Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("sessions row count after a failed login = %d, want 0", n)
	}
}

func TestLoginIsCaseInsensitiveOnUsername(t *testing.T) {
	f := newFixture(t)
	if _, err := f.Login(t.Context(), LoginRequest{Username: "ALICE", Password: []byte(testPassword)}); err != nil {
		t.Fatalf("Login(upper-case username): %v", err)
	}
}

func TestLoginTrimsUsernameWhitespace(t *testing.T) {
	f := newFixture(t)
	if _, err := f.Login(t.Context(), LoginRequest{Username: "  alice  ", Password: []byte(testPassword)}); err != nil {
		t.Fatalf("Login(padded username): %v", err)
	}
}

func TestLoginRehashesOnSupersededParams(t *testing.T) {
	f := newFixture(t)

	oldParams := auth.Params{MemoryKiB: 32, Time: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	oldHasher, err := auth.NewHasher(auth.Config{Params: oldParams})
	if err != nil {
		t.Fatalf("auth.NewHasher(old): %v", err)
	}
	oldHash, err := oldHasher.Hash(t.Context(), []byte(testPassword))
	if err != nil {
		t.Fatalf("Hash under old params: %v", err)
	}
	f.exec(t, "UPDATE users SET password_hash = ? WHERE id = ?", oldHash, f.userID)

	if _, err := f.Login(t.Context(), LoginRequest{Username: "alice", Password: []byte(testPassword)}); err != nil {
		t.Fatalf("Login: %v", err)
	}

	var storedHash string
	if err := f.row(t, "SELECT password_hash FROM users WHERE id = ?", f.userID).Scan(&storedHash); err != nil {
		t.Fatalf("read rehashed user: %v", err)
	}
	if storedHash == oldHash {
		t.Fatal("password_hash was not rewritten after a login under superseded parameters")
	}
	if f.hasher.NeedsRehash(storedHash) {
		t.Error("the rewritten hash still NeedsRehash against the fixture's own hasher; it was not rehashed under the current profile")
	}

	if err := f.hasher.Verify(t.Context(), storedHash, []byte(testPassword)); err != nil {
		t.Errorf("Verify(rehashed): %v, want nil", err)
	}
}
