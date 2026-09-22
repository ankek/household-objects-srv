package storage

import (
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/roles"
	"strconv"
	"sync"
	"testing"
)

func redeemInviteParamsFor(tokenHash, idSuffix string, now int64) RedeemInviteParams {
	return RedeemInviteParams{
		TokenHash:    tokenHash,
		UserID:       "redeemed-usr-" + idSuffix,
		Username:     "redeemed-user-" + idSuffix,
		PasswordHash: "$argon2id$fake-redeemed$" + idSuffix,
		Now:          now,
	}
}

func TestRedeemInviteSucceeds(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	rp := redeemInviteParamsFor(ip.TokenHash, "1", 1_700_050_000_000)
	got, err := s.RedeemInvite(t.Context(), rp)
	if err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	if got.GroupID != owner.GroupID {
		t.Errorf("GroupID = %q, want the invite's own group %q", got.GroupID, owner.GroupID)
	}
	if got.UserID != rp.UserID || got.Username != rp.Username {
		t.Errorf("RedeemedInvite = %+v, want UserID=%q Username=%q", got, rp.UserID, rp.Username)
	}

	var role string
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT role FROM users WHERE id = ?", rp.UserID,
	).Scan(&role); err != nil {
		t.Fatalf("read created user's role: %v", err)
	}
	if role != roles.Member {
		t.Errorf("created user's role = %q, want %q (never owner -- FR-007/A4)", role, roles.Member)
	}

	invites, err := s.ListInvites(t.Context(), owner.GroupID)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(invites) != 1 {
		t.Fatalf("ListInvites = %+v, want exactly one row", invites)
	}
	if !invites[0].Redeemed {
		t.Error("Redeemed = false after RedeemInvite succeeded, want true")
	}
	if invites[0].RedeemedByUserID != rp.UserID {
		t.Errorf("RedeemedByUserID = %q, want %q", invites[0].RedeemedByUserID, rp.UserID)
	}
}

func TestRedeemInviteJoinsTheInvitesOwnGroup(t *testing.T) {
	s := newTestStorage(t)
	ownerA := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), ownerA); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	ownerB := firstUserParams("b")
	if err := s.RegisterNewGroup(t.Context(), ownerB); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}

	inviteA := inviteParamsFor(ownerA, "a1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), inviteA); err != nil {
		t.Fatalf("CreateInvite(a): %v", err)
	}
	inviteB := inviteParamsFor(ownerB, "b1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), inviteB); err != nil {
		t.Fatalf("CreateInvite(b): %v", err)
	}

	got, err := s.RedeemInvite(t.Context(), redeemInviteParamsFor(inviteA.TokenHash, "a", 1_700_050_000_000))
	if err != nil {
		t.Fatalf("RedeemInvite(a): %v", err)
	}
	if got.GroupID != ownerA.GroupID {
		t.Errorf("redeeming group A's invite landed in group %q, want %q", got.GroupID, ownerA.GroupID)
	}
	if got.GroupID == ownerB.GroupID {
		t.Fatal("redeeming group A's invite landed in group B")
	}
}

func TestRedeemInviteIsSingleUse(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	if _, err := s.RedeemInvite(t.Context(), redeemInviteParamsFor(ip.TokenHash, "first", 1_700_050_000_000)); err != nil {
		t.Fatalf("first RedeemInvite: %v", err)
	}

	_, err := s.RedeemInvite(t.Context(), redeemInviteParamsFor(ip.TokenHash, "second", 1_700_060_000_000))
	if !errors.Is(err, ErrInviteNotRedeemable) {
		t.Fatalf("second RedeemInvite: error = %v, want ErrInviteNotRedeemable", err)
	}

	if n := countRows(t, s, "users"); n != 2 {
		t.Errorf("users table has %d rows, want 2 (owner + exactly one redemption)", n)
	}
}

