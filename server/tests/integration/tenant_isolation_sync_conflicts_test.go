package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTenantIsolationSyncConflictsDoesNotLeakAcrossGroups(t *testing.T) {
	srv := newLiveServer(t, true)
	a := provisionPushIsoTenant(t, srv, "confl-a")
	b := provisionPushIsoTenant(t, srv, "confl-b")
	requirePushIsoDistinctTenantIDs(t, a, b)

	type marks struct{ winner, loser, winMut, loseMut string }
	mk := func(tn pushIsoTenant) marks {
		return marks{
			winner: tn.name + "-WINNER-value", loser: tn.name + "-LOSER-value",
			winMut: tn.name + "-mut-win", loseMut: tn.name + "-mut-lose",
		}
	}
	ma, mb := mk(a), mk(b)

	produce := func(tn pushIsoTenant, m marks) {
		base := pushIsoGetItem(t, srv, false, tn.cookie, tn.itemID).Version
		resp, _ := pushIsoPushOnce(t, srv, false, tn.cookie, tn.name+"-dev", []pushIsoMutationWire{
			pushIsoItemUpdateMutation(m.winMut, tn.itemID, base, m.winner)})
		if len(resp.Applied) != 1 || len(resp.Conflicts) != 0 {
			t.Fatalf("%s winner push: %+v, want one applied", tn.name, resp)
		}
		resp, _ = pushIsoPushOnce(t, srv, false, tn.cookie, tn.name+"-dev", []pushIsoMutationWire{
			pushIsoItemUpdateMutation(m.loseMut, tn.itemID, base, m.loser)})
		if len(resp.Conflicts) != 1 {
			t.Fatalf("%s stale push: %+v, want exactly one conflict", tn.name, resp)
		}
	}
	produce(a, ma)
	produce(b, mb)

	bItemName := pushIsoGetItem(t, srv, false, b.cookie, b.itemID).Name
	if bItemName == "" {
		t.Fatal("test bug: B's item has no name to search for")
	}

	for _, bearer := range []bool{false, true} {
		kind := map[bool]string{false: "cookie", true: "bearer"}[bearer]
		t.Run("own-log-only/"+kind, func(t *testing.T) {
			for _, c := range []struct {
				own, other pushIsoTenant
				m, om      marks
			}{{a, b, ma, mb}, {b, a, mb, ma}} {
				page, rec := conflictsGetAll(t, srv, bearer, pushIsoCredValue(c.own, bearer), "")
				if len(page) != 1 {
					t.Fatalf("%s's log has %d entries, want exactly 1 (its own): %s", c.own.name, len(page), rec)
				}
				e := page[0]
				if e.EntityID != c.own.itemID || e.EntityType != "item" || e.FieldName != "name" {
					t.Errorf("%s's entry = %+v, want its own item/name conflict", c.own.name, e)
				}
				if e.MutationID == nil || *e.MutationID != c.m.loseMut {
					t.Errorf("%s's entry mutation_id = %v, want the WIRE id %q", c.own.name, e.MutationID, c.m.loseMut)
				}
				if e.ServerValue == nil || *e.ServerValue != `"`+c.m.winner+`"` ||
					e.LosingClientValue == nil || *e.LosingClientValue != `"`+c.m.loser+`"` {
					t.Errorf("%s's entry values = %v / %v, want its own winner/loser", c.own.name, e.ServerValue, e.LosingClientValue)
				}
				for _, needle := range []string{c.other.itemID, c.other.labelID, c.other.groupID, c.om.winner, c.om.loser, c.om.winMut, c.om.loseMut} {
					if strings.Contains(rec, needle) {
						t.Errorf("%s's conflict log leaked %q: %s", c.own.name, needle, rec)
					}
				}
			}
		})
	}

	collideMut := "confl-a-collide-b-item"
	resp, _ := pushIsoPushOnce(t, srv, false, a.cookie, "confl-a-dev", []pushIsoMutationWire{
		pushIsoItemCreateMutation(collideMut, b.itemID, "A-COLLIDER-name")})
	if len(resp.Applied) != 0 || len(resp.Conflicts) != 1 || resp.Conflicts[0].EntityID != b.itemID ||
		resp.Conflicts[0].FieldName != entityConflictFieldWire {
		t.Fatalf("A's create colliding with B's item id: %+v, want one whole-entity conflict and nothing applied", resp)
	}

	for _, bearer := range []bool{false, true} {
		kind := map[bool]string{false: "cookie", true: "bearer"}[bearer]
		t.Run("fu57-collision-never-exposes-other-groups-value/"+kind, func(t *testing.T) {
			page, raw := conflictsGetAll(t, srv, bearer, pushIsoCredValue(a, bearer), "")
			var got *conflictLogEntryWire
			for i := range page {
				if page[i].MutationID != nil && *page[i].MutationID == collideMut {
					got = &page[i]
				}
			}
			if got == nil {
				t.Fatalf("A's log has no entry for its colliding create: %s", raw)
			}
			if got.EntityID != b.itemID || got.FieldName != entityConflictFieldWire {
				t.Errorf("collision entry = %+v, want whole-entity conflict on the colliding id", *got)
			}
			t.Logf("FU-57 evidence: A's collision entry server_value=%s losing_client_value=%s",
				derefOrNull(got.ServerValue), derefOrNull(got.LosingClientValue))
			if got.ServerValue != nil {
				for _, forbidden := range []string{bItemName, mb.winner, mb.loser} {
					if strings.Contains(*got.ServerValue, forbidden) {
						t.Errorf("collision entry's server_value %q exposes B's %q", *got.ServerValue, forbidden)
					}
				}
			}
			for _, forbidden := range []string{bItemName, mb.winner, mb.loser, b.labelID} {
				if strings.Contains(raw, forbidden) {
					t.Errorf("A's log body contains B's data %q: %s", forbidden, raw)
				}
			}

			bPage, bRaw := conflictsGetAll(t, srv, bearer, pushIsoCredValue(b, bearer), "")
			if len(bPage) != 1 {
				t.Errorf("B's log has %d entries after A's collision, want still exactly 1: %s", len(bPage), bRaw)
			}
			for _, needle := range []string{collideMut, "A-COLLIDER-name", got.ID} {
				if strings.Contains(bRaw, needle) {
					t.Errorf("B's log contains A's collision artifact %q: %s", needle, bRaw)
				}
			}
		})
	}

	if it := pushIsoGetItem(t, srv, false, b.cookie, b.itemID); it.Name != bItemName {
		t.Errorf("B's item name = %q after A's collision, want %q", it.Name, bItemName)
	}
}

