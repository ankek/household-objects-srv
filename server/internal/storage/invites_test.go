package storage

import (
	"errors"
	"testing"
)

func inviteParamsFor(p FirstUserParams, idSuffix string, expiresAt int64) InviteParams {
	return InviteParams{
		ID:              "inv-" + idSuffix,
		GroupID:         p.GroupID,
		CreatedByUserID: p.UserID,
		TokenHash:       "hash-" + idSuffix,
		ExpiresAt:       expiresAt,
		Now:             1_700_000_000_000,
	}
}

func TestCreateInviteRoundTrips(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	ip := inviteParamsFor(p, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	got, err := s.ListInvites(t.Context(), p.GroupID)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListInvites = %+v, want exactly one row", got)
	}
	inv := got[0]
	if inv.ID != ip.ID {
		t.Errorf("ID = %q, want %q", inv.ID, ip.ID)
	}
	if inv.CreatedByUserID != ip.CreatedByUserID {
		t.Errorf("CreatedByUserID = %q, want %q", inv.CreatedByUserID, ip.CreatedByUserID)
	}
	if inv.ExpiresAtUnixMilli != ip.ExpiresAt {
		t.Errorf("ExpiresAtUnixMilli = %d, want %d", inv.ExpiresAtUnixMilli, ip.ExpiresAt)
	}
	if inv.CreatedAtUnixMilli != ip.Now || inv.UpdatedAtUnixMilli != ip.Now {
		t.Errorf("CreatedAtUnixMilli/UpdatedAtUnixMilli = %d/%d, want both %d", inv.CreatedAtUnixMilli, inv.UpdatedAtUnixMilli, ip.Now)
	}
	if inv.Redeemed {
		t.Error("Redeemed = true for a freshly issued invite, want false")
	}

	var changeSeqCounter int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT change_seq_counter FROM groups WHERE id = ?", p.GroupID,
	).Scan(&changeSeqCounter); err != nil {
		t.Fatalf("read change_seq_counter: %v", err)
	}
	if changeSeqCounter != 2 {
		t.Errorf("groups.change_seq_counter = %d, want 2 (1 from RegisterFirstUser, 1 from CreateInvite)", changeSeqCounter)
	}
}

func TestCreateInviteStoresOnlyTheHash(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	ip := inviteParamsFor(p, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	var tokenHash string
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT token_hash FROM invites WHERE id = ?", ip.ID,
	).Scan(&tokenHash); err != nil {
		t.Fatalf("read token_hash: %v", err)
	}
	if tokenHash != ip.TokenHash {
		t.Errorf("stored token_hash = %q, want %q", tokenHash, ip.TokenHash)
	}
}

func TestCreateInviteRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	valid := inviteParamsFor(p, "1", 1_700_100_000_000)

	cases := []struct {
		name   string
		mutate func(InviteParams) InviteParams
	}{
		{"empty ID", func(ip InviteParams) InviteParams { ip.ID = ""; return ip }},
		{"empty GroupID", func(ip InviteParams) InviteParams { ip.GroupID = ""; return ip }},
		{"empty CreatedByUserID", func(ip InviteParams) InviteParams { ip.CreatedByUserID = ""; return ip }},
		{"empty TokenHash", func(ip InviteParams) InviteParams { ip.TokenHash = ""; return ip }},
		{"zero ExpiresAt", func(ip InviteParams) InviteParams { ip.ExpiresAt = 0; return ip }},
		{"zero Now", func(ip InviteParams) InviteParams { ip.Now = 0; return ip }},
		{"ExpiresAt equal to Now", func(ip InviteParams) InviteParams { ip.ExpiresAt = ip.Now; return ip }},
		{"ExpiresAt before Now", func(ip InviteParams) InviteParams { ip.ExpiresAt = ip.Now - 1; return ip }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.CreateInvite(t.Context(), tc.mutate(valid)); err == nil {
				t.Errorf("CreateInvite(%s) succeeded, want a validation error", tc.name)
			}
		})
	}
}

func TestCreateInviteRefusesUnopenedStorage(t *testing.T) {
	var s *Storage
	if err := s.CreateInvite(t.Context(), InviteParams{}); !errors.Is(err, ErrNoGroup) {
		t.Errorf("CreateInvite on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
}
