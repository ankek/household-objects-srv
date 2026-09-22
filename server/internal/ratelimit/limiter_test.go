package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestAllowPermitsAKeyThatHasNeverFailed(t *testing.T) {
	l := New(Config{Clock: newFakeClock()})

	ok, retryAfter := l.Allow("never-seen")
	if !ok {
		t.Errorf("Allow(never-seen) = (%v, %v), want (true, 0)", ok, retryAfter)
	}
	if got := l.len(); got != 0 {
		t.Errorf("Allow on an unknown key created an entry; len = %d, want 0", got)
	}
}

func TestFreeAttemptsAreNotBlocked(t *testing.T) {
	clock := newFakeClock()
	l := New(Config{Clock: clock, FreeAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Minute})

	for i := 0; i < 3; i++ {
		l.RecordFailure("alice")
		if ok, retryAfter := l.Allow("alice"); !ok {
			t.Fatalf("after %d failures: Allow = (%v, %v), want (true, 0) -- still inside FreeAttempts", i+1, ok, retryAfter)
		}
	}
}

func TestFailureBeyondFreeAttemptsBlocksWithBackoff(t *testing.T) {
	clock := newFakeClock()
	l := New(Config{Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute})

	l.RecordFailure("alice")
	l.RecordFailure("alice")

	ok, retryAfter := l.Allow("alice")
	if ok {
		t.Fatal("Allow returned true immediately after the blocking failure, want false")
	}
	if retryAfter != time.Second {
		t.Errorf("retryAfter = %v, want exactly BaseDelay (%v)", retryAfter, time.Second)
	}

	clock.Advance(999 * time.Millisecond)
	if ok, _ := l.Allow("alice"); ok {
		t.Error("Allow returned true 1ms before the block expires")
	}

	clock.Advance(1 * time.Millisecond)
	if ok, _ := l.Allow("alice"); !ok {
		t.Error("Allow returned false once the clock reached blockedUntil, want true")
	}
}

func TestBackoffDoublesThenCaps(t *testing.T) {
	clock := newFakeClock()
	l := New(Config{Clock: clock, FreeAttempts: 0, BaseDelay: time.Second, MaxDelay: 30 * time.Second})

	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		l.RecordSuccess("bob")
		for range DefaultFreeAttempts {
			l.RecordFailure("bob")
		}
		l.RecordFailure("bob")
		for range i {
			l.RecordFailure("bob")
		}
		_, retryAfter := l.Allow("bob")
		if retryAfter != w {
			t.Errorf("iteration %d: retryAfter = %v, want %v", i, retryAfter, w)
		}
	}

	for range 200 {
		l.RecordFailure("bob")
	}
	if _, retryAfter := l.Allow("bob"); retryAfter != 30*time.Second {
		t.Errorf("after 200+ failures: retryAfter = %v, want exactly the cap (%v), not an overflowed value", retryAfter, 30*time.Second)
	}
}

func TestRecordSuccessClearsHistory(t *testing.T) {
	clock := newFakeClock()
	l := New(Config{Clock: clock, FreeAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Minute})

	l.RecordFailure("alice")
	l.RecordFailure("alice")
	if ok, _ := l.Allow("alice"); ok {
		t.Fatal("expected alice to be blocked before RecordSuccess")
	}

	l.RecordSuccess("alice")
	if ok, retryAfter := l.Allow("alice"); !ok {
		t.Errorf("after RecordSuccess: Allow = (%v, %v), want (true, 0)", ok, retryAfter)
	}
	if got := l.len(); got != 0 {
		t.Errorf("after RecordSuccess: len = %d, want 0 (the entry should be gone, not merely unblocked)", got)
	}
}

func TestUnknownAndKnownUsernamesAreBlockedIdentically(t *testing.T) {
	clock := newFakeClock()
	l := New(Config{Clock: clock, FreeAttempts: 2, BaseDelay: time.Second, MaxDelay: time.Minute})

	for _, key := range []string{"acct:alice-is-real", "acct:mallory-made-this-up"} {
		t.Run(key, func(t *testing.T) {
			for i := 0; i < 2; i++ {
				l.RecordFailure(key)
				if ok, _ := l.Allow(key); !ok {
					t.Fatalf("blocked after only %d failures (FreeAttempts=2)", i+1)
				}
			}
			l.RecordFailure(key)
			ok, retryAfter := l.Allow(key)
			if ok {
				t.Fatal("not blocked after exceeding FreeAttempts")
			}
			if retryAfter != time.Second {
				t.Errorf("retryAfter = %v, want %v", retryAfter, time.Second)
			}
		})
	}
}

func TestBoundedMemoryEvictsLeastRecentlyUsed(t *testing.T) {
	clock := newFakeClock()
	const maxEntries = 8
	l := New(Config{Clock: clock, MaxEntries: maxEntries, FreeAttempts: 0, BaseDelay: time.Second, MaxDelay: time.Minute})

	for i := range maxEntries {
		l.RecordFailure(fmt.Sprintf("key-%02d", i))
	}
	if got := l.len(); got != maxEntries {
		t.Fatalf("after filling to capacity: len = %d, want %d", got, maxEntries)
	}

	for i := 1; i < maxEntries; i++ {
		l.Allow(fmt.Sprintf("key-%02d", i))
	}

	l.RecordFailure("key-new")

	if got := l.len(); got != maxEntries {
		t.Fatalf("after exceeding capacity: len = %d, want %d (bounded)", got, maxEntries)
	}
	if ok, _ := l.Allow("key-00"); !ok {
		t.Error("key-00 reports blocked, but it should have been evicted (its history is gone) and therefore Allow-able")
	}
}

func TestConcurrentUseIsRaceFree(t *testing.T) {
	l := New(Config{Clock: newFakeClock(), FreeAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond})

	keys := []string{"ip:1", "ip:2", "acct:alice", "acct:bob"}
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			key := keys[g%len(keys)]
			for range 50 {
				if ok, _ := l.Allow(key); ok {
					if g%2 == 0 {
						l.RecordFailure(key)
					} else {
						l.RecordSuccess(key)
					}
				}
			}
		}(g)
	}
	wg.Wait()
}
