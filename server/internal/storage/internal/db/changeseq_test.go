package db

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
)

func allocOnce(t *testing.T, s *Store, groupID string) (int64, error) {
	t.Helper()
	var seq int64
	err := s.Tx(t.Context(), func(tx *sql.Tx) error {
		var err error
		seq, err = AllocChangeSeq(t.Context(), tx, groupID)
		return err
	})
	return seq, err
}

func readCounter(t *testing.T, s *Store, groupID string) int64 {
	t.Helper()
	var counter int64
	err := s.Reader().QueryRowContext(t.Context(),
		`SELECT change_seq_counter FROM groups WHERE id = ?`, groupID).Scan(&counter)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return counter
}

func TestAllocChangeSeqIsMonotonicFromOne(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")

	for want := int64(1); want <= 3; want++ {
		got, err := allocOnce(t, store, "g1")
		if err != nil {
			t.Fatalf("AllocChangeSeq: %v", err)
		}
		if got != want {
			t.Fatalf("AllocChangeSeq = %d, want %d", got, want)
		}
	}
	if got := readCounter(t, store, "g1"); got != 3 {
		t.Errorf("persisted counter = %d, want 3", got)
	}
}

func TestAllocChangeSeqIsPerGroup(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")
	seedGroup(t, store, "g2")

	for i := 0; i < 3; i++ {
		if _, err := allocOnce(t, store, "g1"); err != nil {
			t.Fatalf("AllocChangeSeq(g1): %v", err)
		}
	}
	got, err := allocOnce(t, store, "g2")
	if err != nil {
		t.Fatalf("AllocChangeSeq(g2): %v", err)
	}
	if got != 1 {
		t.Errorf("first g2 allocation = %d, want 1; the counter is shared across tenants", got)
	}
	if got := readCounter(t, store, "g1"); got != 3 {
		t.Errorf("g1 counter = %d, want 3", got)
	}
}

func TestAllocChangeSeqLosesNoUpdatesUnderConcurrency(t *testing.T) {
	const writers = 32

	store := newTestStore(t)
	seedGroup(t, store, "g1")

	seqs := make([]int64, writers)
	errs := make([]error, writers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var seq int64
			errs[i] = store.Tx(t.Context(), func(tx *sql.Tx) error {
				var err error
				seq, err = AllocChangeSeq(t.Context(), tx, "g1")
				return err
			})
			seqs[i] = seq
		}()
	}
	close(start)
	wg.Wait()

	seen := make(map[int64]int, writers)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
		if prev, dup := seen[seqs[i]]; dup {
			t.Fatalf("writers %d and %d both got change_seq %d", prev, i, seqs[i])
		}
		seen[seqs[i]] = i
	}
	for want := int64(1); want <= writers; want++ {
		if _, ok := seen[want]; !ok {
			t.Errorf("change_seq %d was never allocated", want)
		}
	}
	if got := readCounter(t, store, "g1"); got != writers {
		t.Errorf("persisted counter = %d, want %d", got, writers)
	}
}

func TestAllocChangeSeqRollsBackWithItsTransaction(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")
	sentinel := errors.New("caller failed")

	err := store.Tx(t.Context(), func(tx *sql.Tx) error {
		if _, err := AllocChangeSeq(t.Context(), tx, "g1"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Tx error = %v, want %v", err, sentinel)
	}
	if got := readCounter(t, store, "g1"); got != 0 {
		t.Errorf("counter after rollback = %d, want 0", got)
	}

	got, err := allocOnce(t, store, "g1")
	if err != nil {
		t.Fatalf("AllocChangeSeq: %v", err)
	}
	if got != 1 {
		t.Errorf("allocation after rollback = %d, want 1", got)
	}
}

func TestAllocChangeSeqReportsMissingGroup(t *testing.T) {
	store := newTestStore(t)
	seedGroup(t, store, "g1")

	_, err := allocOnce(t, store, "no-such-group")
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("error = %v, want ErrGroupNotFound", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Error("ErrGroupNotFound wraps sql.ErrNoRows; callers cannot distinguish the two")
	}
}
