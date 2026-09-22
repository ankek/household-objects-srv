package storage

import (
	"errors"
	"testing"
)

type userRow struct {
	passwordHash string
	updatedAt    int64
	version      int64
	changeSeq    int64
	deletedAt    *int64
}

func readUser(t *testing.T, s *Storage, groupID, userID string) userRow {
	t.Helper()
	var r userRow
	err := s.store.Writer().QueryRowContext(t.Context(),
		`SELECT password_hash, updated_at, version, change_seq, deleted_at FROM users WHERE group_id = ? AND id = ?`,
		groupID, userID,
	).Scan(&r.passwordHash, &r.updatedAt, &r.version, &r.changeSeq, &r.deletedAt)
	if err != nil {
		t.Fatalf("read user %s/%s: %v", groupID, userID, err)
	}
	return r
}

func TestResetPasswordUpdatesHashAndBumpsPrimitives(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	before := readUser(t, s, owner.GroupID, owner.UserID)

	const newHash = "$argon2id$fake$reset-1"
	const now = 1_700_000_500_000
	result, err := s.ResetPassword(t.Context(), owner.Username, newHash, now)
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if result.UserID != owner.UserID || result.GroupID != owner.GroupID {
		t.Errorf("ResetPassword result = %+v, want UserID=%q GroupID=%q", result, owner.UserID, owner.GroupID)
	}

	after := readUser(t, s, owner.GroupID, owner.UserID)
	if after.passwordHash != newHash {
		t.Errorf("password_hash = %q, want %q", after.passwordHash, newHash)
	}
	if after.updatedAt != now {
		t.Errorf("updated_at = %d, want %d", after.updatedAt, now)
	}
	if after.version != before.version+1 {
		t.Errorf("version = %d, want %d (before + 1)", after.version, before.version+1)
	}
	if after.changeSeq <= before.changeSeq {
		t.Errorf("change_seq = %d, want strictly greater than the pre-reset value %d", after.changeSeq, before.changeSeq)
	}
}

func TestResetPasswordRevokesLiveSessionsAndDeviceTokens(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	session := sessionParamsFor(owner, "s1")
	if err := s.CreateSession(t.Context(), session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	device := deviceTokenParamsFor(owner, "d1")
	if err := s.CreateDeviceToken(t.Context(), device); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}

	result, err := s.ResetPassword(t.Context(), owner.Username, "$argon2id$fake$reset-2", 1_700_000_600_000)
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if result.SessionsRevoked != 1 {
		t.Errorf("SessionsRevoked = %d, want 1", result.SessionsRevoked)
	}
	if result.DeviceTokensRevoked != 1 {
		t.Errorf("DeviceTokensRevoked = %d, want 1", result.DeviceTokensRevoked)
	}

	sessions, err := s.ListSessions(t.Context(), owner.GroupID, owner.UserID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Revoked {
		t.Fatalf("ListSessions after reset = %+v, want exactly one session with Revoked=true", sessions)
	}

	devices, err := s.ListDeviceTokens(t.Context(), owner.GroupID, owner.UserID)
	if err != nil {
		t.Fatalf("ListDeviceTokens: %v", err)
	}
	if len(devices) != 1 || !devices[0].Revoked {
		t.Fatalf("ListDeviceTokens after reset = %+v, want exactly one device token with Revoked=true", devices)
	}
}

func TestResetPasswordDoesNotRevokeOtherMembersCredentials(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	memberID := seedSecondUser(t, s, owner, "b")
	memberSession := SessionParams{
		ID: "ses-member-1", GroupID: owner.GroupID, UserID: memberID,
		TokenHash: "hash-member-1", ExpiresAt: 1_700_000_100_000, Now: 1_700_000_000_000,
	}
	if err := s.CreateSession(t.Context(), memberSession); err != nil {
		t.Fatalf("CreateSession(member): %v", err)
	}
	memberDevice := DeviceTokenParams{
		ID: "dvc-member-1", GroupID: owner.GroupID, UserID: memberID,
		TokenHash: "hash-member-1", DeviceLabel: "Member's Phone", Now: 1_700_000_000_000,
	}
	if err := s.CreateDeviceToken(t.Context(), memberDevice); err != nil {
		t.Fatalf("CreateDeviceToken(member): %v", err)
	}

	if _, err := s.ResetPassword(t.Context(), owner.Username, "$argon2id$fake$reset-3", 1_700_000_700_000); err != nil {
		t.Fatalf("ResetPassword(owner): %v", err)
	}

	sessions, err := s.ListSessions(t.Context(), owner.GroupID, memberID)
	if err != nil {
		t.Fatalf("ListSessions(member): %v", err)
	}
	if len(sessions) != 1 || sessions[0].Revoked {
		t.Fatalf("ListSessions(member) after resetting the OWNER's password = %+v, want the member's own session untouched", sessions)
	}

	devices, err := s.ListDeviceTokens(t.Context(), owner.GroupID, memberID)
	if err != nil {
		t.Fatalf("ListDeviceTokens(member): %v", err)
	}
	if len(devices) != 1 || devices[0].Revoked {
		t.Fatalf("ListDeviceTokens(member) after resetting the OWNER's password = %+v, want the member's own device token untouched", devices)
	}
}

func TestResetPasswordFailsLoudlyForUnknownUsername(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	_, err := s.ResetPassword(t.Context(), "does-not-exist", "$argon2id$fake$reset-4", 1_700_000_800_000)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("ResetPassword(unknown username) error = %v, want ErrUserNotFound", err)
	}

	after := readUser(t, s, owner.GroupID, owner.UserID)
	if after.passwordHash == "$argon2id$fake$reset-4" {
		t.Error("ResetPassword(unknown username) changed an unrelated account's password hash")
	}
}

