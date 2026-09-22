package groups

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestNewRegistrationGateRejectsNilService(t *testing.T) {
	if _, err := NewRegistrationGate(nil, true); err == nil {
		t.Error("NewRegistrationGate accepted a nil *Service")
	}
}

func TestRegistrationGateAllowsTheFirstRegistrationRegardlessOfToggle(t *testing.T) {
	for _, open := range []bool{false, true} {
		t.Run(fmt.Sprintf("open=%v", open), func(t *testing.T) {
			svc := newTestService(t)
			gate, err := NewRegistrationGate(svc.Service, open)
			if err != nil {
				t.Fatalf("NewRegistrationGate: %v", err)
			}

			got, err := gate.Register(t.Context(), RegisterRequest{
				Username: "alice", Password: []byte("correct horse battery staple"),
			})
			if err != nil {
				t.Fatalf("Register: %v", err)
			}
			if got.Username != "alice" {
				t.Errorf("Username = %q, want %q", got.Username, "alice")
			}
		})
	}
}

func TestRegistrationGateClosedRefusesOnceAGroupExists(t *testing.T) {
	svc := newTestService(t)
	gate, err := NewRegistrationGate(svc.Service, false)
	if err != nil {
		t.Fatalf("NewRegistrationGate: %v", err)
	}

	if _, err := gate.Register(t.Context(), RegisterRequest{
		Username: "alice", Password: []byte("correct horse battery staple"),
	}); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, err = gate.Register(t.Context(), RegisterRequest{
		Username: "bob", Password: []byte("another good password"),
	})
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("second Register (gate closed) error = %v, want ErrRegistrationClosed", err)
	}

	var groupCount int
	if err := svc.row(t, "SELECT COUNT(*) FROM groups").Scan(&groupCount); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 1 {
		t.Errorf("groups row count = %d, want 1 (the rejected attempt must not have written anything)", groupCount)
	}
}

func TestRegistrationGateOpenCreatesASecondSeparateGroup(t *testing.T) {
	svc := newTestService(t)
	gate, err := NewRegistrationGate(svc.Service, true)
	if err != nil {
		t.Fatalf("NewRegistrationGate: %v", err)
	}

	alice, err := gate.Register(t.Context(), RegisterRequest{
		Username: "alice", Password: []byte("correct horse battery staple"),
	})
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}

	bob, err := gate.Register(t.Context(), RegisterRequest{
		Username: "bob", Password: []byte("another good password"),
	})
	if err != nil {
		t.Fatalf("second Register (gate open): %v", err)
	}

	if bob.GroupID == "" {
		t.Fatal("second Register returned an empty GroupID")
	}
	if bob.GroupID == alice.GroupID {
		t.Fatalf("bob joined alice's existing group (%s); the gate must create a new one, not join the existing one (FR-006 is invites' job)", bob.GroupID)
	}

	var groupCount int
	if err := svc.row(t, "SELECT COUNT(*) FROM groups").Scan(&groupCount); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 2 {
		t.Fatalf("groups row count = %d, want 2", groupCount)
	}

	var bobRole string
	if err := svc.row(t, "SELECT role FROM users WHERE id = ?", bob.UserID).Scan(&bobRole); err != nil {
		t.Fatalf("read bob's role: %v", err)
	}
	if bobRole != "owner" {
		t.Errorf("bob's role = %q, want %q", bobRole, "owner")
	}
}

func TestRegistrationGateOpenStillValidatesTheNewGroupRequest(t *testing.T) {
	svc := newTestService(t)
	gate, err := NewRegistrationGate(svc.Service, true)
	if err != nil {
		t.Fatalf("NewRegistrationGate: %v", err)
	}

	if _, err := gate.Register(t.Context(), RegisterRequest{
		Username: "alice", Password: []byte("correct horse battery staple"),
	}); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	if _, err := gate.Register(t.Context(), RegisterRequest{
		Username: "", Password: []byte("another good password"),
	}); !errors.Is(err, ErrUsernameInvalid) {
		t.Errorf("Register with empty username (gate open, group exists) error = %v, want ErrUsernameInvalid", err)
	}

	if _, err := gate.Register(t.Context(), RegisterRequest{
		Username: "carol", Password: []byte("short"),
	}); !errors.Is(err, ErrPasswordInvalid) {
		t.Errorf("Register with a too-short password (gate open, group exists) error = %v, want ErrPasswordInvalid", err)
	}

	var groupCount int
	if err := svc.row(t, "SELECT COUNT(*) FROM groups").Scan(&groupCount); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 1 {
		t.Errorf("groups row count = %d, want 1 (both invalid attempts must have written nothing)", groupCount)
	}
}

func TestRegistrationGateOpenZeroesThePasswordSlice(t *testing.T) {
	svc := newTestService(t)
	gate, err := NewRegistrationGate(svc.Service, true)
	if err != nil {
		t.Fatalf("NewRegistrationGate: %v", err)
	}

	if _, err := gate.Register(t.Context(), RegisterRequest{
		Username: "alice", Password: []byte("correct horse battery staple"),
	}); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	password := []byte("another good password")
	if _, err := gate.Register(t.Context(), RegisterRequest{Username: "bob", Password: password}); err != nil {
		t.Fatalf("second Register (gate open): %v", err)
	}
	for i, b := range password {
		if b != 0 {
			t.Fatalf("password[%d] = %d, want 0; the gate did not zero the caller's buffer", i, b)
		}
	}
}

func TestRegistrationGateOpenConcurrentRegistrationsEachGetTheirOwnGroup(t *testing.T) {
	const racers = 8

	svc := newTestService(t)
	gate, err := NewRegistrationGate(svc.Service, true)
	if err != nil {
		t.Fatalf("NewRegistrationGate: %v", err)
	}

	if _, err := gate.Register(t.Context(), RegisterRequest{
		Username: "founder", Password: []byte("correct horse battery staple"),
	}); err != nil {
		t.Fatalf("founding Register: %v", err)
	}

	results := make([]Registered, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = gate.Register(t.Context(), RegisterRequest{
				Username: fmt.Sprintf("racer%02d", i),
				Password: []byte("correct horse battery staple"),
			})
		}(i)
	}
	close(start)
	wg.Wait()

	seen := make(map[string]bool, racers)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: Register: %v", i, err)
		}
		if seen[results[i].GroupID] {
			t.Fatalf("racer %d: group id %s was already used by another racer", i, results[i].GroupID)
		}
		seen[results[i].GroupID] = true
	}

	var groupCount int
	if err := svc.row(t, "SELECT COUNT(*) FROM groups").Scan(&groupCount); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != racers+1 {
		t.Fatalf("groups row count = %d, want %d (the founder plus one per racer)", groupCount, racers+1)
	}
}
