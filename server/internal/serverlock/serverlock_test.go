package serverlock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireThenProbeReportsHeld(t *testing.T) {
	dataDir := t.TempDir()

	lock, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lock.Close() })

	err = Probe(dataDir)
	if err == nil {
		t.Fatal("Probe = nil, want a HeldError while the lock is held")
	}
	if !IsHeld(err) {
		t.Fatalf("IsHeld(%v) = false, want true", err)
	}
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("errors.As into *HeldError failed for %v", err)
	}
	if held.PID != os.Getpid() {
		t.Errorf("HeldError.PID = %d, want this test process's own pid %d", held.PID, os.Getpid())
	}
	if !strings.Contains(err.Error(), "hho serve") {
		t.Errorf("HeldError.Error() = %q, want it to name \"hho serve\"", err.Error())
	}
}

func TestAcquireFailsWhileAlreadyHeld(t *testing.T) {
	dataDir := t.TempDir()

	first, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	_, err = Acquire(dataDir)
	if err == nil {
		t.Fatal("second Acquire = nil, want a HeldError while the first is still held")
	}
	if !IsHeld(err) {
		t.Fatalf("IsHeld(%v) = false, want true", err)
	}
}

func TestCloseReleasesTheLock(t *testing.T) {
	dataDir := t.TempDir()

	lock, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := Probe(dataDir); !IsHeld(err) {
		t.Fatalf("Probe before Close = %v, want a HeldError", err)
	}

	if err := lock.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := Probe(dataDir); err != nil {
		t.Fatalf("Probe after Close = %v, want nil (lock released)", err)
	}

	second, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("Acquire after Close: %v", err)
	}
	_ = second.Close()
}

func TestCloseIsIdempotentAndNilSafe(t *testing.T) {
	var nilLock *Lock
	if err := nilLock.Close(); err != nil {
		t.Errorf("nil *Lock Close() = %v, want nil", err)
	}

	dataDir := t.TempDir()
	lock, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Errorf("second Close: %v, want nil (idempotent)", err)
	}
}

func TestProbeOnAnEmptyDataDirIsFree(t *testing.T) {
	dataDir := t.TempDir()
	if err := Probe(dataDir); err != nil {
		t.Fatalf("Probe on a fresh data directory = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, FileName)); err != nil {
		t.Errorf("stat lock file after Probe: %v, want it to have been created", err)
	}
}

func TestLockFileSurvivesClose(t *testing.T) {
	dataDir := t.TempDir()
	lock, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, FileName)); err != nil {
		t.Errorf("lock file removed on Close: %v, want it to remain", err)
	}
}
