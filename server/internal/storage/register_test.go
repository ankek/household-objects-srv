package storage

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func firstUserParams(suffix string) FirstUserParams {
	return FirstUserParams{
		GroupID:      "grp-" + suffix,
		GroupName:    "Household " + suffix,
		UserID:       "usr-" + suffix,
		Username:     "user-" + suffix,
		PasswordHash: "$argon2id$fake$" + suffix,
		Now:          1_700_000_000_000,
	}
}

func countRows(t *testing.T, s *Storage, table string) int {
	t.Helper()
	var n int
	if err := s.store.Writer().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestRegisterFirstUserCreatesGroupAndOwner(t *testing.T) {
	s := newTestStorage(t)
	p := firstUserParams("a")

	if err := s.RegisterFirstUser(t.Context(), p); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	if got := countRows(t, s, "groups"); got != 1 {
		t.Fatalf("groups row count = %d, want 1", got)
	}
	if got := countRows(t, s, "users"); got != 1 {
		t.Fatalf("users row count = %d, want 1", got)
	}

	var name string
	var registrationEnabled, changeSeqCounter int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT name, registration_enabled, change_seq_counter FROM groups WHERE id = ?", p.GroupID,
	).Scan(&name, &registrationEnabled, &changeSeqCounter); err != nil {
		t.Fatalf("read created group: %v", err)
	}
	if name != p.GroupName {
		t.Errorf("group name = %q, want %q", name, p.GroupName)
	}
	if registrationEnabled != 0 {
		t.Errorf("registration_enabled = %d, want 0 (FR-005: closed by default after first run)", registrationEnabled)
	}
	if changeSeqCounter != 1 {
		t.Errorf("group change_seq_counter = %d, want 1 (bumped once, to stamp the owner row)", changeSeqCounter)
	}

	var username, passwordHash, role string
	var groupID string
	var changeSeq int64
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT group_id, username, password_hash, role, change_seq FROM users WHERE id = ?", p.UserID,
	).Scan(&groupID, &username, &passwordHash, &role, &changeSeq); err != nil {
		t.Fatalf("read created user: %v", err)
	}
	if groupID != p.GroupID {
		t.Errorf("user group_id = %q, want %q", groupID, p.GroupID)
	}
	if username != p.Username {
		t.Errorf("username = %q, want %q", username, p.Username)
	}
	if passwordHash != p.PasswordHash {
		t.Errorf("password_hash = %q, want %q", passwordHash, p.PasswordHash)
	}
	if role != ownerRole {
		t.Errorf("role = %q, want %q (FR-007: the first user is the owner, not the member default)", role, ownerRole)
	}
	if changeSeq != 1 {
		t.Errorf("user change_seq = %d, want 1", changeSeq)
	}
}

func TestRegisterFirstUserRejectsSecondRegistration(t *testing.T) {
	s := newTestStorage(t)
	first := firstUserParams("first")
	if err := s.RegisterFirstUser(t.Context(), first); err != nil {
		t.Fatalf("first RegisterFirstUser: %v", err)
	}

	second := firstUserParams("second")
	err := s.RegisterFirstUser(t.Context(), second)
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("second RegisterFirstUser error = %v, want ErrRegistrationClosed", err)
	}

	if got := countRows(t, s, "groups"); got != 1 {
		t.Fatalf("groups row count after rejected second registration = %d, want 1", got)
	}
	if got := countRows(t, s, "users"); got != 1 {
		t.Fatalf("users row count after rejected second registration = %d, want 1", got)
	}

	var username string
	if err := s.store.Writer().QueryRowContext(t.Context(),
		"SELECT username FROM users WHERE id = ?", first.UserID,
	).Scan(&username); err != nil {
		t.Fatalf("read first user: %v", err)
	}
	if username != first.Username {
		t.Errorf("surviving user = %q, want the first attempt's %q", username, first.Username)
	}
}

