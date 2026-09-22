package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func touchScheduledBackup(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("not a real archive"), 0o600); err != nil {
		t.Fatalf("write fixture %q: %v", name, err)
	}
}

func scheduledName(y, m, d, hh, mm, ss int) string {
	t := time.Date(y, time.Month(m), d, hh, mm, ss, 0, time.UTC)
	return ScheduledBackupFilename(t)
}

func TestScheduledBackupTimestampRoundTrips(t *testing.T) {
	now := time.Date(2026, 9, 10, 3, 4, 5, 0, time.UTC)
	name := ScheduledBackupFilename(now)
	if name != "hho-scheduled-backup-20260910T030405Z.tar.gz" {
		t.Fatalf("ScheduledBackupFilename = %q, want the documented shape", name)
	}
	ts, ok := scheduledBackupTimestamp(name)
	if !ok {
		t.Fatalf("scheduledBackupTimestamp(%q) ok = false, want true", name)
	}
	if !ts.Equal(now) {
		t.Errorf("scheduledBackupTimestamp(%q) = %v, want %v", name, ts, now)
	}
}

func TestScheduledBackupTimestampRejectsForeignNames(t *testing.T) {
	for _, name := range []string{
		"hho-backup-20260910T030405Z.tar.gz",
		"hho-scheduled-backup-20260910T030405Z.tar",
		"hho-scheduled-backup-not-a-timestamp.tar.gz",
		"my-own-manual-copy.tar.gz",
		"hho-scheduled-backup-20260910T030405Z.tar.gz.bak",
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := scheduledBackupTimestamp(name); ok {
				t.Errorf("scheduledBackupTimestamp(%q) ok = true, want false", name)
			}
		})
	}
}

func TestPruneScheduledBackupsKeepsTheNewestRetainAndRemovesTheRest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		scheduledName(2026, 9, 1, 0, 0, 0),
		scheduledName(2026, 9, 2, 0, 0, 0),
		scheduledName(2026, 9, 3, 0, 0, 0),
		scheduledName(2026, 9, 4, 0, 0, 0),
		scheduledName(2026, 9, 5, 0, 0, 0),
	}
	for _, name := range names {
		touchScheduledBackup(t, dir, name)
	}

	pruned, errs := pruneScheduledBackups(dir, 3)
	if len(errs) != 0 {
		t.Fatalf("pruneScheduledBackups errors = %v, want none", errs)
	}
	if pruned != 2 {
		t.Fatalf("pruned = %d, want 2", pruned)
	}

	for _, name := range names[:2] {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("os.Stat(%s) = %v, want not-exist (should have been pruned)", name, err)
		}
	}
	for _, name := range names[2:] {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("os.Stat(%s) = %v, want the file to survive retention", name, err)
		}
	}
}

func TestPruneScheduledBackupsIsANoOpAtOrUnderRetain(t *testing.T) {
	for _, count := range []int{0, 1, 3} {
		t.Run(t.Name(), func(t *testing.T) {
			dir := t.TempDir()
			var names []string
			for i := 0; i < count; i++ {
				name := scheduledName(2026, 9, i+1, 0, 0, 0)
				names = append(names, name)
				touchScheduledBackup(t, dir, name)
			}

			pruned, errs := pruneScheduledBackups(dir, 3)
			if len(errs) != 0 {
				t.Fatalf("errors = %v, want none", errs)
			}
			if pruned != 0 {
				t.Fatalf("pruned = %d, want 0 (only %d file(s) present, retain=3)", pruned, count)
			}
			for _, name := range names {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Errorf("os.Stat(%s) = %v, want it to survive", name, err)
				}
			}
		})
	}
}

func TestPruneScheduledBackupsNeverTouchesForeignFiles(t *testing.T) {
	dir := t.TempDir()

	foreign := []string{
		"hho-backup-20200101T000000Z.tar.gz",
		"my-manual-copy.tar.gz",
		"README.txt",
	}
	for _, name := range foreign {
		touchScheduledBackup(t, dir, name)
	}

	var own []string
	for i := 1; i <= 5; i++ {
		name := scheduledName(2026, 9, i, 0, 0, 0)
		own = append(own, name)
		touchScheduledBackup(t, dir, name)
	}

	pruned, errs := pruneScheduledBackups(dir, 2)
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
	if pruned != 3 {
		t.Fatalf("pruned = %d, want 3", pruned)
	}

	for _, name := range foreign {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("foreign file %q was touched by retention: os.Stat = %v, want it untouched", name, err)
		}
	}
	for _, name := range own[:3] {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("os.Stat(%s) = %v, want not-exist (should have been pruned)", name, err)
		}
	}
	for _, name := range own[3:] {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("os.Stat(%s) = %v, want it to survive", name, err)
		}
	}
}

func TestPruneScheduledBackupsSkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	subdirName := scheduledName(2026, 1, 1, 0, 0, 0)
	if err := os.MkdirAll(filepath.Join(dir, subdirName), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for i := 2; i <= 4; i++ {
		touchScheduledBackup(t, dir, scheduledName(2026, 1, i, 0, 0, 0))
	}

	pruned, errs := pruneScheduledBackups(dir, 1)
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
	if pruned != 2 {
		t.Fatalf("pruned = %d, want 2", pruned)
	}
	if _, err := os.Stat(filepath.Join(dir, subdirName)); err != nil {
		t.Errorf("subdirectory was removed by retention: os.Stat = %v, want it to survive", err)
	}
}

func TestPruneScheduledBackupsReportsAListFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	pruned, errs := pruneScheduledBackups(missing, 3)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly 1", errs)
	}
}

func TestRunScheduledRejectsNonPositiveRetain(t *testing.T) {
	s := newTestStorage(t)
	dataDir := t.TempDir()

	for _, retain := range []int{0, -1} {
		if _, err := RunScheduled(t.Context(), s, dataDir, retain); err == nil {
			t.Errorf("RunScheduled(retain=%d) succeeded, want an error", retain)
		}
	}

	entries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir backups: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("backups/ has %d entries after a rejected call, want 0", len(entries))
	}
}

func TestRunScheduledWritesAValidArchiveUnderBackups(t *testing.T) {
	s := newTestStorage(t)
	scope := newGroupScope(t, s, "group-a")
	seedItemInScope(t, scope, "item-a")
	dataDir := t.TempDir()
	writeAttachmentFile(t, dataDir, "group-a", "note.txt", []byte("hello from a scheduled backup"))

	result, err := RunScheduled(t.Context(), s, dataDir, 7)
	if err != nil {
		t.Fatalf("RunScheduled: %v", err)
	}
	if result.Path == "" {
		t.Fatal("result.Path is empty")
	}
	wantDir := filepath.Join(dataDir, "backups")
	if filepath.Dir(result.Path) != wantDir {
		t.Errorf("result.Path = %q, want a file directly under %q", result.Path, wantDir)
	}
	if _, ok := scheduledBackupTimestamp(filepath.Base(result.Path)); !ok {
		t.Errorf("result.Path's own filename %q does not parse as one of this feature's own names", filepath.Base(result.Path))
	}
	if result.Stats.AttachmentFiles != 1 {
		t.Errorf("Stats.AttachmentFiles = %d, want 1", result.Stats.AttachmentFiles)
	}
	if result.Pruned != 0 {
		t.Errorf("Pruned = %d, want 0 (only one backup exists)", result.Pruned)
	}

	f, err := os.Open(result.Path)
	if err != nil {
		t.Fatalf("open written archive: %v", err)
	}
	defer func() { _ = f.Close() }()
	entries := readArchiveEntries(t, f)
	if len(entries) != 3 {
		t.Fatalf("archive has %d entries, want 3: %+v", len(entries), entries)
	}
	if entries[0].Name != ManifestEntryName {
		t.Errorf("first entry = %q, want %q", entries[0].Name, ManifestEntryName)
	}

	info, err := os.Stat(result.Path)
	if err != nil {
		t.Fatalf("stat written archive: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("archive mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestRunScheduledPrunesOldBackupsAfterWritingANewOne(t *testing.T) {
	s := newTestStorage(t)
	dataDir := t.TempDir()

	backupsDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}
	touchScheduledBackup(t, backupsDir, "an-operators-own-manual-copy.tar.gz")

	const retain = 2
	var paths []string
	for i := 0; i < 4; i++ {
		result, err := RunScheduled(t.Context(), s, dataDir, retain)
		if err != nil {
			t.Fatalf("RunScheduled #%d: %v", i, err)
		}
		paths = append(paths, result.Path)
		time.Sleep(1100 * time.Millisecond)
	}

	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatalf("ReadDir backups: %v", err)
	}
	var ownCount int
	var foreignSurvived bool
	for _, e := range entries {
		if e.Name() == "an-operators-own-manual-copy.tar.gz" {
			foreignSurvived = true
			continue
		}
		if _, ok := scheduledBackupTimestamp(e.Name()); ok {
			ownCount++
		}
	}
	if !foreignSurvived {
		t.Error("the operator's own manual copy was removed by retention, want it untouched")
	}
	if ownCount != retain {
		t.Errorf("scheduled backups remaining = %d, want exactly %d", ownCount, retain)
	}

	for _, path := range paths[len(paths)-retain:] {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("newest backup %q missing after retention: %v", path, err)
		}
	}
	for _, path := range paths[:len(paths)-retain] {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("os.Stat(%s) = %v, want not-exist (should have been pruned)", path, err)
		}
	}
}
