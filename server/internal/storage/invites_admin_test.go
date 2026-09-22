package storage

import (
	"database/sql"
	"errors"
	"testing"
)

func redeemInviteDirectly(t *testing.T, s *Storage, inviteID, redeemedByUserID string, redeemedAt int64) {
	t.Helper()
	if _, err := s.store.Writer().ExecContext(t.Context(),
		"UPDATE invites SET redeemed_at = ?, redeemed_by_user_id = ? WHERE id = ?",
		redeemedAt, redeemedByUserID, inviteID,
	); err != nil {
		t.Fatalf("redeem invite directly: %v", err)
	}
}

func readInviteRow(t *testing.T, s *Storage, inviteID string) (expiresAt, updatedAt, version, changeSeq int64) {
	t.Helper()
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT expires_at, updated_at, version, change_seq FROM invites WHERE id = ?", inviteID,
	).Scan(&expiresAt, &updatedAt, &version, &changeSeq); err != nil {
		t.Fatalf("read invite row: %v", err)
	}
	return
}

func groupChangeSeqCounter(t *testing.T, s *Storage, groupID string) int64 {
	t.Helper()
	var counter int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT change_seq_counter FROM groups WHERE id = ?", groupID,
	).Scan(&counter); err != nil {
		t.Fatalf("read change_seq_counter: %v", err)
	}
	return counter
}

func TestListInvitesIsGroupWideNotPerCreator(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	secondUserID := seedSecondUser(t, s, owner, "b")

	byOwner := inviteParamsFor(owner, "owner-1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), byOwner); err != nil {
		t.Fatalf("CreateInvite(by owner): %v", err)
	}
	bySecond := InviteParams{
		ID: "inv-second-1", GroupID: owner.GroupID, CreatedByUserID: secondUserID,
		TokenHash: "hash-second-1", ExpiresAt: 1_700_100_000_000, Now: 1_700_000_000_000,
	}
	if err := s.CreateInvite(t.Context(), bySecond); err != nil {
		t.Fatalf("CreateInvite(by second user): %v", err)
	}

	got, err := s.ListInvites(t.Context(), owner.GroupID)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListInvites = %+v, want both invites regardless of which household member created them", got)
	}
}

func TestListInvitesExcludesOtherGroups(t *testing.T) {
	s := newTestStorage(t)
	a := firstUserParams("a")
	b := firstUserParams("b")
	if err := s.RegisterFirstUser(t.Context(), a); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	if err := s.RegisterNewGroup(t.Context(), b); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}
	if err := s.CreateInvite(t.Context(), inviteParamsFor(a, "a1", 1_700_100_000_000)); err != nil {
		t.Fatalf("CreateInvite(a): %v", err)
	}

	got, err := s.ListInvites(t.Context(), b.GroupID)
	if err != nil {
		t.Fatalf("ListInvites(b): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListInvites(b) = %+v, want none of group a's invites", got)
	}
}

func TestListInvitesReportsRedeemedState(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	memberID := seedSecondUser(t, s, owner, "b")
	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	redeemInviteDirectly(t, s, ip.ID, memberID, 1_700_050_000_000)

	got, err := s.ListInvites(t.Context(), owner.GroupID)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListInvites = %+v, want exactly one row", got)
	}
	if !got[0].Redeemed {
		t.Error("Redeemed = false after direct redemption, want true")
	}
	if got[0].RedeemedAtUnixMilli != 1_700_050_000_000 {
		t.Errorf("RedeemedAtUnixMilli = %d, want 1700050000000", got[0].RedeemedAtUnixMilli)
	}
	if got[0].RedeemedByUserID != memberID {
		t.Errorf("RedeemedByUserID = %q, want %q", got[0].RedeemedByUserID, memberID)
	}
}

