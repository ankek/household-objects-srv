package storage

import (
	"testing"
)

func TestMemberListReturnsOwnerAfterRegistration(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	scope, err := s.ForGroup(MustGroupID(owner.GroupID))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	got, err := scope.Members().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List returned %d members, want 1: %+v", len(got), got)
	}
	if got[0].ID != owner.UserID || got[0].Username != owner.Username || got[0].Role != "owner" || got[0].JoinedAtUnixMilli != owner.Now {
		t.Errorf("List()[0] = %+v, want {ID:%q Username:%q Role:owner JoinedAtUnixMilli:%d}",
			got[0], owner.UserID, owner.Username, owner.Now)
	}
}

func TestMemberListOrdersByJoinedAtThenID(t *testing.T) {
	s := newTestStorage(t)
	owner := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), owner); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}
	seedSecondUserAt(t, s, owner, "z", owner.Now+1000)
	seedSecondUserAt(t, s, owner, "m", owner.Now+1000)

	scope, err := s.ForGroup(MustGroupID(owner.GroupID))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	got, err := scope.Members().List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d members, want 3: %+v", len(got), got)
	}
	wantIDs := []string{owner.UserID, "usr-m", "usr-z"}
	for i, w := range wantIDs {
		if got[i].ID != w {
			t.Errorf("List()[%d].ID = %q, want %q (owner first, then the same-instant pair broken by id ASC): %+v", i, got[i].ID, w, got)
		}
	}
}

func TestMemberListIsScopedToItsOwnGroup(t *testing.T) {
	s := newTestStorage(t)
	ownerA := firstUserParams("a")
	if err := s.RegisterFirstUser(t.Context(), ownerA); err != nil {
		t.Fatalf("RegisterFirstUser(a): %v", err)
	}
	ownerB := firstUserParams("b")
	if err := s.RegisterNewGroup(t.Context(), ownerB); err != nil {
		t.Fatalf("RegisterNewGroup(b): %v", err)
	}
	seedSecondUserAt(t, s, ownerA, "a2", ownerA.Now+1)

	scopeB, err := s.ForGroup(MustGroupID(ownerB.GroupID))
	if err != nil {
		t.Fatalf("ForGroup(b): %v", err)
	}

	got, err := scopeB.Members().List(t.Context())
	if err != nil {
		t.Fatalf("List(b): %v", err)
	}
	if len(got) != 1 || got[0].ID != ownerB.UserID {
		t.Fatalf("group B's member list = %+v, want exactly its own owner %q -- group A's members leaked across the scope", got, ownerB.UserID)
	}
}

func seedSecondUserAt(t *testing.T, s *Storage, p FirstUserParams, suffix string, now int64) (userID string) {
	t.Helper()
	userID = "usr-" + suffix
	if _, err := s.store.Writer().ExecContext(t.Context(),
		`INSERT INTO users (id, group_id, username, password_hash, role, created_at, updated_at, version, deleted_at, change_seq)
		 VALUES (?, ?, ?, ?, 'member', ?, ?, 1, NULL, 0)`,
		userID, p.GroupID, "user-"+suffix, "$argon2id$fake$"+suffix, now, now,
	); err != nil {
		t.Fatalf("seed second user at %d: %v", now, err)
	}
	return userID
}
