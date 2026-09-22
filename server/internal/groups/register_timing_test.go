package groups

import (
	"crypto/rand"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestReopenedRegistrationCostsOneHashNotTwo(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the production argon2id profile: 12 MiB and ~28-40ms per hash")
	}

	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}

	path := filepath.Join(t.TempDir(), "db", "hho.db")
	store, err := storage.Open(t.Context(), storage.Config{Path: path})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	svc, err := NewService(store, hasher)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	gate, err := NewRegistrationGate(svc, true)
	if err != nil {
		t.Fatalf("NewRegistrationGate: %v", err)
	}

	if _, err := gate.Register(t.Context(), RegisterRequest{
		Username: "founder", Password: []byte("correct horse battery staple"),
	}); err != nil {
		t.Fatalf("founding Register: %v", err)
	}

	hashes, restore := countSaltDraws()
	defer restore()

	start := time.Now()
	if _, err := gate.Register(t.Context(), RegisterRequest{
		Username: "racer", Password: []byte("correct horse battery staple"),
	}); err != nil {
		t.Fatalf("re-opened Register: %v", err)
	}
	elapsed := time.Since(start)

	got := hashes.Load()
	t.Logf("re-opened registration performed %d hash(es) in %v", got, elapsed)

	if got != 1 {
		t.Errorf("re-opened registration performed %d hash(es) (want exactly 1) -- this is the "+
			"double-hash regression T036 fixed: Service.Register is hashing before finding out "+
			"registration is closed, so registerNewGroup's own hash is a second, wasted one", got)
	}
}

func countSaltDraws() (count *atomic.Int64, restore func()) {
	count = new(atomic.Int64)
	original := rand.Reader
	rand.Reader = saltCountingReader{inner: original, count: count}
	return count, func() { rand.Reader = original }
}

type saltCountingReader struct {
	inner io.Reader
	count *atomic.Int64
}

func (r saltCountingReader) Read(p []byte) (int, error) {
	if len(p) == auth.DefaultSaltLength {
		r.count.Add(1)
	}
	return r.inner.Read(p)
}
