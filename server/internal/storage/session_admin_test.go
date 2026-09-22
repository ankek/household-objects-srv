package storage

import (
	"errors"
	"testing"
)

func seedSecondUser(t *testing.T, s *Storage, p FirstUserParams, suffix string) (userID string) {
	t.Helper()
	userID = "usr-" + suffix
	if _, err := s.store.Writer().ExecContext(t.Context(),
		`INSERT INTO users (id, group_id, username, password_hash, role, created_at, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, ?, 'member', ?, ?, 1, NULL, 0)`,
		userID, p.GroupID, "user-"+suffix, "$argon2id$fake$"+suffix, p.Now, p.Now,
	); err != nil {
		t.Fatalf("seed second user: %v", err)
	}
	return userID
}

func TestListSessionsReturnsOnlyTheCallersOwnSessions(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	memberID := seedSecondUser(t, s, owner, "b")

	ownerSession := sessionParamsFor(owner, "owner-1")
	if err := s.CreateSession(t.Context(), ownerSession); err != nil {
		t.Fatalf("CreateSession(owner): %v", err)
	}
	memberSession := SessionParams{
		ID: "ses-member-1", GroupID: owner.GroupID, UserID: memberID,
		TokenHash: "hash-member-1", ExpiresAt: 1_700_000_100_000, Now: 1_700_000_000_000,
	}
	if err := s.CreateSession(t.Context(), memberSession); err != nil {
		t.Fatalf("CreateSession(member): %v", err)
	}

	got, err := s.ListSessions(t.Context(), owner.GroupID, owner.UserID)
	if err != nil {
		t.Fatalf("ListSessions(owner): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListSessions(owner) returned %d sessions, want 1 (the member's session in the same group must not leak in): %+v", len(got), got)
	}
	if got[0].ID != ownerSession.ID {
		t.Errorf("ListSessions(owner)[0].ID = %q, want %q", got[0].ID, ownerSession.ID)
	}

	got, err = s.ListSessions(t.Context(), owner.GroupID, memberID)
	if err != nil {
		t.Fatalf("ListSessions(member): %v", err)
	}
	if len(got) != 1 || got[0].ID != memberSession.ID {
		t.Fatalf("ListSessions(member) = %+v, want exactly [%q]", got, memberSession.ID)
	}
}

func TestListSessionsExcludesOtherGroups(t *testing.T) {
	s := newTestStorage(t)
	a := firstUserParams("a")
	b := firstUserParams("b")
	if err := s.RegisterFirstUser(t.Context(), a); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	if err := s.RegisterNewGroup(t.Context(), b); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}
	if err := s.CreateSession(t.Context(), sessionParamsFor(a, "a1")); err != nil {
		t.Fatalf("CreateSession(a): %v", err)
	}

	got, err := s.ListSessions(t.Context(), b.GroupID, b.UserID)
	if err != nil {
		t.Fatalf("ListSessions(b): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListSessions(b) = %+v, want none of group a's sessions", got)
	}
}

