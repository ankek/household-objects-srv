package storage

import (
	"errors"
	"testing"
)

func TestListDeviceTokensReturnsOnlyTheCallersOwnDevices(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	memberID := seedSecondUser(t, s, owner, "b")

	ownerDevice := deviceTokenParamsFor(owner, "owner-1")
	if err := s.CreateDeviceToken(t.Context(), ownerDevice); err != nil {
		t.Fatalf("CreateDeviceToken(owner): %v", err)
	}
	memberDevice := DeviceTokenParams{
		ID: "dvc-member-1", GroupID: owner.GroupID, UserID: memberID,
		TokenHash: "hash-member-1", DeviceLabel: "Member's Phone", Now: 1_700_000_000_000,
	}
	if err := s.CreateDeviceToken(t.Context(), memberDevice); err != nil {
		t.Fatalf("CreateDeviceToken(member): %v", err)
	}

	got, err := s.ListDeviceTokens(t.Context(), owner.GroupID, owner.UserID)
	if err != nil {
		t.Fatalf("ListDeviceTokens(owner): %v", err)
	}
	if len(got) != 1 || got[0].ID != ownerDevice.ID {
		t.Fatalf("ListDeviceTokens(owner) = %+v, want exactly [%q] (the member's device in the same group must not leak in)", got, ownerDevice.ID)
	}

	got, err = s.ListDeviceTokens(t.Context(), owner.GroupID, memberID)
	if err != nil {
		t.Fatalf("ListDeviceTokens(member): %v", err)
	}
	if len(got) != 1 || got[0].ID != memberDevice.ID {
		t.Fatalf("ListDeviceTokens(member) = %+v, want exactly [%q]", got, memberDevice.ID)
	}
}

func TestListDeviceTokensExcludesOtherGroups(t *testing.T) {
	s := newTestStorage(t)
	a := firstUserParams("a")
	b := firstUserParams("b")
	if err := s.RegisterFirstUser(t.Context(), a); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	if err := s.RegisterNewGroup(t.Context(), b); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}
	if err := s.CreateDeviceToken(t.Context(), deviceTokenParamsFor(a, "a1")); err != nil {
		t.Fatalf("CreateDeviceToken(a): %v", err)
	}

	got, err := s.ListDeviceTokens(t.Context(), b.GroupID, b.UserID)
	if err != nil {
		t.Fatalf("ListDeviceTokens(b): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListDeviceTokens(b) = %+v, want none of group a's devices", got)
	}
}

func TestListDeviceTokensReportsRevokedState(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	dp := deviceTokenParamsFor(p, "1")
	if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}

	got, err := s.ListDeviceTokens(t.Context(), p.GroupID, p.UserID)
	if err != nil {
		t.Fatalf("ListDeviceTokens: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListDeviceTokens = %+v, want exactly one row", got)
	}
	if got[0].Revoked {
		t.Error("Revoked = true for a freshly issued device token, want false")
	}
	if got[0].DeviceLabel != dp.DeviceLabel {
		t.Errorf("DeviceLabel = %q, want %q", got[0].DeviceLabel, dp.DeviceLabel)
	}

	if err := s.RevokeDeviceToken(t.Context(), p.GroupID, p.UserID, dp.ID, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeDeviceToken: %v", err)
	}
	got, err = s.ListDeviceTokens(t.Context(), p.GroupID, p.UserID)
	if err != nil {
		t.Fatalf("ListDeviceTokens after revoke: %v", err)
	}
	if !got[0].Revoked {
		t.Error("Revoked = false after RevokeDeviceToken, want true")
	}
	if got[0].RevokedAtUnixMilli != 1_700_000_050_000 {
		t.Errorf("RevokedAtUnixMilli = %d, want 1700000050000", got[0].RevokedAtUnixMilli)
	}
}

