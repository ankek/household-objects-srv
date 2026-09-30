package sync

import (
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

type selfConflictEnv struct {
	s      *storage.Storage
	commit storage.PushCommitRepository
	scope  storage.Scope
	raw    *sql.DB
}

func newSelfConflictEnv(t *testing.T) selfConflictEnv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db", "hho.db")
	s, err := storage.Open(t.Context(), storage.Config{Path: path, ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	scope := seedGroup(t, s, "groupA")
	commit := pushCommitFor(t, s, "groupA")

	raw, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open inspection handle: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	res, err := Push(t.Context(), commit, PushBatch{Now: 100, Mutations: []PushMutation{{
		MutationID: "mut-create", EntityType: "item", EntityID: "item-x", BaseVersion: 0,
		Fields: pushFields(t, map[string]any{"name": "Orig"}),
	}}})
	if err != nil || len(res.Applied) != 1 || res.Applied[0].Version != 1 {
		t.Fatalf("seed create: res=%+v err=%v, want one applied at version 1", res, err)
	}
	return selfConflictEnv{s: s, commit: commit, scope: scope, raw: raw}
}

func (e selfConflictEnv) push(t *testing.T, now int64, ms ...PushMutation) PushResult {
	t.Helper()
	res, err := Push(t.Context(), e.commit, PushBatch{Mutations: ms, Now: now})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	return res
}

func (e selfConflictEnv) item(t *testing.T) storage.Item {
	t.Helper()
	it, err := e.scope.Items().Get(t.Context(), "item-x")
	if err != nil {
		t.Fatalf("Items().Get: %v", err)
	}
	return it
}

type conflictRow struct{ field, losing, mutationID string }

func (e selfConflictEnv) conflicts(t *testing.T) []conflictRow {
	t.Helper()
	rows, err := e.raw.QueryContext(t.Context(), `SELECT field_name, COALESCE(losing_client_value_snapshot, ''), COALESCE(mutation_id, '')
		FROM conflicts WHERE group_id = 'groupA' AND entity_type = 'item' AND entity_id = 'item-x' ORDER BY created_at, id`)
	if err != nil {
		t.Fatalf("query conflicts: %v", err)
	}
	defer rows.Close()
	var out []conflictRow
	for rows.Next() {
		var r conflictRow
		if err := rows.Scan(&r.field, &r.losing, &r.mutationID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func (e selfConflictEnv) ledgerID(t *testing.T, mutationID string) string {
	t.Helper()
	var id string
	if err := e.raw.QueryRowContext(t.Context(), `SELECT id FROM mutations WHERE group_id = 'groupA' AND mutation_id = ?`, mutationID).Scan(&id); err != nil {
		t.Fatalf("ledger row for %s: %v", mutationID, err)
	}
	return id
}

func mut(t *testing.T, id string, base int64, fields map[string]any) PushMutation {
	t.Helper()
	return PushMutation{MutationID: id, EntityType: "item", EntityID: "item-x", BaseVersion: base, Fields: pushFields(t, fields)}
}

func TestPushSelfConflictRule(t *testing.T) {
	t.Run("separate pushes: M2 conflicts with applied M1, row written, ledger replays M2 as skipped", func(t *testing.T) {
		e := newSelfConflictEnv(t)
		m1 := mut(t, "m1", 1, map[string]any{"name": "First"})
		m2 := mut(t, "m2", 1, map[string]any{"name": "Second"})

		r1 := e.push(t, 200, m1)
		if len(r1.Applied) != 1 || r1.Applied[0].MutationID != "m1" || r1.Applied[0].Version != 2 || len(r1.Conflicts) != 0 {
			t.Fatalf("M1 result = %+v, want applied at version 2, no conflicts", r1)
		}

		r2 := e.push(t, 300, m2)
		if len(r2.Applied) != 0 || len(r2.Skipped) != 0 {
			t.Fatalf("M2 result = %+v, want nothing applied or skipped", r2)
		}
		if len(r2.Conflicts) != 1 || r2.Conflicts[0].MutationID != "m2" || r2.Conflicts[0].FieldName != "name" || r2.Conflicts[0].EntityID != "item-x" {
			t.Fatalf("M2 Conflicts = %+v, want one entry {m2 item item-x name}", r2.Conflicts)
		}

		rows := e.conflicts(t)
		if len(rows) != 1 || rows[0].field != "name" || rows[0].losing != `"Second"` || rows[0].mutationID != e.ledgerID(t, "m2") {
			t.Fatalf("conflicts rows = %+v, want exactly one for name, losing value \"Second\", linked to M2's ledger row", rows)
		}
		if it := e.item(t); it.Name != "First" || it.Version != 2 {
			t.Fatalf("item = name %q version %d, want \"First\" at version 2", it.Name, it.Version)
		}

		replay := e.push(t, 400, m2)
		if len(replay.Skipped) != 1 || replay.Skipped[0].MutationID != "m2" || len(replay.Applied) != 0 || len(replay.Conflicts) != 0 {
			t.Fatalf("M2 replay = %+v, want only Skipped[m2]", replay)
		}
		if replay.NewWatermark != r2.NewWatermark {
			t.Fatalf("replay watermark = %d, want unchanged %d", replay.NewWatermark, r2.NewWatermark)
		}
		if got := e.conflicts(t); len(got) != 1 {
			t.Fatalf("conflicts rows after replay = %+v, want still 1", got)
		}
		if it := e.item(t); it.Name != "First" || it.Version != 2 {
			t.Fatalf("item after replay = name %q version %d, want unchanged", it.Name, it.Version)
		}
	})

	t.Run("same batch: M1 and M2 with the same stale base", func(t *testing.T) {
		e := newSelfConflictEnv(t)
		m1 := mut(t, "m1", 1, map[string]any{"name": "First"})
		m2 := mut(t, "m2", 1, map[string]any{"name": "Second"})

		res := e.push(t, 200, m1, m2)
		if len(res.Applied) != 1 || res.Applied[0].MutationID != "m1" || res.Applied[0].Version != 2 {
			t.Fatalf("Applied = %+v, want only m1 at version 2", res.Applied)
		}
		if len(res.Conflicts) != 1 || res.Conflicts[0].MutationID != "m2" || res.Conflicts[0].FieldName != "name" {
			t.Fatalf("Conflicts = %+v, want one entry for m2 name", res.Conflicts)
		}
		if rows := e.conflicts(t); len(rows) != 1 || rows[0].losing != `"Second"` {
			t.Fatalf("conflicts rows = %+v, want one with losing value \"Second\"", rows)
		}
		if it := e.item(t); it.Name != "First" || it.Version != 2 {
			t.Fatalf("item = name %q version %d, want \"First\" at 2", it.Name, it.Version)
		}

		replay := e.push(t, 400, m1, m2)
		if len(replay.Skipped) != 2 || len(replay.Applied) != 0 || len(replay.Conflicts) != 0 {
			t.Fatalf("batch replay = %+v, want both skipped", replay)
		}
		if got := e.conflicts(t); len(got) != 1 {
			t.Fatalf("conflicts rows after replay = %+v, want still 1", got)
		}
	})

	t.Run("control: M2 rebased onto M1's applied version applies", func(t *testing.T) {
		e := newSelfConflictEnv(t)
		r1 := e.push(t, 200, mut(t, "m1", 1, map[string]any{"name": "First"}))
		if len(r1.Applied) != 1 {
			t.Fatalf("M1 result = %+v, want applied", r1)
		}
		r2 := e.push(t, 300, mut(t, "m2", r1.Applied[0].Version, map[string]any{"name": "Second"}))
		if len(r2.Applied) != 1 || r2.Applied[0].MutationID != "m2" || r2.Applied[0].Version != r1.Applied[0].Version+1 || len(r2.Conflicts) != 0 {
			t.Fatalf("M2 result = %+v, want applied at version %d, no conflicts", r2, r1.Applied[0].Version+1)
		}
		if rows := e.conflicts(t); len(rows) != 0 {
			t.Fatalf("conflicts rows = %+v, want none", rows)
		}
		if it := e.item(t); it.Name != "Second" || it.Version != 3 {
			t.Fatalf("item = name %q version %d, want \"Second\" at 3", it.Name, it.Version)
		}
	})

	t.Run("different fields with the same stale base merge without conflict", func(t *testing.T) {
		e := newSelfConflictEnv(t)
		res := e.push(t, 200,
			mut(t, "m1", 1, map[string]any{"name": "First"}),
			mut(t, "m2", 1, map[string]any{"description": "Desc"}),
		)
		if len(res.Applied) != 2 || len(res.Conflicts) != 0 {
			t.Fatalf("result = %+v, want both applied, no conflicts", res)
		}
		if rows := e.conflicts(t); len(rows) != 0 {
			t.Fatalf("conflicts rows = %+v, want none", rows)
		}
		if it := e.item(t); it.Name != "First" || it.Description != "Desc" || it.Version != 3 {
			t.Fatalf("item = %q/%q v%d, want First/Desc v3", it.Name, it.Description, it.Version)
		}
	})
}