func TestRegisterFirstUserConcurrentRaceCreatesExactlyOneGroup(t *testing.T) {
	const racers = 32

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
			errs[i] = s.RegisterFirstUser(t.Context(), params[i])
		}(i)
	}
	close(start)
	wg.Wait()

	var winners []int
	for i, err := range errs {
		switch {
		case err == nil:
			winners = append(winners, i)
		case errors.Is(err, ErrRegistrationClosed):
		default:
			t.Fatalf("racer %d: unexpected error %v (want nil or ErrRegistrationClosed)", i, err)
		}
	}

	if len(winners) != 1 {
		t.Fatalf("%d of %d racers succeeded, want exactly 1; the atomicity mechanism let more than one registration through", len(winners), racers)
	}
	winner := params[winners[0]]

	if got := countRows(t, s, "groups"); got != 1 {
		t.Fatalf("groups row count after the race = %d, want 1", got)
	}
	if got := countRows(t, s, "users"); got != 1 {
		t.Fatalf("users row count after the race = %d, want 1", got)
	}

	var groupID string
	if err := s.store.Writer().QueryRowContext(t.Context(), "SELECT id FROM groups").Scan(&groupID); err != nil {
		t.Fatalf("read the surviving group: %v", err)
	}
	if groupID != winner.GroupID {
		t.Errorf("surviving group id = %q, want the reported winner's %q", groupID, winner.GroupID)
	}

	var username, role string
	if err := s.store.Writer().QueryRowContext(t.Context(), "SELECT username, role FROM users").Scan(&username, &role); err != nil {
		t.Fatalf("read the surviving user: %v", err)
	}
	if username != winner.Username {
		t.Errorf("surviving user = %q, want the reported winner's %q", username, winner.Username)
	}
	if role != ownerRole {
		t.Errorf("surviving user role = %q, want %q", role, ownerRole)
	}
}

func TestRegisterFirstUserRejectsIncompleteParams(t *testing.T) {
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
		{"negative Now", func(p *FirstUserParams) { p.Now = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mutate(&p)
			if err := s.RegisterFirstUser(t.Context(), p); err == nil {
				t.Errorf("RegisterFirstUser(%+v) succeeded, want a validation error", p)
			}
		})
	}

	if got := countRows(t, s, "groups"); got != 0 {
		t.Fatalf("groups row count after only-invalid attempts = %d, want 0", got)
	}
}

func TestRegisterFirstUserRefusesUnopenedStorage(t *testing.T) {
	t.Run("zero Storage", func(t *testing.T) {
		if err := (&Storage{}).RegisterFirstUser(t.Context(), firstUserParams("x")); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("RegisterFirstUser on a zero Storage: error = %v, want ErrNoGroup", err)
		}
	})
	t.Run("nil Storage", func(t *testing.T) {
		var s *Storage
		if err := s.RegisterFirstUser(t.Context(), firstUserParams("x")); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("RegisterFirstUser on a nil *Storage: error = %v, want ErrNoGroup", err)
		}
	})
}

func TestHasAnyGroupReportsFalseOnAnEmptyDatabase(t *testing.T) {
	s := newTestStorage(t)

	got, err := s.HasAnyGroup(t.Context())
	if err != nil {
		t.Fatalf("HasAnyGroup: %v", err)
	}
	if got {
		t.Error("HasAnyGroup = true on an empty database, want false")
	}
}

func TestHasAnyGroupReportsTrueOnceAGroupExists(t *testing.T) {
	s := newTestStorage(t)
	if err := s.RegisterFirstUser(t.Context(), firstUserParams("a")); err != nil {
		t.Fatalf("RegisterFirstUser: %v", err)
	}

	got, err := s.HasAnyGroup(t.Context())
	if err != nil {
		t.Fatalf("HasAnyGroup: %v", err)
	}
	if !got {
		t.Error("HasAnyGroup = false once a group exists, want true")
	}
}

func TestHasAnyGroupRefusesUnopenedStorage(t *testing.T) {
	t.Run("zero Storage", func(t *testing.T) {
		if _, err := (&Storage{}).HasAnyGroup(t.Context()); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("HasAnyGroup on a zero Storage: error = %v, want ErrNoGroup", err)
		}
	})
	t.Run("nil Storage", func(t *testing.T) {
		var s *Storage
		if _, err := s.HasAnyGroup(t.Context()); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("HasAnyGroup on a nil *Storage: error = %v, want ErrNoGroup", err)
		}
	})
}
