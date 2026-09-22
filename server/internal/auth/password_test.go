package auth

import (
	"context"
	"errors"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"
)

func cheapParams() Params {
	p := DefaultParams()
	p.MemoryKiB = 64
	p.Time = 1
	return p
}

func cheapHasher(t *testing.T) *Hasher {
	t.Helper()
	h, err := NewHasher(Config{Params: cheapParams(), Reclaim: RetainOSMemory})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	return h
}

func mustHash(t *testing.T, h *Hasher, password string) string {
	t.Helper()
	encoded, err := h.Hash(context.Background(), []byte(password))
	if err != nil {
		t.Fatalf("Hash(%q): %v", password, err)
	}
	return encoded
}

func TestHashVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	h := cheapHasher(t)
	for _, password := range []string{
		"correct horse battery staple",
		"",
		"pässwörd with ünicode",
		strings.Repeat("x", 4096),
		"\x00embedded\x00nul\x00",
	} {
		encoded := mustHash(t, h, password)
		if err := h.Verify(context.Background(), encoded, []byte(password)); err != nil {
			t.Errorf("Verify of %q round-trip: got %v, want nil", password, err)
		}
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	t.Parallel()
	h := cheapHasher(t)
	encoded := mustHash(t, h, "correct horse battery staple")
	for _, wrong := range []string{
		"Correct horse battery staple",
		"correct horse battery stapl",
		"correct horse battery staple ",
		"",
	} {
		if err := h.Verify(context.Background(), encoded, []byte(wrong)); !errors.Is(err, ErrMismatch) {
			t.Errorf("Verify with wrong password %q: got %v, want ErrMismatch", wrong, err)
		}
	}
}

func TestVerifyRejectsUnreadableHashWithoutPanicking(t *testing.T) {
	t.Parallel()
	h := cheapHasher(t)
	valid := mustHash(t, h, "pw")

	cases := map[string]string{
		"empty":              "",
		"garbage":            "not a hash at all",
		"bcrypt":             "$2y$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
		"no leading dollar":  strings.TrimPrefix(valid, "$"),
		"truncated at salt":  valid[:strings.LastIndex(valid, "$")],
		"truncated mid":      valid[:len(valid)/2],
		"one field too many": valid + "$extra",
		"argon2i":            strings.Replace(valid, "argon2id", "argon2i", 1),
		"argon2d":            strings.Replace(valid, "argon2id", "argon2d", 1),
		"old version":        strings.Replace(valid, "v=19", "v=16", 1),
		"missing version":    strings.Replace(valid, "v=19", "19", 1),
		"reordered params":   strings.Replace(valid, "m=64,t=1,p=1", "t=1,m=64,p=1", 1),
		"missing p":          strings.Replace(valid, "m=64,t=1,p=1", "m=64,t=1", 1),
		"negative t":         strings.Replace(valid, "t=1", "t=-1", 1),
		"zero t":             strings.Replace(valid, "t=1", "t=0", 1),
		"zero p":             strings.Replace(valid, "p=1", "p=0", 1),
		"absurd m":           strings.Replace(valid, "m=64", "m=4194304", 1),
		"overflowing m":      strings.Replace(valid, "m=64", "m=99999999999", 1),
		"absurd t":           strings.Replace(valid, "t=1", "t=1000000", 1),
		"padded base64":      strings.Replace(valid, "$m=", "=$m=", 1),
		"non-base64":         valid[:len(valid)-4] + "!!!!",
		"empty salt":         strings.Replace(valid, "$"+strings.Split(valid, "$")[4]+"$", "$$", 1),
		"nul bytes only":     "\x00\x00\x00\x00\x00",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			err := h.Verify(context.Background(), encoded, []byte("pw"))
			if err == nil {
				t.Fatalf("Verify(%q) accepted an unreadable hash", encoded)
			}
			if !errors.Is(err, ErrMalformedHash) && !errors.Is(err, ErrMismatch) {
				t.Fatalf("Verify(%q): got %v, want ErrMalformedHash or ErrMismatch", encoded, err)
			}
			if !h.NeedsRehash(encoded) {
				t.Errorf("NeedsRehash(%q) = false; an unreadable hash always needs replacing", encoded)
			}
		})
	}
}