func TestRevokeDeviceTokenRefusesASameGroupDifferentUsersToken(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	memberID := seedSecondUser(t, s, owner, "b")

	ownerDevice := deviceTokenParamsFor(owner, "owner-1")
	if err := s.CreateDeviceToken(t.Context(), ownerDevice); err != nil {
		t.Fatalf("CreateDeviceToken(owner): %v", err)
	}

	err := s.RevokeDeviceToken(t.Context(), owner.GroupID, memberID, ownerDevice.ID, 1_700_000_050_000)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeDeviceToken(member revoking owner's device) = %v, want ErrNotFound", err)
	}

	got, err := s.ListDeviceTokens(t.Context(), owner.GroupID, owner.UserID)
	if err != nil {
		t.Fatalf("ListDeviceTokens(owner): %v", err)
	}
	if len(got) != 1 || got[0].Revoked {
		t.Fatalf("owner's device token after a rejected cross-user revoke attempt = %+v, want one unrevoked device", got)
	}
}

func TestRevokeDeviceTokenLeavesTheUsersOtherDeviceTokenLiveAndUsable(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	kept := deviceTokenParamsFor(p, "kept")
	if err := s.CreateDeviceToken(t.Context(), kept); err != nil {
		t.Fatalf("CreateDeviceToken(kept): %v", err)
	}
	target := deviceTokenParamsFor(p, "target")
	if err := s.CreateDeviceToken(t.Context(), target); err != nil {
		t.Fatalf("CreateDeviceToken(target): %v", err)
	}

	if err := s.RevokeDeviceToken(t.Context(), p.GroupID, p.UserID, target.ID, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeDeviceToken(target): %v", err)
	}

	targetAuth, err := s.DeviceTokenAuth(t.Context(), target.TokenHash)
	if err != nil {
		t.Fatalf("DeviceTokenAuth(target): %v", err)
	}
	if !targetAuth.Revoked {
		t.Fatal("DeviceTokenAuth(target).Revoked = false, want true -- the targeted device token was never revoked")
	}

	got, err := s.ListDeviceTokens(t.Context(), p.GroupID, p.UserID)
	if err != nil {
		t.Fatalf("ListDeviceTokens: %v", err)
	}
	var keptEntry *DeviceTokenInfo
	for i := range got {
		if got[i].ID == kept.ID {
			keptEntry = &got[i]
		}
	}
	if keptEntry == nil {
		t.Fatalf("ListDeviceTokens after revoking the OTHER device token = %+v, missing the kept device token %q entirely", got, kept.ID)
	}
	if keptEntry.Revoked {
		t.Fatal("kept device token's Revoked = true after revoking a DIFFERENT device token of the same user -- this is exactly the `id` predicate widening this test exists to catch")
	}

	keptAuth, err := s.DeviceTokenAuth(t.Context(), kept.TokenHash)
	if err != nil {
		t.Fatalf("DeviceTokenAuth(kept) after revoking the other device token: %v", err)
	}
	if keptAuth.Revoked {
		t.Fatal("DeviceTokenAuth(kept).Revoked = true after revoking a DIFFERENT device token, want false -- the surviving credential must still authenticate")
	}
	if keptAuth.GroupID != p.GroupID {
		t.Errorf("DeviceTokenAuth(kept).GroupID = %q, want %q", keptAuth.GroupID, p.GroupID)
	}
	if keptAuth.UserID != p.UserID {
		t.Errorf("DeviceTokenAuth(kept).UserID = %q, want %q", keptAuth.UserID, p.UserID)
	}
	if keptAuth.Role != ownerRole {
		t.Errorf("DeviceTokenAuth(kept).Role = %q, want %q", keptAuth.Role, ownerRole)
	}
}

func TestRevokeDeviceTokenRejectsAForeignGroupID(t *testing.T) {
	s := newTestStorage(t)
	a := firstUserParams("a")
	b := firstUserParams("b")
	if err := s.RegisterFirstUser(t.Context(), a); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	if err := s.RegisterNewGroup(t.Context(), b); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}
	dp := deviceTokenParamsFor(a, "1")
	if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}

	if err := s.RevokeDeviceToken(t.Context(), b.GroupID, b.UserID, dp.ID, 1_700_000_050_000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeDeviceToken(foreign group) = %v, want ErrNotFound", err)
	}
}

