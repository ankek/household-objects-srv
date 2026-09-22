package backup

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(t.TempDir(), "db", "hho.db"), ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func newGroupScope(t *testing.T, s *storage.Storage, groupID string) storage.Scope {
	t.Helper()
	err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    groupID,
		UserID:       groupID + "-owner",
		Username:     groupID + "-owner",
		PasswordHash: "not-a-real-hash",
		Now:          time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("RegisterNewGroup(%s): %v", groupID, err)
	}
	scope, err := s.ForGroup(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%s): %v", groupID, err)
	}
	return scope
}

func seedItemInScope(t *testing.T, scope storage.Scope, itemID string) {
	t.Helper()
	_, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID: itemID, Name: itemID + " name", ShortCode: itemID, Now: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("seed item %q: %v", itemID, err)
	}
}

func TestSnapshotProducesAnIndependentlyOpenableDatabaseWithMatchingData(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "groupA"), "itemA1")

	dst := filepath.Join(t.TempDir(), "hho-snapshot.db")
	if err := Snapshot(t.Context(), s, dst); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	snap, err := storage.Open(t.Context(), storage.Config{Path: dst, ReadConns: 2})
	if err != nil {
		t.Fatalf("Open(snapshot) -- Snapshot did not produce a valid, independently-openable database: %v", err)
	}
	defer func() {
		if err := snap.Close(); err != nil {
			t.Errorf("Close(snapshot): %v", err)
		}
	}()

	scope, err := snap.ForGroup(storage.MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup on snapshot: %v", err)
	}
	got, err := scope.Items().Get(t.Context(), "itemA1")
	if err != nil {
		t.Fatalf("Get(itemA1) on snapshot: %v", err)
	}
	if got.ID != "itemA1" || got.GroupID != "groupA" {
		t.Fatalf("snapshot row = %+v, want id itemA1 in groupA", got)
	}
}

func TestSnapshotCreatesMissingDestinationDirectories(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "groupA"), "itemA1")

	dst := filepath.Join(t.TempDir(), "backups", "nested", "hho-snapshot.db")
	if err := Snapshot(t.Context(), s, dst); err != nil {
		t.Fatalf("Snapshot into a not-yet-created directory: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("Stat(dst): %v", err)
	}
}

func TestSnapshotLeavesNoTempFileBehindOnSuccess(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "groupA"), "itemA1")

	dir := t.TempDir()
	dst := filepath.Join(dir, "hho-snapshot.db")
	if err := Snapshot(t.Context(), s, dst); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(dst) {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("directory contains %v, want exactly [%s] -- a staging temp file was left behind", names, filepath.Base(dst))
	}
}

func TestSnapshotCanBeRetriedAtTheSameDestination(t *testing.T) {
	s := newTestStorage(t)
	scope := newGroupScope(t, s, "groupA")
	seedItemInScope(t, scope, "itemA1")

	dst := filepath.Join(t.TempDir(), "hho-snapshot.db")
	if err := Snapshot(t.Context(), s, dst); err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}

	seedItemInScope(t, scope, "itemA2")
	if err := Snapshot(t.Context(), s, dst); err != nil {
		t.Fatalf("second Snapshot at the same destination: %v, want success", err)
	}

	snap, err := storage.Open(t.Context(), storage.Config{Path: dst, ReadConns: 2})
	if err != nil {
		t.Fatalf("Open(snapshot): %v", err)
	}
	defer func() {
		if err := snap.Close(); err != nil {
			t.Errorf("Close(snapshot): %v", err)
		}
	}()
	snapScope, err := snap.ForGroup(storage.MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}
	if _, err := snapScope.Items().Get(t.Context(), "itemA2"); err != nil {
		t.Fatalf("Get(itemA2) on the SECOND snapshot: %v, want the retried snapshot to reflect the latest state", err)
	}
}

func TestSnapshotOutputIsOwnerOnlyReadable(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "groupA"), "itemA1")

	dst := filepath.Join(t.TempDir(), "hho-snapshot.db")
	if err := Snapshot(t.Context(), s, dst); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("Stat(dst): %v", err)
	}
	if got := info.Mode().Perm(); got != snapshotFileMode {
		t.Fatalf("snapshot file mode = %#o, want exactly %#o (owner read/write only)", got, snapshotFileMode)
	}
}

func TestSnapshotRejectsAnEmptyDestination(t *testing.T) {
	s := newTestStorage(t)
	if err := Snapshot(t.Context(), s, ""); err == nil {
		t.Fatal("Snapshot(\"\") succeeded, want a validation error")
	}
}
