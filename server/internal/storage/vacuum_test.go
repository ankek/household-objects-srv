package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStorageVacuumIntoProducesAnIndependentlyOpenableSnapshotWithMatchingData(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)
	seedItem(t, s, "groupA", "itemA2", 20)
	seedItem(t, s, "groupB", "itemB1", 30)

	dst := filepath.Join(t.TempDir(), "snapshot.db")
	if err := s.VacuumInto(t.Context(), dst); err != nil {
		t.Fatalf("VacuumInto: %v", err)
	}

	snap, err := Open(t.Context(), Config{Path: dst, ReadConns: 2})
	if err != nil {
		t.Fatalf("Open(snapshot) -- VacuumInto did not produce a valid, independently-openable database: %v", err)
	}
	defer func() {
		if err := snap.Close(); err != nil {
			t.Errorf("Close(snapshot): %v", err)
		}
	}()

	scopeA, err := snap.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA) on snapshot: %v", err)
	}
	listed, err := scopeA.Items().List(t.Context(), Page{Limit: 100})
	if err != nil {
		t.Fatalf("List on snapshot: %v", err)
	}
	if got := itemIDs(listed); len(got) != 2 || got[0] != "itemA1" || got[1] != "itemA2" {
		t.Fatalf("snapshot groupA items = %v, want [itemA1 itemA2]", got)
	}

	scopeB, err := snap.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB) on snapshot: %v", err)
	}
	gotB, err := scopeB.Items().Get(t.Context(), "itemB1")
	if err != nil {
		t.Fatalf("Get(itemB1) on snapshot: %v", err)
	}
	wantB, err := (func() (Item, error) {
		scopeBSrc, err := s.ForGroup(MustGroupID("groupB"))
		if err != nil {
			return Item{}, err
		}
		return scopeBSrc.Items().Get(t.Context(), "itemB1")
	})()
	if err != nil {
		t.Fatalf("Get(itemB1) on source: %v", err)
	}
	if gotB != wantB {
		t.Fatalf("snapshot row = %+v, want the source row unchanged = %+v", gotB, wantB)
	}
}

func TestStorageVacuumIntoWritesACompactedSingleFileNotAWalCopy(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)

	dst := filepath.Join(t.TempDir(), "snapshot.db")
	if err := s.VacuumInto(t.Context(), dst); err != nil {
		t.Fatalf("VacuumInto: %v", err)
	}

	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dst + suffix); !os.IsNotExist(err) {
			t.Errorf("snapshot has a %s sidecar (err=%v); VACUUM INTO must produce one compacted file, not a copy expecting WAL siblings", suffix, err)
		}
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("Stat(snapshot): %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("snapshot file is empty")
	}
}

func TestStorageVacuumIntoSnapshotIsUnaffectedByLaterSourceWrites(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)

	dst := filepath.Join(t.TempDir(), "snapshot.db")
	if err := s.VacuumInto(t.Context(), dst); err != nil {
		t.Fatalf("VacuumInto: %v", err)
	}

	seedItem(t, s, "groupA", "itemA2", 20)

	snap, err := Open(t.Context(), Config{Path: dst, ReadConns: 2})
	if err != nil {
		t.Fatalf("Open(snapshot): %v", err)
	}
	defer func() {
		if err := snap.Close(); err != nil {
			t.Errorf("Close(snapshot): %v", err)
		}
	}()

	scopeA, err := snap.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA) on snapshot: %v", err)
	}
	listed, err := scopeA.Items().List(t.Context(), Page{Limit: 100})
	if err != nil {
		t.Fatalf("List on snapshot: %v", err)
	}
	if got := itemIDs(listed); len(got) != 1 || got[0] != "itemA1" {
		t.Fatalf("snapshot groupA items = %v, want exactly [itemA1] -- a write made to the source AFTER the snapshot leaked into it", got)
	}

	scopeASrc, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA) on source: %v", err)
	}
	listedSrc, err := scopeASrc.Items().List(t.Context(), Page{Limit: 100})
	if err != nil {
		t.Fatalf("List on source: %v", err)
	}
	if got := itemIDs(listedSrc); len(got) != 2 {
		t.Fatalf("source groupA items = %v, want 2 -- the later write itself did not land", got)
	}
}

func TestStorageVacuumIntoRefusesAnExistingDestination(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA1", 10)

	dst := filepath.Join(t.TempDir(), "snapshot.db")
	if err := s.VacuumInto(t.Context(), dst); err != nil {
		t.Fatalf("first VacuumInto: %v", err)
	}
	if err := s.VacuumInto(t.Context(), dst); err == nil {
		t.Fatal("second VacuumInto at the same destination succeeded, want an error -- VACUUM INTO must refuse to overwrite an existing file")
	}
}

func TestStorageVacuumIntoRejectsAnEmptyDestination(t *testing.T) {
	s := newTestStorage(t)
	if err := s.VacuumInto(t.Context(), ""); err == nil {
		t.Fatal("VacuumInto(\"\") succeeded, want a validation error")
	}
}

func TestStorageVacuumIntoRefusesAnUnopenedStorage(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "snapshot.db")

	t.Run("unopened storage", func(t *testing.T) {
		if err := (&Storage{}).VacuumInto(t.Context(), dst); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("VacuumInto on a zero Storage: error = %v, want ErrNoGroup", err)
		}
	})

	t.Run("nil storage", func(t *testing.T) {
		var s *Storage
		if err := s.VacuumInto(t.Context(), dst); !errors.Is(err, ErrNoGroup) {
			t.Fatalf("VacuumInto on a nil *Storage: error = %v, want ErrNoGroup", err)
		}
	})
}

func TestStorageVacuumIntoDoesNotRaceOrDeadlockAgainstConcurrentWrites(t *testing.T) {
	const writers = 8
	const snapshots = 4

	s := newTestStorage(t)
	seedItem(t, s, "groupA", "seed", 1)
	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	dir := t.TempDir()
	writeErrs := make([]error, writers)
	snapshotErrs := make([]error, snapshots)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := scopeA.Items().Create(t.Context(), CreateItemParams{
				ID:        fmt.Sprintf("item-%d", i),
				Name:      fmt.Sprintf("item %d", i),
				ShortCode: fmt.Sprintf("SC%d", i),
				Now:       time.Now().UnixMilli(),
			})
			writeErrs[i] = err
		}(i)
	}
	for i := 0; i < snapshots; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			snapshotErrs[i] = s.VacuumInto(t.Context(), filepath.Join(dir, fmt.Sprintf("snap-%d.db", i)))
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range writeErrs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	for i, err := range snapshotErrs {
		if err != nil {
			t.Errorf("snapshot %d: %v", i, err)
		}
	}

	listed, err := scopeA.Items().List(t.Context(), Page{Limit: 100})
	if err != nil {
		t.Fatalf("List after concurrency: %v", err)
	}
	if len(listed) != writers+1 {
		t.Fatalf("groupA has %d items after concurrent writes+snapshots, want %d (%d writers + the seed row) -- a lost write", len(listed), writers+1, writers)
	}
}