func TestRedeemInviteConcurrentRedemptionsExactlyOneWins(t *testing.T) {
	const racers = 32

	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		wins     int
		notFound int
		other    []error
	)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rp := redeemInviteParamsFor(ip.TokenHash, "racer-"+strconv.Itoa(i), 1_700_050_000_000)
			_, err := s.RedeemInvite(t.Context(), rp)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrInviteNotRedeemable):
				notFound++
			default:
				other = append(other, err)
			}
		}(i)
	}
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("%d goroutine(s) returned an unexpected error, e.g. %v", len(other), other[0])
	}
	if wins != 1 {
		t.Errorf("wins = %d, want exactly 1 (single-use invite redeemed by %d racers)", wins, racers)
	}
	if notFound != racers-1 {
		t.Errorf("ErrInviteNotRedeemable count = %d, want %d", notFound, racers-1)
	}

	if n := countRows(t, s, "users"); n != 2 {
		t.Errorf("users table has %d rows after the race, want 2 (owner + exactly one redemption)", n)
	}
}

func TestRedeemInviteRejectsUnknownToken(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	_, err := s.RedeemInvite(t.Context(), redeemInviteParamsFor("no-such-hash", "1", 1_700_000_000_000))
	if !errors.Is(err, ErrInviteNotRedeemable) {
		t.Errorf("RedeemInvite(unknown token): error = %v, want ErrInviteNotRedeemable", err)
	}
}

func TestRedeemInviteRejectsExpiredToken(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	const expiresAt = 1_700_100_000_000
	ip := inviteParamsFor(owner, "1", expiresAt)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	cases := []struct {
		name string
		now  int64
	}{
		{"now equals expiry", expiresAt},
		{"now after expiry", expiresAt + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.RedeemInvite(t.Context(), redeemInviteParamsFor(ip.TokenHash, tc.name, tc.now))
			if !errors.Is(err, ErrInviteNotRedeemable) {
				t.Errorf("RedeemInvite(%s): error = %v, want ErrInviteNotRedeemable", tc.name, err)
			}
		})
	}
}

func TestRedeemInviteRejectsRevokedToken(t *testing.T) {
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
		t.Fatalf("RevokeInvite: %v", err)
	}

	_, err := s.RedeemInvite(t.Context(), redeemInviteParamsFor(ip.TokenHash, "1", 1_700_050_100_000))
	if !errors.Is(err, ErrInviteNotRedeemable) {
		t.Errorf("RedeemInvite(revoked): error = %v, want ErrInviteNotRedeemable", err)
	}
}

func TestRedeemInviteUsernameCollisionDoesNotBurnTheInvite(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	elsewhere := firstUserParams("elsewhere")
	elsewhere.Username = "taken"
	if err := s.RegisterNewGroup(t.Context(), elsewhere); err != nil {
		t.Fatalf("RegisterNewGroup(elsewhere): %v", err)
	}

	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	collide := redeemInviteParamsFor(ip.TokenHash, "collide", 1_700_050_000_000)
	collide.Username = "taken"
	_, err := s.RedeemInvite(t.Context(), collide)
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("RedeemInvite(colliding username): error = %v, want ErrUsernameTaken", err)
	}

	retry := redeemInviteParamsFor(ip.TokenHash, "retry", 1_700_050_100_000)
	got, err := s.RedeemInvite(t.Context(), retry)
	if err != nil {
		t.Fatalf("retry RedeemInvite: %v", err)
	}
	if got.Username != retry.Username {
		t.Errorf("retry succeeded but Username = %q, want %q", got.Username, retry.Username)
	}

	var n int
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM users WHERE id = ?", collide.UserID,
	).Scan(&n); err != nil {
		t.Fatalf("count collide.UserID rows: %v", err)
	}
	if n != 0 {
		t.Errorf("the failed collision attempt left %d row(s) for its UserID behind, want 0", n)
	}
}

