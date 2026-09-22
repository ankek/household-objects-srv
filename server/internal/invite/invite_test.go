package invite

import (
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
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

type testFixture struct {
	*Service
	storage *storage.Storage
	dbPath  string
	groupID string
	userID  string
}

func (f testFixture) queryRow(t *testing.T, query string, args []any, dest ...any) {
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
	if err := conn.QueryRowContext(t.Context(), query, args...).Scan(dest...); err != nil {
		t.Fatalf("query %q: %v", query, err)
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

	hasher, err := auth.NewHasher(cheapHasherConfig)
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}

	svc, err := NewService(store, hasher)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return testFixture{Service: svc, storage: store, dbPath: path, groupID: groupID, userID: userID}
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
	hasher, err := auth.NewHasher(cheapHasherConfig)
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}
	if _, err := NewService(nil, hasher); err == nil {
		t.Error("NewService accepted a nil *storage.Storage")
	}
}

func TestNewServiceRejectsNilHasher(t *testing.T) {
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
	if _, err := NewService(store, nil); err == nil {
		t.Error("NewService accepted a nil *auth.Hasher")
	}
}

func TestIssueWithValidRequestSucceeds(t *testing.T) {
	f := newFixture(t)

	before := time.Now()
	got, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, CreatedByUserID: f.userID})
	after := time.Now()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if got.ID == "" {
		t.Error("ID is empty")
	}
	if got.Token == "" {
		t.Fatal("Token is empty")
	}

	wantExpiryFloor, wantExpiryCeil := before.Add(DefaultTTL), after.Add(DefaultTTL)
	if got.ExpiresAt.Before(wantExpiryFloor) || got.ExpiresAt.After(wantExpiryCeil) {
		t.Errorf("ExpiresAt = %v, want between %v and %v (DefaultTTL out from issuance)", got.ExpiresAt, wantExpiryFloor, wantExpiryCeil)
	}

	invites, err := f.storage.ListInvites(t.Context(), f.groupID)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(invites) != 1 || invites[0].ID != got.ID {
		t.Fatalf("ListInvites = %+v, want exactly [%q]", invites, got.ID)
	}
	if invites[0].CreatedByUserID != f.userID {
		t.Errorf("CreatedByUserID = %q, want %q", invites[0].CreatedByUserID, f.userID)
	}
	if invites[0].Redeemed {
		t.Error("Redeemed = true for a freshly issued invite, want false")
	}
	if invites[0].ExpiresAtUnixMilli != got.ExpiresAt.UnixMilli() {
		t.Errorf("stored ExpiresAtUnixMilli = %d, want %d (Issued.ExpiresAt)", invites[0].ExpiresAtUnixMilli, got.ExpiresAt.UnixMilli())
	}
}

func TestIssueStoresOnlyTheHashNeverTheToken(t *testing.T) {
	f := newFixture(t)
	got, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	var storedHash string
	f.queryRow(t, "SELECT token_hash FROM invites WHERE id = ?", []any{got.ID}, &storedHash)
	if storedHash == got.Token {
		t.Fatal("stored token_hash equals the raw token verbatim: the bearer credential is not hashed at rest")
	}
	if storedHash != bearertoken.Hash(got.Token) {
		t.Errorf("stored token_hash = %q, want %q (bearertoken.Hash(token))", storedHash, bearertoken.Hash(got.Token))
	}
}

func TestIssueRejectsIncompleteRequest(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name string
		req  IssueRequest
		want error
	}{
		{"empty GroupID", IssueRequest{CreatedByUserID: f.userID}, ErrGroupIDRequired},
		{"empty CreatedByUserID", IssueRequest{GroupID: f.groupID}, ErrCreatedByUserIDRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.Issue(t.Context(), tc.req); err != tc.want {
				t.Errorf("Issue(%+v) = %v, want %v", tc.req, err, tc.want)
			}
		})
	}
}

func TestIssueProducesDistinctTokensAndIDs(t *testing.T) {
	f := newFixture(t)
	a, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("Issue (a): %v", err)
	}
	b, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("Issue (b): %v", err)
	}
	if a.ID == b.ID {
		t.Errorf("two issuances produced the same ID %q", a.ID)
	}
	if a.Token == b.Token {
		t.Error("two issuances produced the same token")
	}
}