func TestResetPasswordRefusesASoftDeletedAccount(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	if _, err := s.store.Writer().ExecContext(t.Context(),
		`UPDATE users SET deleted_at = ? WHERE group_id = ? AND id = ?`,
		1_700_000_050_000, owner.GroupID, owner.UserID,
	); err != nil {
		t.Fatalf("soft-delete the account: %v", err)
	}

	_, err := s.ResetPassword(t.Context(), owner.Username, "$argon2id$fake$reset-5", 1_700_000_900_000)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("ResetPassword(soft-deleted username) error = %v, want ErrUserNotFound", err)
	}
}

func TestResetPasswordMatchesUsernameCaseInsensitively(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	result, err := s.ResetPassword(t.Context(), "USER-A", "$argon2id$fake$reset-6", 1_700_001_000_000)
	if err != nil {
		t.Fatalf("ResetPassword(uppercased username): %v", err)
	}
	if result.UserID != owner.UserID {
		t.Errorf("ResetPassword(\"USER-A\") resolved to user %q, want %q", result.UserID, owner.UserID)
	}
}

func TestUsersUsernameIndexMakesCrossGroupAmbiguityImpossible(t *testing.T) {
	s := newTestStorage(t)
	a := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), a); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	b := firstUserParams("b")
	if err := s.RegisterNewGroup(t.Context(), b); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}

	_, err := s.store.Writer().ExecContext(t.Context(),
		`INSERT INTO users (id, group_id, username, password_hash, role, created_at, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, ?, 'member', ?, ?, 1, NULL, 0)`,
		"usr-collide", b.GroupID, a.Username, "$argon2id$fake$collide", b.Now, b.Now,
	)
	if err == nil {
		t.Fatal("inserting a second live user with a username already used in another group succeeded; ux_users_username no longer prevents cross-household username collisions, which is the invariant ResetPassword's username-only lookup depends on")
	}
}

func TestResetPasswordSecondCallReportsNothingLeftToRevoke(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	if err := s.CreateSession(t.Context(), sessionParamsFor(owner, "s1")); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := s.ResetPassword(t.Context(), owner.Username, "$argon2id$fake$reset-7", 1_700_001_100_000); err != nil {
		t.Fatalf("ResetPassword (first): %v", err)
	}
	second, err := s.ResetPassword(t.Context(), owner.Username, "$argon2id$fake$reset-8", 1_700_001_200_000)
	if err != nil {
		t.Fatalf("ResetPassword (second): %v", err)
	}
	if second.SessionsRevoked != 0 {
		t.Errorf("second reset's SessionsRevoked = %d, want 0 (the one session was already revoked by the first reset)", second.SessionsRevoked)
	}
}
