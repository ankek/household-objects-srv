package invite

import (
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/roles"
	"testing"
	"time"
)

func TestRedeemWithValidRequestSucceeds(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	got, err := f.Redeem(t.Context(), RedeemRequest{
		Token:    issued.Token,
		Username: "newmember",
		Password: []byte("correct horse battery staple"),
	})
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if got.GroupID != f.groupID {
		t.Errorf("GroupID = %q, want %q (the invite's own group)", got.GroupID, f.groupID)
	}
	if got.Username != "newmember" {
		t.Errorf("Username = %q, want %q", got.Username, "newmember")
	}
	if got.UserID == "" || got.UserID == f.userID {
		t.Errorf("UserID = %q, want a freshly minted id distinct from the inviting owner's", got.UserID)
	}

	var role string
	f.queryRow(t, "SELECT role FROM users WHERE id = ?", []any{got.UserID}, &role)
	if role != roles.Member {
		t.Errorf("role = %q, want %q", role, roles.Member)
	}

	invites, err := f.storage.ListInvites(t.Context(), f.groupID)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(invites) != 1 || !invites[0].Redeemed || invites[0].RedeemedByUserID != got.UserID {
		t.Errorf("ListInvites = %+v, want exactly one redeemed invite naming %q", invites, got.UserID)
	}
}

func TestRedeemIsSingleUse(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := f.Redeem(t.Context(), RedeemRequest{
		Token: issued.Token, Username: "first", Password: []byte("correct horse battery staple"),
	}); err != nil {
		t.Fatalf("first Redeem: %v", err)
	}

	_, err = f.Redeem(t.Context(), RedeemRequest{
		Token: issued.Token, Username: "second", Password: []byte("correct horse battery staple"),
	})
	if !errors.Is(err, ErrNotRedeemable) {
		t.Errorf("second Redeem: error = %v, want ErrNotRedeemable", err)
	}
}

func TestRedeemRejectsUnknownToken(t *testing.T) {
	f := newFixture(t)
	_, err := f.Redeem(t.Context(), RedeemRequest{
		Token: "this-token-was-never-issued", Username: "someone", Password: []byte("correct horse battery staple"),
	})
	if !errors.Is(err, ErrNotRedeemable) {
		t.Errorf("Redeem(unknown token): error = %v, want ErrNotRedeemable", err)
	}
}

func TestRedeemRejectsUsernameCollisionWithoutBurningTheInvite(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	_, err = f.Redeem(t.Context(), RedeemRequest{
		Token: issued.Token, Username: "alice", Password: []byte("correct horse battery staple"),
	})
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("Redeem(colliding username): error = %v, want ErrUsernameTaken", err)
	}

	got, err := f.Redeem(t.Context(), RedeemRequest{
		Token: issued.Token, Username: "alice2", Password: []byte("correct horse battery staple"),
	})
	if err != nil {
		t.Fatalf("retry Redeem: %v", err)
	}
	if got.Username != "alice2" {
		t.Errorf("retry Redeem: Username = %q, want %q", got.Username, "alice2")
	}
}

func TestRedeemRejectsIncompleteRequest(t *testing.T) {
	f := newFixture(t)
	issued, err := f.Issue(t.Context(), IssueRequest{GroupID: f.groupID, CreatedByUserID: f.userID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	validPassword := []byte("correct horse battery staple")

	cases := []struct {
		name string
		req  RedeemRequest
		want error
	}{
		{"empty token", RedeemRequest{Token: "", Username: "u", Password: validPassword}, ErrTokenRequired},
		{"whitespace-only token", RedeemRequest{Token: "   ", Username: "u", Password: validPassword}, ErrTokenRequired},
		{"empty username", RedeemRequest{Token: issued.Token, Username: "", Password: validPassword}, ErrUsernameInvalid},
		{"too-long username", RedeemRequest{Token: issued.Token, Username: string(make([]byte, maxUsernameLen+1)), Password: validPassword}, ErrUsernameInvalid},
		{"too-short password", RedeemRequest{Token: issued.Token, Username: "u", Password: []byte("short")}, ErrPasswordInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pw := append([]byte(nil), tc.req.Password...)
			req := tc.req
			req.Password = pw
			if _, err := f.Redeem(t.Context(), req); !errors.Is(err, tc.want) {
				t.Errorf("Redeem(%s): error = %v, want %v", tc.name, err, tc.want)
			}
		})
	}
}

func TestRedeemDoesNotHashPasswordForAnUnredeemableToken(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the production argon2id profile: 12 MiB and ~tens of ms per hash, repeated")
	}

	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}

	const samples = 7

	baseline := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		start := time.Now()
		if _, err := hasher.Hash(t.Context(), []byte("correct horse battery staple")); err != nil {
			t.Fatalf("baseline sample %d: Hash: %v", i, err)
		}
		baseline = append(baseline, time.Since(start))
	}

	svc := &Service{storage: newFixture(t).storage, hasher: hasher, ttl: DefaultTTL}
	measured := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		start := time.Now()
		_, err := svc.Redeem(t.Context(), RedeemRequest{
			Token:    "this-token-was-never-issued",
			Username: "someone",
			Password: []byte("correct horse battery staple"),
		})
		measured = append(measured, time.Since(start))
		if !errors.Is(err, ErrNotRedeemable) {
			t.Fatalf("sample %d: Redeem: error = %v, want ErrNotRedeemable", i, err)
		}
	}

	baselineMedian := medianDuration(baseline)
	measuredMedian := medianDuration(measured)
	if measuredMedian >= baselineMedian/2 {
		t.Errorf("median cost of Redeem(unredeemable token) = %v, want well under half of one hash's cost (%v); a password hash appears to be running before the invite is known to be redeemable", measuredMedian, baselineMedian)
	}
}

func medianDuration(d []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), d...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	return sorted[len(sorted)/2]
}