func TestListSessionsReportsRevokedState(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	sp := sessionParamsFor(p, "1")
	if err := s.CreateSession(t.Context(), sp); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := s.ListSessions(t.Context(), p.GroupID, p.UserID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListSessions = %+v, want exactly one row", got)
	}
	if got[0].Revoked {
		t.Error("Revoked = true for a freshly created session, want false")
	}
	if got[0].UserAgent != sp.UserAgent {
		t.Errorf("UserAgent = %q, want %q", got[0].UserAgent, sp.UserAgent)
	}
	if got[0].CreatedFromIP != sp.CreatedFromIP {
		t.Errorf("CreatedFromIP = %q, want %q", got[0].CreatedFromIP, sp.CreatedFromIP)
	}

	if err := s.RevokeSession(t.Context(), p.GroupID, p.UserID, sp.ID, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	got, err = s.ListSessions(t.Context(), p.GroupID, p.UserID)
	if err != nil {
		t.Fatalf("ListSessions after revoke: %v", err)
	}
	if !got[0].Revoked {
		t.Error("Revoked = false after RevokeSession, want true")
	}
	if got[0].RevokedAtUnixMilli != 1_700_000_050_000 {
		t.Errorf("RevokedAtUnixMilli = %d, want 1700000050000", got[0].RevokedAtUnixMilli)
	}
}

func TestRevokeSessionRefusesASameGroupDifferentUsersSession(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	memberID := seedSecondUser(t, s, owner, "b")

	ownerSession := sessionParamsFor(owner, "owner-1")
	if err := s.CreateSession(t.Context(), ownerSession); err != nil {
		t.Fatalf("CreateSession(owner): %v", err)
	}

	err := s.RevokeSession(t.Context(), owner.GroupID, memberID, ownerSession.ID, 1_700_000_050_000)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(member revoking owner's session) = %v, want ErrNotFound", err)
	}

	got, err := s.ListSessions(t.Context(), owner.GroupID, owner.UserID)
	if err != nil {
		t.Fatalf("ListSessions(owner): %v", err)
	}
	if len(got) != 1 || got[0].Revoked {
		t.Fatalf("owner's session after a rejected cross-user revoke attempt = %+v, want one unrevoked session", got)
	}
}

func TestRevokeSessionLeavesTheUsersOtherSessionLiveAndUsable(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	kept := sessionParamsFor(p, "kept")
	if err := s.CreateSession(t.Context(), kept); err != nil {
		t.Fatalf("CreateSession(kept): %v", err)
	}
	target := sessionParamsFor(p, "target")
	if err := s.CreateSession(t.Context(), target); err != nil {
		t.Fatalf("CreateSession(target): %v", err)
	}

	if err := s.RevokeSession(t.Context(), p.GroupID, p.UserID, target.ID, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeSession(target): %v", err)
	}

	targetAuth, err := s.SessionAuth(t.Context(), target.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth(target): %v", err)
	}
	if !targetAuth.Revoked {
		t.Fatal("SessionAuth(target).Revoked = false, want true -- the targeted session was never revoked")
	}

	got, err := s.ListSessions(t.Context(), p.GroupID, p.UserID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	var keptEntry *SessionInfo
	for i := range got {
		if got[i].ID == kept.ID {
			keptEntry = &got[i]
		}
	}
	if keptEntry == nil {
		t.Fatalf("ListSessions after revoking the OTHER session = %+v, missing the kept session %q entirely", got, kept.ID)
	}
	if keptEntry.Revoked {
		t.Fatal("kept session's Revoked = true after revoking a DIFFERENT session of the same user -- this is exactly the `id` predicate widening this test exists to catch")
	}

	keptAuth, err := s.SessionAuth(t.Context(), kept.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth(kept) after revoking the other session: %v", err)
	}
	if keptAuth.Revoked {
		t.Fatal("SessionAuth(kept).Revoked = true after revoking a DIFFERENT session, want false -- the surviving credential must still authenticate")
	}
	if keptAuth.GroupID != p.GroupID {
		t.Errorf("SessionAuth(kept).GroupID = %q, want %q", keptAuth.GroupID, p.GroupID)
	}
	if keptAuth.UserID != p.UserID {
		t.Errorf("SessionAuth(kept).UserID = %q, want %q", keptAuth.UserID, p.UserID)
	}
	if keptAuth.Role != ownerRole {
		t.Errorf("SessionAuth(kept).Role = %q, want %q", keptAuth.Role, ownerRole)
	}
	if keptAuth.ExpiresAtUnixMilli != kept.ExpiresAt {
		t.Errorf("SessionAuth(kept).ExpiresAtUnixMilli = %d, want %d (unchanged by an unrelated session's revoke)", keptAuth.ExpiresAtUnixMilli, kept.ExpiresAt)
	}
}

func TestRevokeSessionRejectsAForeignGroupID(t *testing.T) {
	s := newTestStorage(t)
	a := firstUserParams("a")
	b := firstUserParams("b")
	if err := s.RegisterFirstUser(t.Context(), a); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	if err := s.RegisterNewGroup(t.Context(), b); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}
	sp := sessionParamsFor(a, "1")
	if err := s.CreateSession(t.Context(), sp); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.RevokeSession(t.Context(), b.GroupID, b.UserID, sp.ID, 1_700_000_050_000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(foreign group) = %v, want ErrNotFound", err)
	}
}

func TestRevokeSessionRejectsAnUnknownID(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	if err := s.RevokeSession(t.Context(), p.GroupID, p.UserID, "no-such-session", 1_700_000_050_000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeSession(unknown id) = %v, want ErrNotFound", err)
	}
}

func TestRevokeSessionSucceedsForTheCallersOwnSession(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	sp := sessionParamsFor(p, "1")
	if err := s.CreateSession(t.Context(), sp); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.RevokeSession(t.Context(), p.GroupID, p.UserID, sp.ID, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	got, err := s.SessionAuth(t.Context(), sp.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth after revoke: %v", err)
	}
	if !got.Revoked {
		t.Error("SessionAuth.Revoked = false after RevokeSession, want true")
	}
}

func TestRevokeSessionIsIdempotent(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	sp := sessionParamsFor(p, "1")
	if err := s.CreateSession(t.Context(), sp); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.RevokeSession(t.Context(), p.GroupID, p.UserID, sp.ID, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeSession (first): %v", err)
	}

	var revokedAt, updatedAt, version, changeSeqBefore int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT revoked_at, updated_at, version, change_seq FROM sessions WHERE id = ?", sp.ID,
	).Scan(&revokedAt, &updatedAt, &version, &changeSeqBefore); err != nil {
		t.Fatalf("read session row after first revoke: %v", err)
	}

	var counterBefore int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT change_seq_counter FROM groups WHERE id = ?", p.GroupID,
	).Scan(&counterBefore); err != nil {
		t.Fatalf("read change_seq_counter before second revoke: %v", err)
	}

	if err := s.RevokeSession(t.Context(), p.GroupID, p.UserID, sp.ID, 1_700_000_099_000); err != nil {
		t.Fatalf("RevokeSession (second, already revoked): %v", err)
	}

	var revokedAt2, updatedAt2, version2, changeSeq2 int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT revoked_at, updated_at, version, change_seq FROM sessions WHERE id = ?", sp.ID,
	).Scan(&revokedAt2, &updatedAt2, &version2, &changeSeq2); err != nil {
		t.Fatalf("read session row after second revoke: %v", err)
	}
	if revokedAt2 != revokedAt {
		t.Errorf("revoked_at after idempotent re-revoke = %d, want unchanged %d", revokedAt2, revokedAt)
	}
	if updatedAt2 != updatedAt || version2 != version || changeSeq2 != changeSeqBefore {
		t.Errorf("row mutated by an idempotent re-revoke: updated_at %d->%d version %d->%d change_seq %d->%d",
			updatedAt, updatedAt2, version, version2, changeSeqBefore, changeSeq2)
	}

	var counterAfter int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT change_seq_counter FROM groups WHERE id = ?", p.GroupID,
	).Scan(&counterAfter); err != nil {
		t.Fatalf("read change_seq_counter after second revoke: %v", err)
	}
	if counterAfter != counterBefore {
		t.Errorf("change_seq_counter moved on an idempotent re-revoke: %d -> %d", counterBefore, counterAfter)
	}
}

func TestRevokeSessionRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	sp := sessionParamsFor(p, "1")
	if err := s.CreateSession(t.Context(), sp); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	cases := []struct {
		name                       string
		groupID, userID, sessionID string
		now                        int64
	}{
		{"empty groupID", "", p.UserID, sp.ID, 1},
		{"empty userID", p.GroupID, "", sp.ID, 1},
		{"empty sessionID", p.GroupID, p.UserID, "", 1},
		{"zero now", p.GroupID, p.UserID, sp.ID, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.RevokeSession(t.Context(), tc.groupID, tc.userID, tc.sessionID, tc.now); err == nil {
				t.Errorf("RevokeSession(%+v) succeeded, want a validation error", tc)
			}
		})
	}
}

func TestSessionAdminMethodsRefuseUnopenedStorage(t *testing.T) {
	var s *Storage

	if _, err := s.ListSessions(t.Context(), "g", "u"); !errors.Is(err, ErrNoGroup) {
		t.Errorf("ListSessions on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
	if err := s.RevokeSession(t.Context(), "g", "u", "ses-1", 1); !errors.Is(err, ErrNoGroup) {
		t.Errorf("RevokeSession on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
}

func TestRevokeCallingSessionEndsOnlyTheSessionHoldingThatDigest(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	calling := sessionParamsFor(owner, "calling")
	other := sessionParamsFor(owner, "other-tab")
	for _, p := range []SessionParams{calling, other} {
		if err := s.CreateSession(t.Context(), p); err != nil {
			t.Fatalf("CreateSession(%s): %v", p.ID, err)
		}
	}

	if err := s.RevokeCallingSession(t.Context(), owner.GroupID, owner.UserID, calling.TokenHash, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeCallingSession: %v", err)
	}

	gone, err := s.SessionAuth(t.Context(), calling.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth(calling) after logout: %v", err)
	}
	if !gone.Revoked {
		t.Error("the session whose digest was passed is still live after RevokeCallingSession")
	}
	live, err := s.SessionAuth(t.Context(), other.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth(other) after logout: %v", err)
	}
	if live.Revoked {
		t.Error("RevokeCallingSession revoked the user's OTHER session too; a logout must end one credential, not the account's every session")
	}
}

func TestRevokeCallingSessionRejectsAnotherGroupsDigest(t *testing.T) {
	s := newTestStorage(t)
	a := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), a); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	b := firstUserParams("b")
	if err := s.RegisterNewGroup(t.Context(), b); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}
	aSession := sessionParamsFor(a, "a-1")
	if err := s.CreateSession(t.Context(), aSession); err != nil {
		t.Fatalf("CreateSession(a): %v", err)
	}

	if err := s.RevokeCallingSession(t.Context(), b.GroupID, b.UserID, aSession.TokenHash, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeCallingSession(cross-group): %v", err)
	}

	still, err := s.SessionAuth(t.Context(), aSession.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth(a) after B's attempt: %v", err)
	}
	if still.Revoked {
		t.Fatal("household B ended household A's session by presenting its digest; the group_id predicate on RevokeSessionByTokenHash is not enforcing the boundary")
	}
}

func TestRevokeCallingSessionRejectsASameGroupDifferentUsersDigest(t *testing.T) {
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

	if err := s.RevokeCallingSession(t.Context(), owner.GroupID, owner.UserID, memberSession.TokenHash, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeCallingSession(sibling's digest): %v", err)
	}

	still, err := s.SessionAuth(t.Context(), memberSession.TokenHash)
	if err != nil {
		t.Fatalf("SessionAuth(member) after the owner's attempt: %v", err)
	}
	if still.Revoked {
		t.Fatal("one household member ended another's session by presenting its digest; the user_id predicate on RevokeSessionByTokenHash is not enforcing the boundary")
	}
}

func TestRevokeCallingSessionIsIdempotent(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	p := sessionParamsFor(owner, "calling")
	if err := s.CreateSession(t.Context(), p); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.RevokeCallingSession(t.Context(), owner.GroupID, owner.UserID, p.TokenHash, 1_700_000_050_000); err != nil {
		t.Fatalf("first RevokeCallingSession: %v", err)
	}
	first, err := s.ListSessions(t.Context(), owner.GroupID, owner.UserID)
	if err != nil || len(first) != 1 {
		t.Fatalf("ListSessions after first revoke: %v, %+v", err, first)
	}

	if err := s.RevokeCallingSession(t.Context(), owner.GroupID, owner.UserID, p.TokenHash, 1_700_000_090_000); err != nil {
		t.Fatalf("second RevokeCallingSession: %v", err)
	}
	second, err := s.ListSessions(t.Context(), owner.GroupID, owner.UserID)
	if err != nil || len(second) != 1 {
		t.Fatalf("ListSessions after second revoke: %v, %+v", err, second)
	}
	if second[0].RevokedAtUnixMilli != first[0].RevokedAtUnixMilli {
		t.Errorf("a second logout moved revoked_at from %d to %d; the `revoked_at IS NULL` guard is not holding",
			first[0].RevokedAtUnixMilli, second[0].RevokedAtUnixMilli)
	}
}

func TestRevokeCallingSessionRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	for name, call := range map[string]func() error{
		"empty group":  func() error { return s.RevokeCallingSession(t.Context(), "", "usr", "hash", 1) },
		"empty user":   func() error { return s.RevokeCallingSession(t.Context(), "grp", "", "hash", 1) },
		"empty digest": func() error { return s.RevokeCallingSession(t.Context(), "grp", "usr", "", 1) },
		"zero now":     func() error { return s.RevokeCallingSession(t.Context(), "grp", "usr", "hash", 0) },
	} {
		if err := call(); err == nil {
			t.Errorf("RevokeCallingSession with %s returned nil; want an error", name)
		}
	}
}

func TestRevokeCallingSessionRefusesUnopenedStorage(t *testing.T) {
	var zero Storage
	if err := zero.RevokeCallingSession(t.Context(), "grp", "usr", "hash", 1); !errors.Is(err, ErrNoGroup) {
		t.Errorf("RevokeCallingSession on a zero Storage = %v, want an error wrapping ErrNoGroup", err)
	}
}
