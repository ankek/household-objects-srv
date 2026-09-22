package backup

import (
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

const (
	syntheticAttachmentFileCount = 8
	syntheticAttachmentFileSize  = 4 << 20
	syntheticDatasetTotalBytes   = syntheticAttachmentFileCount * syntheticAttachmentFileSize

	maxAllowedHeapGrowth = 8 << 20

	gbScaleEnvVar = "HHO_BACKUP_MEMTEST_GB"
)

type patternReader struct {
	remaining int64
	rnd       *rand.Rand
}

func newPatternReader(n int64, seed int64) *patternReader {
	return &patternReader{remaining: n, rnd: rand.New(rand.NewSource(seed))} //nolint:gosec // test fixture content, not a security boundary
}

func (r *patternReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.rnd.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func writeSyntheticAttachmentTree(t *testing.T, dataDir, groupID string, fileCount int, fileSize int64) int64 {
	t.Helper()
	dir := filepath.Join(dataDir, "attachments", groupID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	var total int64
	for i := 0; i < fileCount; i++ {
		name := filepath.Join(dir, "synthetic-"+itoa(i)+".bin")
		f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatalf("create synthetic attachment %s: %v", name, err)
		}
		n, err := io.CopyN(f, newPatternReader(fileSize, int64(i)+1), fileSize)
		closeErr := f.Close()
		if err != nil {
			t.Fatalf("write synthetic attachment %s: %v", name, err)
		}
		if closeErr != nil {
			t.Fatalf("close synthetic attachment %s: %v", name, closeErr)
		}
		if n != fileSize {
			t.Fatalf("wrote %d of %d requested bytes to %s", n, fileSize, name)
		}
		total += n
	}
	return total
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func measurePeakHeapGrowth(t *testing.T, fn func()) uint64 {
	t.Helper()

	runtime.GC()
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)

	var peak atomic.Uint64
	peak.Store(base.HeapAlloc)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				for {
					cur := peak.Load()
					if m.HeapAlloc <= cur || peak.CompareAndSwap(cur, m.HeapAlloc) {
						break
					}
				}
			}
		}
	}()

	fn()
	close(done)

	observed := peak.Load()
	if observed <= base.HeapAlloc {
		return 0
	}
	return observed - base.HeapAlloc
}

func TestWriteArchiveAttachmentStreamingUsesBoundedMemory(t *testing.T) {
	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "group-t118"), "item-t118")

	dataDir := t.TempDir()
	total := writeSyntheticAttachmentTree(t, dataDir, "group-t118", syntheticAttachmentFileCount, syntheticAttachmentFileSize)
	if total != syntheticDatasetTotalBytes {
		t.Fatalf("test fixture invariant broken: wrote %d bytes, want exactly %d", total, syntheticDatasetTotalBytes)
	}

	var stats ArchiveStats
	var writeErr error
	peak := measurePeakHeapGrowth(t, func() {
		stats, writeErr = WriteArchive(t.Context(), s, dataDir, io.Discard)
	})
	if writeErr != nil {
		t.Fatalf("WriteArchive: %v", writeErr)
	}
	if stats.AttachmentFiles != syntheticAttachmentFileCount {
		t.Fatalf("stats.AttachmentFiles = %d, want %d -- the dataset was not fully archived, so this "+
			"run proves nothing about streaming a tree of the intended size", stats.AttachmentFiles, syntheticAttachmentFileCount)
	}

	t.Logf("synthetic attachments tree = %d bytes (%.1f MiB); peak heap growth during WriteArchive = "+
		"%d bytes (%.2f MiB); allowed ceiling = %d bytes (%.0f MiB)",
		total, float64(total)/(1<<20), peak, float64(peak)/(1<<20), uint64(maxAllowedHeapGrowth), float64(maxAllowedHeapGrowth)/(1<<20))

	if peak > uint64(maxAllowedHeapGrowth) {
		t.Errorf("peak heap growth during WriteArchive = %d bytes (%.2f MiB), want <= %d bytes (%.0f MiB) -- "+
			"NFR-020 requires the attachments tree to be streamed, never buffered; this magnitude of "+
			"growth is consistent with the tree (or a large fraction of it) having been materialized "+
			"in memory rather than streamed",
			peak, float64(peak)/(1<<20), uint64(maxAllowedHeapGrowth), float64(maxAllowedHeapGrowth)/(1<<20))
	}
}

func TestWriteArchiveAttachmentStreamingUsesBoundedMemoryAtGBScale(t *testing.T) {
	if os.Getenv(gbScaleEnvVar) == "" {
		t.Skipf("skipping literal GB-scale streaming-memory run: set %s=1 to opt in -- this writes "+
			"and gzips roughly 2 GiB of synthetic attachment data and takes several minutes, so it does "+
			"not run by default and is not part of any CI gate today", gbScaleEnvVar)
	}

	const (
		fileCount = 128
		fileSize  = 16 << 20
	)

	s := newTestStorage(t)
	seedItemInScope(t, newGroupScope(t, s, "group-t118-gb"), "item-t118-gb")

	dataDir := t.TempDir()
	total := writeSyntheticAttachmentTree(t, dataDir, "group-t118-gb", fileCount, fileSize)

	var stats ArchiveStats
	var writeErr error
	peak := measurePeakHeapGrowth(t, func() {
		stats, writeErr = WriteArchive(t.Context(), s, dataDir, io.Discard)
	})
	if writeErr != nil {
		t.Fatalf("WriteArchive: %v", writeErr)
	}
	if stats.AttachmentFiles != fileCount {
		t.Fatalf("stats.AttachmentFiles = %d, want %d", stats.AttachmentFiles, fileCount)
	}

	t.Logf("synthetic attachments tree = %d bytes (%.2f GiB); peak heap growth during WriteArchive = "+
		"%d bytes (%.2f MiB); allowed ceiling = %d bytes (%.0f MiB)",
		total, float64(total)/(1<<30), peak, float64(peak)/(1<<20), uint64(maxAllowedHeapGrowth), float64(maxAllowedHeapGrowth)/(1<<20))

	if peak > uint64(maxAllowedHeapGrowth) {
		t.Errorf("peak heap growth during WriteArchive = %d bytes (%.2f MiB) against a %.2f GiB tree, "+
			"want <= %d bytes (%.0f MiB) -- NFR-020 violation at the literal GB scale it names",
			peak, float64(peak)/(1<<20), float64(total)/(1<<30), uint64(maxAllowedHeapGrowth), float64(maxAllowedHeapGrowth)/(1<<20))
	}
}
