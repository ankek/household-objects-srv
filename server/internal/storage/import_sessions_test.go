package storage

import (
	"database/sql"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"testing"
)

func importSessionsScope(t *testing.T) (repoA, repoB ImportSessionRepository, s *Storage) {
	t.Helper()
	s = newTestStorage(t)
	seedGroupRow(t, s, "groupA")
	seedGroupRow(t, s, "groupB")

	var err error
	repoA, err = s.ForGroupImportSessions(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroupImportSessions(groupA): %v", err)
	}
	repoB, err = s.ForGroupImportSessions(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroupImportSessions(groupB): %v", err)
	}
	return repoA, repoB, s
}

func TestImportSessionCreateRecordsSourceStatusAndTTL(t *testing.T) {
	repoA, _, _ := importSessionsScope(t)

	const now int64 = 1_000_000
	created, err := repoA.Create(t.Context(), CreateImportSessionParams{
		ID:     "import-1",
		Source: "native",
		Now:    now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID != "import-1" {
		t.Errorf("ID = %q, want %q", created.ID, "import-1")
	}
	if created.GroupID != "groupA" {
		t.Errorf("GroupID = %q, want %q", created.GroupID, "groupA")
	}
	if created.Source != "native" {
		t.Errorf("Source = %q, want %q", created.Source, "native")
	}
	if created.Status != "staged" {
		t.Errorf("Status = %q, want %q", created.Status, "staged")
	}
	if created.CreatedAt != now {
		t.Errorf("CreatedAt = %d, want %d", created.CreatedAt, now)
	}
	wantExpires := now + ImportSessionTTL.Milliseconds()
	if created.ExpiresAt != wantExpires {
		t.Errorf("ExpiresAt = %d, want %d (CreatedAt + ImportSessionTTL)", created.ExpiresAt, wantExpires)
	}
}

func TestImportSessionCreateRejectsAnEmptySource(t *testing.T) {
	repoA, _, _ := importSessionsScope(t)

	if _, err := repoA.Create(t.Context(), CreateImportSessionParams{ID: "import-1", Now: 1}); err == nil {
		t.Fatal("Create with an empty Source succeeded, want an error")
	}
}

func TestImportSessionCreateIsInvisibleToAnotherGroup(t *testing.T) {
	repoA, _, s := importSessionsScope(t)

	created, err := repoA.Create(t.Context(), CreateImportSessionParams{
		ID:     "import-cross-tenant",
		Source: "native",
		Now:    1,
	})
	if err != nil {
		t.Fatalf("Create (groupA): %v", err)
	}

	q := gen.New(s.store.Reader())

	rowA, err := q.GetImportSession(t.Context(), gen.GetImportSessionParams{GroupID: "groupA", ID: created.ID})
	if err != nil {
		t.Fatalf("GetImportSession(groupA, %q): %v", created.ID, err)
	}
	if rowA.ID != created.ID {
		t.Fatalf("GetImportSession(groupA) returned id %q, want %q", rowA.ID, created.ID)
	}

	_, err = q.GetImportSession(t.Context(), gen.GetImportSessionParams{GroupID: "groupB", ID: created.ID})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetImportSession(groupB, %q) error = %v, want sql.ErrNoRows; a session created for "+
			"groupA must be invisible to groupB (P-3)", created.ID, err)
	}
}

func TestImportSessionGetReturnsTheCreatedRow(t *testing.T) {
	repoA, _, _ := importSessionsScope(t)

	created, err := repoA.Create(t.Context(), CreateImportSessionParams{ID: "import-1", Source: "native", Now: 1000})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repoA.Get(t.Context(), "import-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != created {
		t.Errorf("Get = %+v, want %+v", got, created)
	}
}

func TestImportSessionGetIsNotFoundForAnUnknownID(t *testing.T) {
	repoA, _, _ := importSessionsScope(t)

	if _, err := repoA.Get(t.Context(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestImportSessionGetIsNotFoundAcrossGroups(t *testing.T) {
	repoA, repoB, _ := importSessionsScope(t)

	created, err := repoA.Create(t.Context(), CreateImportSessionParams{ID: "import-cross", Source: "native", Now: 1})
	if err != nil {
		t.Fatalf("Create (groupA): %v", err)
	}

	if _, err := repoB.Get(t.Context(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Get(%q) error = %v, want ErrNotFound; a session created for groupA must be "+
			"invisible to groupB (P-3)", created.ID, err)
	}
}

func TestImportSessionDeleteRemovesTheRow(t *testing.T) {
	repoA, _, _ := importSessionsScope(t)

	created, err := repoA.Create(t.Context(), CreateImportSessionParams{ID: "import-1", Source: "native", Now: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repoA.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repoA.Get(t.Context(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrNotFound", err)
	}
}

func TestImportSessionDeleteIsIdempotentWhenAlreadyGone(t *testing.T) {
	repoA, _, _ := importSessionsScope(t)

	if err := repoA.Delete(t.Context(), "never-existed"); err != nil {
		t.Fatalf("Delete(never-existed) = %v, want nil (deleting an absent row is not an error)", err)
	}
}

func TestImportSessionDeleteDoesNotAffectAnotherGroup(t *testing.T) {
	repoA, repoB, _ := importSessionsScope(t)

	created, err := repoA.Create(t.Context(), CreateImportSessionParams{ID: "import-cross", Source: "native", Now: 1})
	if err != nil {
		t.Fatalf("Create (groupA): %v", err)
	}

	if err := repoB.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("groupB Delete(%q) = %v, want nil (no matching row in groupB)", created.ID, err)
	}
	if _, err := repoA.Get(t.Context(), created.ID); err != nil {
		t.Fatalf("groupA Get(%q) after groupB's Delete = %v, want nil (groupB must not affect groupA's row, P-3)", created.ID, err)
	}
}