func TestRevokeInviteSucceedsForAnOpenInvite(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	_, _, baselineVersion, baselineChangeSeq := readInviteRow(t, s, ip.ID)

	revokeAt := int64(1_700_050_000_000)
	if err := s.RevokeInvite(t.Context(), owner.GroupID, ip.ID, revokeAt); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}

	expiresAt, updatedAt, version, changeSeq := readInviteRow(t, s, ip.ID)
	if expiresAt > revokeAt {
		t.Fatalf("expires_at after RevokeInvite = %d, want it moved to at most %d (the revoke instant), not left at the original future expiry", expiresAt, revokeAt)
	}
	if expiresAt != revokeAt {
		t.Errorf("expires_at after RevokeInvite = %d, want exactly %d", expiresAt, revokeAt)
	}
	if updatedAt != revokeAt {
		t.Errorf("updated_at after RevokeInvite = %d, want %d", updatedAt, revokeAt)
	}
	if version != baselineVersion+1 {
		t.Errorf("version after one RevokeInvite = %d, want %d (baseline + one write)", version, baselineVersion+1)
	}
	if changeSeq != baselineChangeSeq+1 {
		t.Errorf("change_seq after RevokeInvite = %d, want %d (baseline + one AllocChangeSeq)", changeSeq, baselineChangeSeq+1)
	}
}

func TestRevokeInviteTreatsExpiresAtEqualNowAsAlreadyExpired(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	expiresAt := int64(1_700_100_000_000)
	ip := inviteParamsFor(owner, "1", expiresAt)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	_, baselineUpdatedAt, baselineVersion, baselineChangeSeq := readInviteRow(t, s, ip.ID)
	baselineCounter := groupChangeSeqCounter(t, s, owner.GroupID)

	if err := s.RevokeInvite(t.Context(), owner.GroupID, ip.ID, expiresAt); err != nil {
		t.Fatalf("RevokeInvite(now == expires_at): %v", err)
	}

	gotExpiresAt, updatedAt, version, changeSeq := readInviteRow(t, s, ip.ID)
	if gotExpiresAt != expiresAt {
		t.Errorf("expires_at after revoking exactly at expires_at = %d, want unchanged %d", gotExpiresAt, expiresAt)
	}
	if updatedAt != baselineUpdatedAt || version != baselineVersion || changeSeq != baselineChangeSeq {
		t.Errorf("row mutated by a revoke at the already-expired boundary: updated_at = %d (want %d), version = %d (want %d), change_seq = %d (want %d)",
			updatedAt, baselineUpdatedAt, version, baselineVersion, changeSeq, baselineChangeSeq)
	}
	if got := groupChangeSeqCounter(t, s, owner.GroupID); got != baselineCounter {
		t.Errorf("groups.change_seq_counter after a no-op revoke at the boundary = %d, want unchanged %d (no AllocChangeSeq should run for an already-unredeemable invite)", got, baselineCounter)
	}

	if err := s.RevokeInvite(t.Context(), owner.GroupID, ip.ID, expiresAt-1); err != nil {
		t.Fatalf("RevokeInvite(now == expires_at - 1): %v", err)
	}
	gotExpiresAt, _, version, _ = readInviteRow(t, s, ip.ID)
	if gotExpiresAt != expiresAt-1 {
		t.Errorf("expires_at after revoking one ms before expiry = %d, want %d (the revoke must have written)", gotExpiresAt, expiresAt-1)
	}
	if version != 2 {
		t.Errorf("version after the second (effective) revoke = %d, want 2", version)
	}
}

func TestRevokeInviteIsANoOpOnAnAlreadyRedeemedInvite(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	memberID := seedSecondUser(t, s, owner, "b")
	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	redeemInviteDirectly(t, s, ip.ID, memberID, 1_700_010_000_000)

	before := [4]int64{}
	before[0], before[1], before[2], before[3] = readInviteRow(t, s, ip.ID)

	if err := s.RevokeInvite(t.Context(), owner.GroupID, ip.ID, 1_700_050_000_000); err != nil {
		t.Fatalf("RevokeInvite(already redeemed): %v", err)
	}

	after := [4]int64{}
	after[0], after[1], after[2], after[3] = readInviteRow(t, s, ip.ID)
	if before != after {
		t.Errorf("row mutated by revoking an already-redeemed invite: before = %+v, after = %+v", before, after)
	}
}

