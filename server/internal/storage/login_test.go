package storage

import (
	"errors"
	"testing"
)

func TestUserForLoginFindsTheRow(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	for _, tc := range []struct{ name, username string }{
		{"exact case", p.Username},
		{"upper case", "USER-A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.UserForLogin(t.Context(), tc.username)
			if err != nil {
				t.Fatalf("UserForLogin(%q): %v", tc.username, err)
			}
			if got.ID != p.UserID {
				t.Errorf("ID = %q, want %q", got.ID, p.UserID)
			}
			if got.GroupID != p.GroupID {
				t.Errorf("GroupID = %q, want %q", got.GroupID, p.GroupID)
			}
			if got.PasswordHash != p.PasswordHash {
				t.Errorf("PasswordHash = %q, want %q", got.PasswordHash, p.PasswordHash)
			}
			if got.Role != ownerRole {
				t.Errorf("Role = %q, want %q", got.Role, ownerRole)
			}
		})
	}
}

func TestUserForLoginReportsUnknownUsername(t *testing.T) {
	s := newTestStorage(t)
	if _, err := s.UserForLogin(t.Context(), "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("UserForLogin(unknown): error = %v, want ErrUserNotFound", err)
	}
}

func TestUserForLoginSkipsSoftDeletedUsers(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	if _, err := s.store.Writer().ExecContext(t.Context(),
		"UPDATE users SET deleted_at = 1 WHERE id = ?", p.UserID,
	); err != nil {
		t.Fatalf("soft-delete user: %v", err)
	}

	if _, err := s.UserForLogin(t.Context(), p.Username); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("UserForLogin(soft-deleted): error = %v, want ErrUserNotFound", err)
	}
}

func sessionParamsFor(p FirstUserParams, idSuffix string) SessionParams {
	return SessionParams{
		ID:            "ses-" + idSuffix,
		GroupID:       p.GroupID,
		UserID:        p.UserID,
		TokenHash:     "hash-" + idSuffix,
		ExpiresAt:     1_700_000_100_000,
		UserAgent:     "test-agent/1.0",
		CreatedFromIP: "203.0.113.5",
		Now:           1_700_000_000_000,
	}
}

func TestCreateSessionThenSessionAuthRoundTrips(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	sp := sessionParamsFor(p, "1")
	if err := s.CreateSession(t.Context(), sp); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := s.SessionAuth(t.Context(), sp.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth: %v", err)
	}
	if got.GroupID != p.GroupID {
		t.Errorf("GroupID = %q, want %q", got.GroupID, p.GroupID)
	}
	if got.UserID != p.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, p.UserID)
	}
	if got.Role != ownerRole {
		t.Errorf("Role = %q, want %q", got.Role, ownerRole)
	}
	if got.ExpiresAtUnixMilli != sp.ExpiresAt {
		t.Errorf("ExpiresAtUnixMilli = %d, want %d", got.ExpiresAtUnixMilli, sp.ExpiresAt)
	}
	if got.Revoked {
		t.Error("Revoked = true for a freshly created session, want false")
	}

	var counter int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT change_seq_counter FROM groups WHERE id = ?", p.GroupID,
	).Scan(&counter); err != nil {
		t.Fatalf("read change_seq_counter: %v", err)
	}
	if counter != 2 {
		t.Errorf("change_seq_counter after CreateSession = %d, want 2 (1 for the owner, 1 for the session)", counter)
	}
}

