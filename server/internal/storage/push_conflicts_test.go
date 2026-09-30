package storage

import (
	"errors"
	"fmt"
	"testing"
)

func seedConflict(t *testing.T, r PushRepository, id, entityID string, detectedAt int64, ledgerRowID string) {
	t.Helper()
	if _, err := r.InsertConflict(t.Context(), InsertConflictParams{
		ID: id, EntityType: "item", EntityID: entityID, FieldName: "name",
		ServerValueSnapshot: `"srv-` + id + `"`, LosingClientValueSnapshot: `"cli-` + id + `"`,
		DetectedAt: detectedAt, MutationID: ledgerRowID, Now: detectedAt,
	}); err != nil {
		t.Fatalf("InsertConflict %s: %v", id, err)
	}
}

func conflictIDs(es []ConflictLogEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

func TestListConflictsIsGroupScoped(t *testing.T) {
	repoA, repoB, _ := pushScope(t)
	seedConflict(t, repoA, "a1", "item-a", 10, "")
	seedConflict(t, repoA, "a2", "item-a", 20, "")
	seedConflict(t, repoB, "b1", "item-b", 15, "")

	pa, err := repoA.ListConflicts(t.Context(), nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := conflictIDs(pa.Entries); fmt.Sprint(got) != "[a2 a1]" {
		t.Errorf("groupA sees %v, want [a2 a1]", got)
	}
	pb, err := repoB.ListConflicts(t.Context(), nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := conflictIDs(pb.Entries); fmt.Sprint(got) != "[b1]" {
		t.Errorf("groupB sees %v, want [b1]", got)
	}
	if pa.Next != nil || pb.Next != nil {
		t.Errorf("Next must be nil when everything fits: %v %v", pa.Next, pb.Next)
	}
}

func TestListConflictsCrossGroupEntityCollision(t *testing.T) {
	repoA, repoB, s := pushScope(t)
	scopeB, err := s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scopeB.Items().Create(t.Context(), CreateItemParams{
		ID: "shared-x", Name: "B-secret-name", ShortCode: "BSEC", Now: 1,
	}); err != nil {
		t.Fatalf("create B item: %v", err)
	}
	seedConflict(t, repoA, "a-collide", "shared-x", 100, "")

	pb, err := repoB.ListConflicts(t.Context(), nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(pb.Entries) != 0 {
		t.Fatalf("groupB sees %v, want none: A's conflict on B's entity id must not leak", conflictIDs(pb.Entries))
	}

	pa, err := repoA.ListConflicts(t.Context(), nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(pa.Entries) != 1 {
		t.Fatalf("groupA sees %d entries, want 1", len(pa.Entries))
	}
	e := pa.Entries[0]
	if e.ServerValue == nil || *e.ServerValue != `"srv-a-collide"` || *e.LosingClientValue != `"cli-a-collide"` {
		t.Errorf("A's entry values = %v/%v, want its own snapshots", e.ServerValue, e.LosingClientValue)
	}
	if e.ServerValue != nil && (*e.ServerValue == "B-secret-name" || *e.ServerValue == `"B-secret-name"`) {
		t.Errorf("server_value resolved from B's live item: %q", *e.ServerValue)
	}
}

func TestListConflictsJoinNeverResolvesAcrossGroups(t *testing.T) {
	repoA, repoB, s := pushScope(t)
	if _, err := repoB.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID: "ledger-b", MutationID: "wire-b", EntityType: "item", EntityID: "x",
		Outcome: MutationOutcomeConflict, Now: 1,
	}); err != nil {
		t.Fatal(err)
	}
	seedConflict(t, repoA, "a-forged", "x", 5, "")
	conn, err := s.store.Writer().Conn(t.Context())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), `UPDATE conflicts SET mutation_id = 'ledger-b' WHERE id = 'a-forged'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}

	pa, err := repoA.ListConflicts(t.Context(), nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pa.Entries) != 1 || pa.Entries[0].MutationID != nil {
		t.Fatalf("A's forged entry = %+v, want one entry with nil mutation_id (B's ledger must not resolve)", pa.Entries)
	}
}

func TestListConflictsMutationIDIsWireIDAndNullWithoutLedgerRow(t *testing.T) {
	repoA, _, _ := pushScope(t)
	if _, err := repoA.Insert(t.Context(), InsertMutationLedgerEntryParams{
		ID: "ledger-row-1", MutationID: "wire-uuid-1", EntityType: "item", EntityID: "i1",
		Outcome: MutationOutcomeConflict, Now: 1,
	}); err != nil {
		t.Fatal(err)
	}
	seedConflict(t, repoA, "with-ledger", "i1", 20, "ledger-row-1")
	seedConflict(t, repoA, "no-ledger", "i1", 10, "")

	p, err := repoA.ListConflicts(t.Context(), nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) != 2 {
		t.Fatalf("got %d entries", len(p.Entries))
	}
	w, n := p.Entries[0], p.Entries[1]
	if w.ID != "with-ledger" || w.MutationID == nil || *w.MutationID != "wire-uuid-1" {
		t.Errorf("with-ledger mutation_id = %v, want wire-uuid-1 (not the ledger row id)", w.MutationID)
	}
	if n.ID != "no-ledger" || n.MutationID != nil {
		t.Errorf("no-ledger mutation_id = %v, want nil", n.MutationID)
	}
}

func TestListConflictsNullSnapshotsAreNil(t *testing.T) {
	repoA, _, _ := pushScope(t)
	if _, err := repoA.InsertConflict(t.Context(), InsertConflictParams{
		ID: "c", EntityType: "item", EntityID: "i", FieldName: "_entity", DetectedAt: 1, Now: 1,
	}); err != nil {
		t.Fatal(err)
	}
	p, err := repoA.ListConflicts(t.Context(), nil, 10)
	if err != nil || len(p.Entries) != 1 {
		t.Fatalf("err=%v entries=%d", err, len(p.Entries))
	}
	if p.Entries[0].ServerValue != nil || p.Entries[0].LosingClientValue != nil {
		t.Errorf("snapshots = %v/%v, want nil/nil", p.Entries[0].ServerValue, p.Entries[0].LosingClientValue)
	}
}

func TestListConflictsKeysetPagination(t *testing.T) {
	repoA, _, _ := pushScope(t)
	seedConflict(t, repoA, "z0", "i", 10, "")
	seedConflict(t, repoA, "b", "i", 50, "")
	seedConflict(t, repoA, "d", "i", 50, "")
	seedConflict(t, repoA, "c", "i", 50, "")
	seedConflict(t, repoA, "a", "i", 40, "")
	seedConflict(t, repoA, "e", "i", 60, "")
	want := []string{"e", "d", "c", "b", "a", "z0"}

	var got []string
	var cur *ConflictCursor
	pages := 0
	for {
		p, err := repoA.ListConflicts(t.Context(), cur, 2)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(p.Entries) > 2 {
			t.Fatalf("limit not honoured: %d entries", len(p.Entries))
		}
		got = append(got, conflictIDs(p.Entries)...)
		if p.Next == nil {
			break
		}
		if len(p.Entries) != 2 {
			t.Fatalf("Next set on a short page")
		}
		dec, err := DecodeConflictCursor(EncodeConflictCursor(*p.Next))
		if err != nil || dec != *p.Next {
			t.Fatalf("cursor round trip: %v %+v vs %+v", err, dec, *p.Next)
		}
		cur = &dec
		if pages > 10 {
			t.Fatal("pagination does not terminate")
		}
		if pages == 1 {
			seedConflict(t, repoA, "newest", "i", 999, "")
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("walk = %v, want %v", got, want)
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3 (exact-multiple final page has Next == nil)", pages)
	}
}

func TestListConflictsLimitBounds(t *testing.T) {
	repoA, _, _ := pushScope(t)
	for _, l := range []int{0, -1, MaxConflictPageLimit + 1} {
		if _, err := repoA.ListConflicts(t.Context(), nil, l); !errors.Is(err, ErrInvalidConflictPage) {
			t.Errorf("limit %d: err = %v, want ErrInvalidConflictPage", l, err)
		}
	}
	if _, err := repoA.ListConflicts(t.Context(), nil, MaxConflictPageLimit); err != nil {
		t.Errorf("limit %d: %v", MaxConflictPageLimit, err)
	}
}

func TestDecodeConflictCursorRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "!!!", "bm9jb2xvbg", EncodeConflictCursor(ConflictCursor{})} {
		if _, err := DecodeConflictCursor(s); !errors.Is(err, ErrInvalidConflictCursor) {
			t.Errorf("Decode(%q) err = %v, want ErrInvalidConflictCursor", s, err)
		}
	}
	c := ConflictCursor{DetectedAt: 7, ID: "a:b:c"}
	if got, err := DecodeConflictCursor(EncodeConflictCursor(c)); err != nil || got != c {
		t.Errorf("colon-bearing id round trip = %+v, %v", got, err)
	}
}
