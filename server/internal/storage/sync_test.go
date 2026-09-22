package storage

import (
	"errors"
	"testing"
)

func syncRepoFor(t *testing.T) (repoA, repoB SyncRepository) {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	seedItem(t, s, "groupB", "itemB", 20)

	repoA, err := s.ForGroupSync(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroupSync(groupA): %v", err)
	}
	repoB, err = s.ForGroupSync(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroupSync(groupB): %v", err)
	}
	return repoA, repoB
}

func TestLowWatermarkDefaultsToZero(t *testing.T) {
	repoA, _ := syncRepoFor(t)

	got, err := repoA.LowWatermark(t.Context())
	if err != nil {
		t.Fatalf("LowWatermark: %v", err)
	}
	if got != 0 {
		t.Errorf("fresh group's low watermark = %d, want 0", got)
	}
}

func TestSetLowWatermarkRoundTrips(t *testing.T) {
	repoA, repoB := syncRepoFor(t)

	if err := repoA.SetLowWatermark(t.Context(), 12345); err != nil {
		t.Fatalf("SetLowWatermark(groupA): %v", err)
	}

	got, err := repoA.LowWatermark(t.Context())
	if err != nil {
		t.Fatalf("LowWatermark(groupA): %v", err)
	}
	if got != 12345 {
		t.Errorf("groupA low watermark = %d, want 12345", got)
	}

	gotB, err := repoB.LowWatermark(t.Context())
	if err != nil {
		t.Fatalf("LowWatermark(groupB): %v", err)
	}
	if gotB != 0 {
		t.Errorf("groupB low watermark after SetLowWatermark(groupA) = %d, want unaffected 0", gotB)
	}
}

func TestSetLowWatermarkRejectsNegative(t *testing.T) {
	repoA, _ := syncRepoFor(t)

	if err := repoA.SetLowWatermark(t.Context(), -1); err == nil {
		t.Error("SetLowWatermark(-1) succeeded, want an error")
	}
}

func TestLowWatermarkUnknownGroupNotFound(t *testing.T) {
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)

	repo, err := s.ForGroupSync(MustGroupID("does-not-exist"))
	if err != nil {
		t.Fatalf("ForGroupSync(does-not-exist): %v", err)
	}

	if _, err := repo.LowWatermark(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Errorf("LowWatermark for a nonexistent group: err = %v, want ErrNotFound", err)
	}
	if err := repo.SetLowWatermark(t.Context(), 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetLowWatermark for a nonexistent group: err = %v, want ErrNotFound", err)
	}
}
