package storage

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestRegisterNewGroupCreatesGroupAndOwnerOnEmptyDatabase(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("solo")

	if err := s.RegisterNewGroup(t.Context(), p); err != nil {
		t.Fatalf("RegisterNewGroup: %v", err)
	}

	if got := countRows(t, s, "groups"); got != 1 {
		t.Fatalf("groups row count = %d, want 1", got)
	}
	if got := countRows(t, s, "users"); got != 1 {
		t.Fatalf("users row count = %d, want 1", got)
	}

	var role string
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT role FROM users WHERE id = ?", p.UserID,
	).Scan(&role); err != nil {
		t.Fatalf("read created user: %v", err)
	}
	if role != ownerRole {
		t.Errorf("role = %q, want %q (every group's first user is its owner)", role, ownerRole)
	}
}

func TestRegisterNewGroupSucceedsWhenAGroupAlreadyExists(t *testing.T) {
	s := newTestStorage(t)
	first := firstUserParams("first")
	if err := s.RegisterFirstUser(t.Context(), first); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	if err := s.RegisterFirstUser(t.Context(), firstUserParams("would-be-second")); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("RegisterFirstUser after one group exists: error = %v, want ErrRegistrationClosed", err)
	}

	second := firstUserParams("second")
	if err := s.RegisterNewGroup(t.Context(), second); err != nil {
		t.Fatalf("RegisterNewGroup with an existing group present: %v", err)
	}

	if got := countRows(t, s, "groups"); got != 2 {
		t.Fatalf("groups row count = %d, want 2 (the original plus the new one)", got)
	}
	if got := countRows(t, s, "users"); got != 2 {
		t.Fatalf("users row count = %d, want 2", got)
	}

	for _, p := range []FirstUserParams{first, second} {
		var groupID, role string
		if err := s.store.Writer().QueryRowContext(t.Context(),
			"SELECT group_id, role FROM users WHERE id = ?", p.UserID,
		).Scan(&groupID, &role); err != nil {
			t.Fatalf("read user %s: %v", p.UserID, err)
		}
		if groupID != p.GroupID {
			t.Errorf("user %s group_id = %q, want %q", p.UserID, groupID, p.GroupID)
		}
		if role != ownerRole {
			t.Errorf("user %s role = %q, want %q", p.UserID, role, ownerRole)
		}
	}
}

func TestRegisterNewGroupConcurrentCallsEachSucceed(t *testing.T) {
	const racers = 16

	s := newTestStorage(t)
	params := make([]FirstUserParams, racers)
	for i := range params {
		params[i] = firstUserParams(fmt.Sprintf("racer%02d", i))
	}

	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = s.RegisterNewGroup(t.Context(), params[i])
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: RegisterNewGroup: %v", i, err)
		}
	}

	if got := countRows(t, s, "groups"); got != racers {
		t.Fatalf("groups row count = %d, want %d (every racer creates its own group)", got, racers)
	}
	if got := countRows(t, s, "users"); got != racers {
		t.Fatalf("users row count = %d, want %d", got, racers)
	}
}

func TestRegisterNewGroupRejectsIncompleteParams(t *testing.T) {
	s := newTestStorage(t)
	base := firstUserParams("valid")

	cases := []struct {
		name   string
		mutate func(*FirstUserParams)
	}{
		{"empty GroupID", func(p *FirstUserParams) { p.GroupID = "" }},
		{"empty GroupName", func(p *FirstUserParams) { p.GroupName = "" }},
		{"empty UserID", func(p *FirstUserParams) { p.UserID = "" }},
		{"empty Username", func(p *FirstUserParams) { p.Username = "" }},
		{"empty PasswordHash", func(p *FirstUserParams) { p.PasswordHash = "" }},
		{"zero Now", func(p *FirstUserParams) { p.Now = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mutate(&p)
			if err := s.RegisterNewGroup(t.Context(), p); err == nil {
				t.Errorf("RegisterNewGroup(%+v) succeeded, want a validation error", p)
			}
		})
	}

	if got := countRows(t, s, "groups"); got != 0 {
		t.Fatalf("groups row count after only-invalid attempts = %d, want 0", got)
	}
}

func TestRegisterNewGroupRefusesUnopenedStorage(t *testing.T) {
	t.Run("zero Storage", func(t *testing.T) {
		if err := (&Storage{}).RegisterNewGroup(t.Context(), firstUserParams("x")); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("RegisterNewGroup on a zero Storage: error = %v, want ErrNoGroup", err)
		}
	})
	t.Run("nil Storage", func(t *testing.T) {
		var s *Storage
		if err := s.RegisterNewGroup(t.Context(), firstUserParams("x")); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("RegisterNewGroup on a nil *Storage: error = %v, want ErrNoGroup", err)
		}
	})
}