func TestRevokeInviteIsIdempotent(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	if err := s.RevokeInvite(t.Context(), owner.GroupID, ip.ID, 1_700_050_000_000); err != nil {
		t.Fatalf("RevokeInvite (first): %v", err)
	}
	expiresAt, updatedAt, version, changeSeq := readInviteRow(t, s, ip.ID)

	if err := s.RevokeInvite(t.Context(), owner.GroupID, ip.ID, 1_700_099_000_000); err != nil {
		t.Fatalf("RevokeInvite (second, already revoked): %v", err)
	}
	expiresAt2, updatedAt2, version2, changeSeq2 := readInviteRow(t, s, ip.ID)

	if expiresAt2 != expiresAt {
		t.Errorf("expires_at after idempotent re-revoke = %d, want unchanged %d", expiresAt2, expiresAt)
	}
	if updatedAt2 != updatedAt || version2 != version || changeSeq2 != changeSeq {
		t.Errorf("row mutated by an idempotent re-revoke: updated_at %d->%d version %d->%d change_seq %d->%d",
			updatedAt, updatedAt2, version, version2, changeSeq, changeSeq2)
	}
}

func TestRevokeInviteRejectsAForeignGroupID(t *testing.T) {
	s := newTestStorage(t)
	a := firstUserParams("a")
	b := firstUserParams("b")
	if err := s.RegisterFirstUser(t.Context(), a); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	if err := s.RegisterNewGroup(t.Context(), b); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}
	ip := inviteParamsFor(a, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	if err := s.RevokeInvite(t.Context(), b.GroupID, ip.ID, 1_700_050_000_000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeInvite(foreign group) = %v, want ErrNotFound", err)
	}

	expiresAt, _, version, _ := readInviteRow(t, s, ip.ID)
	if expiresAt != ip.ExpiresAt || version != 1 {
		t.Fatalf("group a's invite after a rejected cross-group revoke = expires_at %d version %d, want untouched (%d, 1)", expiresAt, version, ip.ExpiresAt)
	}
}

func TestRevokeInviteRejectsAnUnknownID(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	if err := s.RevokeInvite(t.Context(), p.GroupID, "no-such-invite", 1_700_050_000_000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeInvite(unknown id) = %v, want ErrNotFound", err)
	}
}

func TestRevokeInviteRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	ip := inviteParamsFor(p, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	cases := []struct {
		name              string
		groupID, inviteID string
		now               int64
	}{
		{"empty groupID", "", ip.ID, 1},
		{"empty inviteID", p.GroupID, "", 1},
		{"zero now", p.GroupID, ip.ID, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.RevokeInvite(t.Context(), tc.groupID, tc.inviteID, tc.now); err == nil {
				t.Errorf("RevokeInvite(%+v) succeeded, want a validation error", tc)
			}
		})
	}
}

func TestInviteAdminMethodsRefuseUnopenedStorage(t *testing.T) {
	var s *Storage

	if _, err := s.ListInvites(t.Context(), "g"); !errors.Is(err, ErrNoGroup) {
		t.Errorf("ListInvites on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
	if err := s.RevokeInvite(t.Context(), "g", "inv-1", 1); !errors.Is(err, ErrNoGroup) {
		t.Errorf("RevokeInvite on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
}

func TestListInvitesExcludesSoftDeletedRows(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	live := inviteParamsFor(owner, "inv-live", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), live); err != nil {
		t.Fatalf("CreateInvite(live): %v", err)
	}
	gone := inviteParamsFor(owner, "inv-soft-deleted", 1_700_100_000_000)
	gone.TokenHash = "hash-soft-deleted"
	if err := s.CreateInvite(t.Context(), gone); err != nil {
		t.Fatalf("CreateInvite(to be soft-deleted): %v", err)
	}

	if err := s.store.Tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			`UPDATE invites SET deleted_at = ? WHERE id = ?`, 1_700_050_000_000, gone.ID)
		return err
	}); err != nil {
		t.Fatalf("soft-delete the invite: %v", err)
	}

	got, err := s.ListInvites(t.Context(), owner.GroupID)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	for _, inv := range got {
		if inv.ID == gone.ID {
			t.Fatalf("ListInvites returned the soft-deleted invite %q; a row with deleted_at set must not appear, matching ListSessionsForUser and ListDeviceTokensForUser", gone.ID)
		}
	}
	if len(got) != 1 || got[0].ID != live.ID {
		t.Fatalf("ListInvites = %+v, want exactly the one live invite %q", got, live.ID)
	}
}
