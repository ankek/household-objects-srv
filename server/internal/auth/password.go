package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"runtime"
	"runtime/debug"
)

var ErrMismatch = errors.New("auth: password does not match")

const (
	DefaultMemoryKiB = 12 * 1024

	DefaultTime = 4

	DefaultParallelism = 1

	DefaultSaltLength = 16

	DefaultKeyLength = 32

	DefaultMaxInFlight = 1
)

type Params struct {
	MemoryKiB uint32

	Time uint32

	Parallelism uint8

	SaltLength int

	KeyLength int
}

func DefaultParams() Params {
	return Params{
		MemoryKiB:   DefaultMemoryKiB,
		Time:        DefaultTime,
		Parallelism: DefaultParallelism,
		SaltLength:  DefaultSaltLength,
		KeyLength:   DefaultKeyLength,
	}
}

func (p Params) Validate() error {
	if err := p.checkLimits(); err != nil {
		return fmt.Errorf("auth: unusable argon2id parameters: %w", err)
	}
	return nil
}

type ReclaimPolicy uint8

const (
	ReclaimOSMemory ReclaimPolicy = iota

	RetainOSMemory
)

type Config struct {
	Params Params

	MaxInFlight int

	Reclaim ReclaimPolicy
}

type Hasher struct {
	params  Params
	sem     chan struct{}
	reclaim ReclaimPolicy
}

func NewHasher(cfg Config) (*Hasher, error) {
	params := cfg.Params
	if params == (Params{}) {
		params = DefaultParams()
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}
	maxInFlight := cfg.MaxInFlight
	if maxInFlight == 0 {
		maxInFlight = DefaultMaxInFlight
	}
	if maxInFlight < 0 {
		return nil, fmt.Errorf("auth: MaxInFlight is %d; a hasher that admits no work would fail every login", maxInFlight)
	}
	switch cfg.Reclaim {
	case ReclaimOSMemory, RetainOSMemory:
	default:
		return nil, fmt.Errorf("auth: unknown ReclaimPolicy %d", cfg.Reclaim)
	}
	return &Hasher{
		params:  params,
		sem:     make(chan struct{}, maxInFlight),
		reclaim: cfg.Reclaim,
	}, nil
}

func (h *Hasher) Params() Params { return h.params }

func (h *Hasher) Hash(ctx context.Context, password []byte) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: cannot draw a salt: %w", err)
	}

	key, err := h.derive(ctx, password, salt, h.params)
	if err != nil {
		return "", err
	}
	return h.params.encode(salt, key), nil
}

func (h *Hasher) Verify(ctx context.Context, encoded string, password []byte) error {
	d, err := decode(encoded)
	if err != nil {
		return err
	}
	key, err := h.derive(ctx, password, d.salt, d.params)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(key, d.key) != 1 {
		return ErrMismatch
	}
	return nil
}

func (h *Hasher) VerifyAbsent(ctx context.Context, password []byte) error {
	decoy := make([]byte, h.params.SaltLength+h.params.KeyLength)
	if _, err := rand.Read(decoy); err != nil {
		return fmt.Errorf("auth: cannot draw a decoy salt: %w", err)
	}
	salt, want := decoy[:h.params.SaltLength], decoy[h.params.SaltLength:]

	key, err := h.derive(ctx, password, salt, h.params)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(key, want) == 1 {
		return nil
	}
	return ErrMismatch
}

func (h *Hasher) NeedsRehash(encoded string) bool {
	d, err := decode(encoded)
	if err != nil {
		return true
	}
	return d.params != h.params
}

func (h *Hasher) derive(ctx context.Context, password, salt []byte, p Params) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case h.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() {
		if h.reclaim == ReclaimOSMemory {
			debug.FreeOSMemory()
		}
		<-h.sem
	}()

	if err := p.withinDecodeLimits(); err != nil {
		return nil, err
	}
	return argon2.IDKey(password, salt, p.Time, p.MemoryKiB, p.Parallelism, uint32(p.KeyLength)), nil
}

func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(b)
}