type conflictLogEntryWire struct {
	ID                string  `json:"id"`
	MutationID        *string `json:"mutation_id"`
	EntityType        string  `json:"entity_type"`
	EntityID          string  `json:"entity_id"`
	FieldName         string  `json:"field_name"`
	ServerValue       *string `json:"server_value"`
	LosingClientValue *string `json:"losing_client_value"`
	DetectedAt        int64   `json:"detected_at"`
}

func derefOrNull(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}

func conflictsGetAll(t *testing.T, srv *liveServer, bearer bool, cred, _ string) ([]conflictLogEntryWire, string) {
	t.Helper()
	var all []conflictLogEntryWire
	var raw strings.Builder
	path := "/api/v1/sync/conflicts?limit=1"
	for i := 0; i < 50; i++ {
		var rec *httptest.ResponseRecorder
		if bearer {
			rec = srv.doBearer(t, http.MethodGet, path, cred, nil)
		} else {
			rec = srv.do(t, http.MethodGet, path, cred, nil)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d: %s", path, rec.Code, rec.Body.String())
		}
		raw.WriteString(rec.Body.String())
		var body struct {
			Conflicts  []conflictLogEntryWire `json:"conflicts"`
			NextCursor *string                `json:"next_cursor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v: %s", err, rec.Body.String())
		}
		all = append(all, body.Conflicts...)
		if body.NextCursor == nil {
			return all, raw.String()
		}
		path = "/api/v1/sync/conflicts?limit=1&after=" + *body.NextCursor
	}
	t.Fatal("conflict log paging did not terminate")
	return nil, ""
}