func TestTamperedHashIsRejected(t *testing.T) {
	t.Parallel()
	h := cheapHasher(t)
	encoded := mustHash(t, h, "pw")
	parts := strings.Split(encoded, "$")

	flip := func(field int) string {
		f := []byte(parts[field])
		if f[0] == 'A' {
			f[0] = 'B'
		} else {
			f[0] = 'A'
		}
		out := append([]string(nil), parts...)
		out[field] = string(f)
		return strings.Join(out, "$")
	}

	for name, tampered := range map[string]string{"salt": flip(4), "digest": flip(5)} {
		if err := h.Verify(context.Background(), tampered, []byte("pw")); !errors.Is(err, ErrMismatch) {
			t.Errorf("Verify with tampered %s: got %v, want ErrMismatch", name, err)
		}
	}
}

func TestSaltIsUniquePerHash(t *testing.T) {
	t.Parallel()
	h := cheapHasher(t)
	const n = 64
	salts := make(map[string]bool, n)
	encodings := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		encoded := mustHash(t, h, "the same password every time")
		d, err := decode(encoded)
		if err != nil {
			t.Fatalf("decode of our own output: %v", err)
		}
		if len(d.salt) != DefaultSaltLength {
			t.Fatalf("salt is %d bytes, want %d", len(d.salt), DefaultSaltLength)
		}
		if salts[string(d.salt)] {
			t.Fatalf("salt repeated within %d hashes", n)
		}
		if encodings[encoded] {
			t.Fatalf("identical encoded hash produced twice for the same password")
		}
		salts[string(d.salt)] = true
		encodings[encoded] = true
	}
}

func TestEncodingRoundTripsParameters(t *testing.T) {
	t.Parallel()
	for _, p := range []Params{
		cheapParams(),
		{MemoryKiB: 8, Time: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16},
		{MemoryKiB: 1024, Time: 3, Parallelism: 2, SaltLength: 32, KeyLength: 64},
		{MemoryKiB: 65536, Time: 64, Parallelism: 8, SaltLength: 64, KeyLength: 32},
	} {
		encoded := p.encode(make([]byte, p.SaltLength), make([]byte, p.KeyLength))
		d, err := decode(encoded)
		if err != nil {
			t.Fatalf("decode(%q): %v", encoded, err)
		}
		if d.params != p {
			t.Errorf("parameters did not survive the encoding: got %+v, want %+v (via %q)", d.params, p, encoded)
		}
	}
}

func TestEncodedFormIsPHC(t *testing.T) {
	t.Parallel()
	p := Params{MemoryKiB: 12288, Time: 4, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	got := p.encode(make([]byte, 16), make([]byte, 32))
	const want = "$argon2id$v=19$m=12288,t=4,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if got != want {
		t.Errorf("encoded form changed:\n got %q\nwant %q", got, want)
	}
}

func TestVerifyUsesTheStoredParameters(t *testing.T) {
	t.Parallel()
	old, err := NewHasher(Config{
		Params:  Params{MemoryKiB: 64, Time: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16},
		Reclaim: RetainOSMemory,
	})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	encoded := mustHash(t, old, "pw")

	current := cheapHasher(t)
	if current.Params() == old.Params() {
		t.Fatal("test is vacuous: both hashers use the same profile")
	}
	if err := current.Verify(context.Background(), encoded, []byte("pw")); err != nil {
		t.Errorf("Verify of a hash written under an older profile: got %v, want nil", err)
	}
	if err := current.Verify(context.Background(), encoded, []byte("wrong")); !errors.Is(err, ErrMismatch) {
		t.Errorf("Verify of an older-profile hash with the wrong password: got %v, want ErrMismatch", err)
	}
}