func TestRedeemInviteAllocatesExactlyOneChangeSeq(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	ip := inviteParamsFor(owner, "1", 1_700_100_000_000)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	before := groupChangeSeqCounter(t, s, owner.GroupID)

	rp := redeemInviteParamsFor(ip.TokenHash, "1", 1_700_050_000_000)
	if _, err := s.RedeemInvite(t.Context(), rp); err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}

	after := groupChangeSeqCounter(t, s, owner.GroupID)
	if after != before+1 {
		t.Errorf("change_seq_counter moved from %d to %d, want exactly +1 (one AllocChangeSeq call for both writes)", before, after)
	}

	var userChangeSeq, inviteChangeSeq int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT change_seq FROM users WHERE id = ?", rp.UserID,
	).Scan(&userChangeSeq); err != nil {
		t.Fatalf("read user change_seq: %v", err)
	}
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT change_seq FROM invites WHERE id = ?", ip.ID,
	).Scan(&inviteChangeSeq); err != nil {
		t.Fatalf("read invite change_seq: %v", err)
	}
	if userChangeSeq != inviteChangeSeq {
		t.Errorf("user.change_seq = %d, invite.change_seq = %d, want equal (one shared seq)", userChangeSeq, inviteChangeSeq)
	}
	if userChangeSeq != after {
		t.Errorf("stamped change_seq = %d, want the allocated counter value %d", userChangeSeq, after)
	}
}

func TestRedeemInviteRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	valid := RedeemInviteParams{
		TokenHash:    "hash-1",
		UserID:       "usr-1",
		Username:     "user-1",
		PasswordHash: "$argon2id$fake$1",
		Now:          1_700_000_000_000,
	}

	cases := []struct {
		name   string
		mutate func(RedeemInviteParams) RedeemInviteParams
	}{
		{"empty TokenHash", func(p RedeemInviteParams) RedeemInviteParams { p.TokenHash = ""; return p }},
		{"empty UserID", func(p RedeemInviteParams) RedeemInviteParams { p.UserID = ""; return p }},
		{"empty Username", func(p RedeemInviteParams) RedeemInviteParams { p.Username = ""; return p }},
		{"empty PasswordHash", func(p RedeemInviteParams) RedeemInviteParams { p.PasswordHash = ""; return p }},
		{"zero Now", func(p RedeemInviteParams) RedeemInviteParams { p.Now = 0; return p }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.RedeemInvite(t.Context(), tc.mutate(valid)); err == nil {
				t.Errorf("RedeemInvite(%s) succeeded, want a validation error", tc.name)
			}
		})
	}
}

func TestRedeemInviteRefusesUnopenedStorage(t *testing.T) {
	var s *Storage
	if _, err := s.RedeemInvite(t.Context(), RedeemInviteParams{}); !errors.Is(err, ErrNoGroup) {
		t.Errorf("RedeemInvite on a nil *Storage: error = %v, want ErrNoGroup", err)
	}
}

func TestInviteRedeemableTracksTheA64Predicate(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	const expiresAt = 1_700_100_000_000
	ip := inviteParamsFor(owner, "1", expiresAt)
	if err := s.CreateInvite(t.Context(), ip); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	if ok, err := s.InviteRedeemable(t.Context(), "no-such-hash", 1_700_000_000_000); err != nil || ok {
		t.Errorf("InviteRedeemable(unknown) = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := s.InviteRedeemable(t.Context(), ip.TokenHash, 1_700_000_000_000); err != nil || !ok {
		t.Errorf("InviteRedeemable(live) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := s.InviteRedeemable(t.Context(), ip.TokenHash, expiresAt); err != nil || ok {
		t.Errorf("InviteRedeemable(now == expiresAt) = (%v, %v), want (false, nil)", ok, err)
	}

	if _, err := s.RedeemInvite(t.Context(), redeemInviteParamsFor(ip.TokenHash, "1", 1_700_050_000_000)); err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	if ok, err := s.InviteRedeemable(t.Context(), ip.TokenHash, 1_700_050_000_001); err != nil || ok {
		t.Errorf("InviteRedeemable(already redeemed) = (%v, %v), want (false, nil)", ok, err)
	}
}
