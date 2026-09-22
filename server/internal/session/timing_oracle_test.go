package session

import (
	"context"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestUnknownUsernameCostsTheSameAsAWrongPassword(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the production argon2id profile: 12 MiB and ~28 ms per attempt")
	}

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
	hash, err := hasher.Hash(t.Context(), []byte(testPassword))
	if err != nil {
		t.Fatalf("Hash: %v", err)
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

	median := func(username, password string) time.Duration {
		const samples = 7
		took := make([]time.Duration, 0, samples)
		for range samples {
			start := time.Now()
			_, _ = svc.Login(context.Background(), LoginRequest{
				Username: username,
				Password: []byte(password),
			})
			took = append(took, time.Since(start))
		}
		sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
		return took[len(took)/2]
	}

	wrongPassword := median("alice", "definitely-not-the-password")
	unknownUsername := median("mallory", "definitely-not-the-password")

	ratio := float64(unknownUsername) / float64(wrongPassword)
	t.Logf("wrong-password=%v unknown-username=%v ratio=%.3f",
		wrongPassword, unknownUsername, ratio)

	const floor = 0.25
	if ratio < floor {
		t.Errorf("username-enumeration oracle: an unknown username answered in %v against a wrong "+
			"password's %v (ratio %.3f, floor %.2f) -- the unknown-username path is no longer "+
			"paying for a full argon2id derivation, so an attacker can tell registered usernames "+
			"from unregistered ones by response time alone",
			unknownUsername, wrongPassword, ratio, floor)
	}
}