func TestNeedsRehash(t *testing.T) {
	t.Parallel()
	h := cheapHasher(t)
	current := h.Params()

	if encoded := mustHash(t, h, "pw"); h.NeedsRehash(encoded) {
		t.Error("NeedsRehash of a hash this hasher just wrote = true, want false")
	}

	for name, mutate := range map[string]func(Params) Params{
		"memory":      func(p Params) Params { p.MemoryKiB *= 2; return p },
		"time":        func(p Params) Params { p.Time++; return p },
		"parallelism": func(p Params) Params { p.Parallelism++; return p },
		"salt length": func(p Params) Params { p.SaltLength += 8; return p },
		"key length":  func(p Params) Params { p.KeyLength += 16; return p },
	} {
		p := mutate(current)
		encoded := p.encode(make([]byte, p.SaltLength), make([]byte, p.KeyLength))
		if !h.NeedsRehash(encoded) {
			t.Errorf("NeedsRehash of a hash differing in %s = false, want true", name)
		}
	}

	weaker := current
	weaker.MemoryKiB /= 2
	weakEncoded := weaker.encode(make([]byte, weaker.SaltLength), make([]byte, weaker.KeyLength))
	if !h.NeedsRehash(weakEncoded) {
		t.Error("NeedsRehash of a weaker profile = false, want true")
	}

	stale, err := NewHasher(Config{
		Params:  Params{MemoryKiB: 64, Time: 2, Parallelism: 1, SaltLength: 8, KeyLength: 16},
		Reclaim: RetainOSMemory,
	})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	staleEncoded := mustHash(t, stale, "pw")
	if err := h.Verify(context.Background(), staleEncoded, []byte("pw")); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !h.NeedsRehash(staleEncoded) {
		t.Fatal("NeedsRehash of a stale-profile hash = false, want true")
	}
	fresh := mustHash(t, h, "pw")
	if h.NeedsRehash(fresh) {
		t.Error("NeedsRehash of the replacement = true, want false")
	}
	if err := h.Verify(context.Background(), fresh, []byte("pw")); err != nil {
		t.Errorf("Verify of the replacement: %v", err)
	}
}