func TestSessionAuthReportsUnknownToken(t *testing.T) {
	s := newTestStorage(t)
	if _, err := s.SessionAuth(t.Context(), "no-such-hash"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("SessionAuth(unknown): error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionAuthReportsRevokedWithoutRefusing(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	sp := sessionParamsFor(p, "1")
	if err := s.CreateSession(t.Context(), sp); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.store.Writer().ExecContext(t.Context(),
		"UPDATE sessions SET revoked_at = 1 WHERE id = ?", sp.ID,
	); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	got, err := s.SessionAuth(t.Context(), sp.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth(revoked): %v", err)
	}
	if !got.Revoked {
		t.Error("Revoked = false for a session with revoked_at set, want true")
	}
}

func TestSessionAuthSkipsSoftDeletedSessionsAndUsers(t *testing.T) {
	t.Run("session tombstoned", func(t *testing.T) {
		s := newTestStorage(t)
		p := firstUserParams("a")
		if err := s.RegisterFirstUser(t.Context(), p); err != nil {
			t.Fatalf("RegisterFirstUser: %v", err)
		}
		sp := sessionParamsFor(p, "1")
		if err := s.CreateSession(t.Context(), sp); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if _, err := s.store.Writer().ExecContext(t.Context(),
			"UPDATE sessions SET deleted_at = 1 WHERE id = ?", sp.ID,
		); err != nil {
			t.Fatalf("tombstone session: %v", err)
		}
		if _, err := s.SessionAuth(t.Context(), sp.TokenHash); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("SessionAuth(tombstoned session): error = %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("owning user tombstoned", func(t *testing.T) {
		s := newTestStorage(t)
		p := firstUserParams("a")
		if err := s.RegisterFirstUser(t.Context(), p); err != nil {
			t.Fatalf("RegisterFirstUser: %v", err)
		}
		sp := sessionParamsFor(p, "1")
		if err := s.CreateSession(t.Context(), sp); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if _, err := s.store.Writer().ExecContext(t.Context(),
			"UPDATE users SET deleted_at = 1 WHERE id = ?", p.UserID,
		); err != nil {
			t.Fatalf("tombstone user: %v", err)
		}
		if _, err := s.SessionAuth(t.Context(), sp.TokenHash); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("SessionAuth(tombstoned owner): error = %v, want ErrSessionNotFound", err)
		}
	})
}

func TestCreateSessionRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	base := sessionParamsFor(p, "1")

	cases := []struct {
		name   string
		mutate func(*SessionParams)
	}{
		{"empty ID", func(sp *SessionParams) { sp.ID = "" }},
		{"empty GroupID", func(sp *SessionParams) { sp.GroupID = "" }},
		{"empty UserID", func(sp *SessionParams) { sp.UserID = "" }},
		{"empty TokenHash", func(sp *SessionParams) { sp.TokenHash = "" }},
		{"zero ExpiresAt", func(sp *SessionParams) { sp.ExpiresAt = 0 }},
		{"zero Now", func(sp *SessionParams) { sp.Now = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sp := base
			tc.mutate(&sp)
			if err := s.CreateSession(t.Context(), sp); err == nil {
				t.Errorf("CreateSession(%+v) succeeded, want a validation error", sp)
			}
		})
	}

	if got := countRows(t, s, "sessions"); got != 0 {
		t.Fatalf("sessions row count after only-invalid attempts = %d, want 0", got)
	}
}

func TestRehashPasswordUpdatesTheStoredHash(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	const newHash = "$argon2id$new$hash"
	if err := s.RehashPassword(t.Context(), p.GroupID, p.UserID, newHash, 1_700_000_500_000); err != nil {
		t.Fatalf("RehashPassword: %v", err)
	}

	var storedHash string
	var version int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT password_hash, version FROM users WHERE id = ?", p.UserID,
	).Scan(&storedHash, &version); err != nil {
		t.Fatalf("read rehashed user: %v", err)
	}
	if storedHash != newHash {
		t.Errorf("password_hash = %q, want %q", storedHash, newHash)
	}
	if version != 2 {
		t.Errorf("version = %d, want 2 (bumped once by the rehash)", version)
	}
}

func TestRehashPasswordReportsUnknownUser(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	if err := s.RehashPassword(t.Context(), p.GroupID, "no-such-user", "$argon2id$x", 1); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("RehashPassword(unknown user): error = %v, want ErrUserNotFound", err)
	}
}

func TestLoginMethodsRefuseUnopenedStorage(t *testing.T) {
	var s *Storage

	if _, err := s.UserForLogin(t.Context(), "alice"); !errors.Is(err, ErrNoGroup) {
		t.Errorf("UserForLogin on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
	if err := s.CreateSession(t.Context(), SessionParams{}); !errors.Is(err, ErrNoGroup) {
		t.Errorf("CreateSession on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
	if _, err := s.SessionAuth(t.Context(), "h"); !errors.Is(err, ErrNoGroup) {
		t.Errorf("SessionAuth on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
	if err := s.RehashPassword(t.Context(), "g", "u", "h", 1); !errors.Is(err, ErrNoGroup) {
		t.Errorf("RehashPassword on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
}
