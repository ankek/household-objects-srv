package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

const (
	DefaultMaxEntries = 4096

	DefaultFreeAttempts = 5

	DefaultBaseDelay = time.Second

	DefaultMaxDelay = 30 * time.Second
)

type Config struct {
	Clock        Clock
	MaxEntries   int
	FreeAttempts int
	BaseDelay    time.Duration
	MaxDelay     time.Duration
}

type entry struct {
	key          string
	failures     int
	blockedUntil time.Time
	elem         *list.Element
}

type Limiter struct {
	mu sync.Mutex

	clock        Clock
	maxEntries   int
	freeAttempts int
	base         time.Duration
	max          time.Duration

	entries map[string]*entry
	order   *list.List
}

func New(cfg Config) *Limiter {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	maxEntries := cfg.MaxEntries
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	free := cfg.FreeAttempts
	if free <= 0 {
		free = DefaultFreeAttempts
	}
	base := cfg.BaseDelay
	if base <= 0 {
		base = DefaultBaseDelay
	}
	maxDelay := cfg.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}
	return &Limiter{
		clock:        clock,
		maxEntries:   maxEntries,
		freeAttempts: free,
		base:         base,
		max:          maxDelay,
		entries:      make(map[string]*entry),
		order:        list.New(),
	}
}

func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, found := l.entries[key]
	if !found {
		return true, 0
	}
	l.order.MoveToFront(e.elem)

	now := l.clock.Now()
	if now.Before(e.blockedUntil) {
		return false, e.blockedUntil.Sub(now)
	}
	return true, 0
}

func (l *Limiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, found := l.entries[key]
	if !found {
		e = l.insertLocked(key)
	} else {
		l.order.MoveToFront(e.elem)
	}

	const failureCeiling = 1 << 30
	if e.failures < failureCeiling {
		e.failures++
	}

	if e.failures <= l.freeAttempts {
		return
	}

	delay := l.base
	for shift := e.failures - l.freeAttempts - 1; shift > 0 && delay < l.max; shift-- {
		delay *= 2
	}
	if delay > l.max || delay <= 0 {
		delay = l.max
	}
	e.blockedUntil = l.clock.Now().Add(delay)
}

func (l *Limiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, found := l.entries[key]
	if !found {
		return
	}
	l.order.Remove(e.elem)
	delete(l.entries, key)
}

func (l *Limiter) insertLocked(key string) *entry {
	if len(l.entries) >= l.maxEntries {
		if back := l.order.Back(); back != nil {
			evicted, _ := back.Value.(*entry)
			l.order.Remove(back)
			if evicted != nil {
				delete(l.entries, evicted.key)
			}
		}
	}
	e := &entry{key: key}
	e.elem = l.order.PushFront(e)
	l.entries[key] = e
	return e
}

func (l *Limiter) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