func TestVerifyAbsentAlwaysMismatches(t *testing.T) {
	t.Parallel()
	h, err := NewHasher(Config{
		Params:  Params{MemoryKiB: 2048, Time: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32},
		Reclaim: RetainOSMemory,
	})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	encoded := mustHash(t, h, "pw")

	fastest := func(fn func()) time.Duration {
		best := time.Hour
		for i := 0; i < 5; i++ {
			start := time.Now()
			fn()
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}

	var absentErr error
	absent := fastest(func() { absentErr = h.VerifyAbsent(context.Background(), []byte("pw")) })
	if !errors.Is(absentErr, ErrMismatch) {
		t.Fatalf("VerifyAbsent: got %v, want ErrMismatch", absentErr)
	}
	real := fastest(func() { _ = h.Verify(context.Background(), encoded, []byte("wrong")) })

	t.Logf("VerifyAbsent %v vs Verify-with-wrong-password %v", absent, real)
	if absent < real/4 {
		t.Errorf("VerifyAbsent took %v against Verify's %v: it is not spending the work that closes "+
			"the username-enumeration oracle", absent, real)
	}
}

func TestContextCancellationIsHonoured(t *testing.T) {
	t.Parallel()
	h := cheapHasher(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Hash(ctx, []byte("pw")); !errors.Is(err, context.Canceled) {
		t.Errorf("Hash with a cancelled context: got %v, want context.Canceled", err)
	}
	encoded := mustHash(t, h, "pw")
	if err := h.Verify(ctx, encoded, []byte("pw")); !errors.Is(err, context.Canceled) {
		t.Errorf("Verify with a cancelled context: got %v, want context.Canceled", err)
	}
	if err := h.VerifyAbsent(ctx, []byte("pw")); !errors.Is(err, context.Canceled) {
		t.Errorf("VerifyAbsent with a cancelled context: got %v, want context.Canceled", err)
	}
}

func TestSaturatedHasherWaitsAndThenGivesUp(t *testing.T) {
	t.Parallel()
	h := cheapHasher(t)
	if cap(h.sem) != DefaultMaxInFlight {
		t.Fatalf("hasher admits %d concurrent hashes, want %d; the memory arithmetic in "+
			"DefaultParams has room for one", cap(h.sem), DefaultMaxInFlight)
	}

	h.sem <- struct{}{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := h.Hash(ctx, []byte("pw"))
	waited := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Hash against a saturated hasher: got %v, want context.DeadlineExceeded", err)
	}
	if waited < 40*time.Millisecond {
		t.Errorf("Hash gave up after %v; it must wait for a slot, not fail fast", waited)
	}

	<-h.sem
	if _, err := h.Hash(context.Background(), []byte("pw")); err != nil {
		t.Errorf("Hash after the slot freed: %v", err)
	}
}

func TestNewHasherRejectsUnusableConfigurations(t *testing.T) {
	t.Parallel()

	h, err := NewHasher(Config{})
	if err != nil {
		t.Fatalf("NewHasher(Config{}) must be the production configuration: %v", err)
	}
	if got := h.Params(); got != DefaultParams() {
		t.Errorf("zero Config gave %+v, want %+v", got, DefaultParams())
	}
	if cap(h.sem) != DefaultMaxInFlight {
		t.Errorf("zero Config gave MaxInFlight %d, want %d", cap(h.sem), DefaultMaxInFlight)
	}

	for name, cfg := range map[string]Config{
		"absurd memory":   {Params: Params{MemoryKiB: 1 << 30, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}},
		"zero time":       {Params: Params{MemoryKiB: 64, Time: 0, Parallelism: 1, SaltLength: 16, KeyLength: 32}},
		"zero lanes":      {Params: Params{MemoryKiB: 64, Time: 1, Parallelism: 0, SaltLength: 16, KeyLength: 32}},
		"short salt":      {Params: Params{MemoryKiB: 64, Time: 1, Parallelism: 1, SaltLength: 4, KeyLength: 32}},
		"short digest":    {Params: Params{MemoryKiB: 64, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 4}},
		"partial params":  {Params: Params{MemoryKiB: 12288}},
		"negative slots":  {MaxInFlight: -1},
		"unknown reclaim": {Reclaim: ReclaimPolicy(9)},
	} {
		if _, err := NewHasher(cfg); err == nil {
			t.Errorf("NewHasher(%s) accepted an unusable configuration", name)
		}
	}
}

func TestZero(t *testing.T) {
	t.Parallel()
	b := []byte("hunter2")
	Zero(b)
	for i, c := range b {
		if c != 0 {
			t.Fatalf("byte %d is %#x after Zero", i, c)
		}
	}
}

func TestMeasuredCostOfDefaultProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates 12 MiB per hash")
	}
	h, err := NewHasher(Config{})
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}

	debug.FreeOSMemory()
	before := residentKiB(t)

	const n = 5
	best, total := time.Hour, time.Duration(0)
	var encoded string
	for i := 0; i < n; i++ {
		start := time.Now()
		encoded = mustHash(t, h, "correct horse battery staple")
		d := time.Since(start)
		total += d
		if d < best {
			best = d
		}
	}
	after := residentKiB(t)

	t.Logf("argon2id m=%d KiB t=%d p=%d: fastest %v, mean %v per hash (including the "+
		"debug.FreeOSMemory reclaim); resident set %d KiB -> %d KiB across %d hashes",
		h.Params().MemoryKiB, h.Params().Time, h.Params().Parallelism, best, total/n, before, after, n)

	if err := h.Verify(context.Background(), encoded, []byte("correct horse battery staple")); err != nil {
		t.Fatalf("Verify at the default profile: %v", err)
	}
	if best > 5*time.Second {
		t.Errorf("a single hash took %v; the profile is far outside what an interactive login can "+
			"spend and NFR-002's latency budget cannot absorb it", best)
	}
}

func residentKiB(t *testing.T) int {
	t.Helper()
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			if kib, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(rest), " kB")); err == nil {
				return kib
			}
		}
	}
	return 0
}

func BenchmarkHash(b *testing.B) {
	h, err := NewHasher(Config{})
	if err != nil {
		b.Fatalf("NewHasher: %v", err)
	}
	password := []byte("correct horse battery staple")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := h.Hash(context.Background(), password); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashWithoutReclaim(b *testing.B) {
	h, err := NewHasher(Config{Reclaim: RetainOSMemory})
	if err != nil {
		b.Fatalf("NewHasher: %v", err)
	}
	password := []byte("correct horse battery staple")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := h.Hash(context.Background(), password); err != nil {
			b.Fatal(err)
		}
	}
}