func TestRevokeDeviceTokenRejectsAnUnknownID(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	if err := s.RevokeDeviceToken(t.Context(), p.GroupID, p.UserID, "no-such-device", 1_700_000_050_000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeDeviceToken(unknown id) = %v, want ErrNotFound", err)
	}
}

func TestRevokeDeviceTokenSucceedsForTheCallersOwnToken(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	dp := deviceTokenParamsFor(p, "1")
	if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}

	if err := s.RevokeDeviceToken(t.Context(), p.GroupID, p.UserID, dp.ID, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeDeviceToken: %v", err)
	}

	got, err := s.DeviceTokenAuth(t.Context(), dp.TokenHash)
	if err != nil {
		t.Fatalf("DeviceTokenAuth after revoke: %v", err)
	}
	if !got.Revoked {
		t.Error("DeviceTokenAuth.Revoked = false after RevokeDeviceToken, want true")
	}
}

func TestRevokeDeviceTokenIsIdempotent(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	dp := deviceTokenParamsFor(p, "1")
	if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}

	if err := s.RevokeDeviceToken(t.Context(), p.GroupID, p.UserID, dp.ID, 1_700_000_050_000); err != nil {
		t.Fatalf("RevokeDeviceToken (first): %v", err)
	}

	var revokedAt, updatedAt, version, changeSeqBefore int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT revoked_at, updated_at, version, change_seq FROM device_tokens WHERE id = ?", dp.ID,
	).Scan(&revokedAt, &updatedAt, &version, &changeSeqBefore); err != nil {
		t.Fatalf("read device token row after first revoke: %v", err)
	}

	if err := s.RevokeDeviceToken(t.Context(), p.GroupID, p.UserID, dp.ID, 1_700_000_099_000); err != nil {
		t.Fatalf("RevokeDeviceToken (second, already revoked): %v", err)
	}

	var revokedAt2, updatedAt2, version2, changeSeq2 int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT revoked_at, updated_at, version, change_seq FROM device_tokens WHERE id = ?", dp.ID,
	).Scan(&revokedAt2, &updatedAt2, &version2, &changeSeq2); err != nil {
		t.Fatalf("read device token row after second revoke: %v", err)
	}
	if revokedAt2 != revokedAt {
		t.Errorf("revoked_at after idempotent re-revoke = %d, want unchanged %d", revokedAt2, revokedAt)
	}
	if updatedAt2 != updatedAt || version2 != version || changeSeq2 != changeSeqBefore {
		t.Errorf("row mutated by an idempotent re-revoke: updated_at %d->%d version %d->%d change_seq %d->%d",
			updatedAt, updatedAt2, version, version2, changeSeqBefore, changeSeq2)
	}
}

func TestRevokeDeviceTokenRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	dp := deviceTokenParamsFor(p, "1")
	if err := s.CreateDeviceToken(t.Context(), dp); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}

	cases := []struct {
		name                      string
		groupID, userID, deviceID string
		now                       int64
	}{
		{"empty groupID", "", p.UserID, dp.ID, 1},
		{"empty userID", p.GroupID, "", dp.ID, 1},
		{"empty deviceID", p.GroupID, p.UserID, "", 1},
		{"zero now", p.GroupID, p.UserID, dp.ID, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.RevokeDeviceToken(t.Context(), tc.groupID, tc.userID, tc.deviceID, tc.now); err == nil {
				t.Errorf("RevokeDeviceToken(%+v) succeeded, want a validation error", tc)
			}
		})
	}
}

func TestDeviceTokenAdminMethodsRefuseUnopenedStorage(t *testing.T) {
	var s *Storage

	if _, err := s.ListDeviceTokens(t.Context(), "g", "u"); !errors.Is(err, ErrNoGroup) {
		t.Errorf("ListDeviceTokens on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
	if err := s.RevokeDeviceToken(t.Context(), "g", "u", "dvc-1", 1); !errors.Is(err, ErrNoGroup) {
		t.Errorf("RevokeDeviceToken on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
}
