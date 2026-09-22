package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTenantIsolationSyncPushDoesNotLeakAcrossGroups(t *testing.T) {
	srv := newLiveServer(t, true)
	a := provisionPushIsoTenant(t, srv, "push-iso-alice")
	b := provisionPushIsoTenant(t, srv, "push-iso-brenda")
	requirePushIsoDistinctTenantIDs(t, a, b)

	credentials := []struct {
		kind   string
		bearer bool
	}{
		{"cookie", false},
		{"bearer", true},
	}

	for _, cred := range credentials {
		t.Run("own_pushes_applied_and_visible_only_to_own_group/"+cred.kind, func(t *testing.T) {
			aEntityID := "push-iso-a-own-item-" + cred.kind
			bEntityID := "push-iso-b-own-item-" + cred.kind

			bWatermarkBefore := pushIsoWatermark(t, srv, cred.bearer, pushIsoCredValue(b, cred.bearer), "dev-b-wm-before-"+cred.kind)

			respA, _ := pushIsoPushOnce(t, srv, cred.bearer, pushIsoCredValue(a, cred.bearer), "dev-a-own-"+cred.kind, []pushIsoMutationWire{
				pushIsoItemCreateMutation("mut-a-own-"+cred.kind, aEntityID, "A's own pushed item "+cred.kind),
			})
			if len(respA.Applied) != 1 || respA.Applied[0].EntityID != aEntityID {
				t.Fatalf("A's own create response = %+v, want exactly one applied entry for entity_id %q", respA, aEntityID)
			}
			if len(respA.Conflicts) != 0 {
				t.Fatalf("A's own create conflicted: %+v, want none", respA.Conflicts)
			}

			bWatermarkAfterAPushed := pushIsoWatermark(t, srv, cred.bearer, pushIsoCredValue(b, cred.bearer), "dev-b-wm-after-"+cred.kind)
			if bWatermarkAfterAPushed != bWatermarkBefore {
				t.Fatalf("B's own watermark moved from %d to %d after ONLY A pushed -- push's watermark read is not scoped per group",
					bWatermarkBefore, bWatermarkAfterAPushed)
			}

			respB, _ := pushIsoPushOnce(t, srv, cred.bearer, pushIsoCredValue(b, cred.bearer), "dev-b-own-"+cred.kind, []pushIsoMutationWire{
				pushIsoItemCreateMutation("mut-b-own-"+cred.kind, bEntityID, "B's own pushed item "+cred.kind),
			})
			if len(respB.Applied) != 1 || respB.Applied[0].EntityID != bEntityID {
				t.Fatalf("B's own create response = %+v, want exactly one applied entry for entity_id %q", respB, bEntityID)
			}

			changesA, _ := syncIsoPullAllPages(t, srv, cred.bearer, pushIsoCredValue(a, cred.bearer), "dev-a-pull-"+cred.kind, 0, 500)
			changesB, _ := syncIsoPullAllPages(t, srv, cred.bearer, pushIsoCredValue(b, cred.bearer), "dev-b-pull-"+cred.kind, 0, 500)

			if !pushIsoChangesContainItemID(changesA, aEntityID) {
				t.Errorf("A's own pull is missing the item A itself just pushed (id %q)", aEntityID)
			}
			if pushIsoChangesContainItemID(changesA, bEntityID) {
				t.Errorf("A's pull LEAKED B's pushed item (id %q) across groups", bEntityID)
			}
			if !pushIsoChangesContainItemID(changesB, bEntityID) {
				t.Errorf("B's own pull is missing the item B itself just pushed (id %q)", bEntityID)
			}
			if pushIsoChangesContainItemID(changesB, aEntityID) {
				t.Errorf("B's pull LEAKED A's pushed item (id %q) across groups", aEntityID)
			}
		})
	}

	if rec := srv.do(t, http.MethodPut, "/api/v1/items/"+a.itemID+"/labels/"+a.labelID, a.cookie, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("seed A's own item<->label edge (item %q, label %q): status = %d, want 204: %s", a.itemID, a.labelID, rec.Code, rec.Body.String())
	}

	aItemBeforeAttack := pushIsoGetItem(t, srv, false, a.cookie, a.itemID)

	attackResp, attackRec := pushIsoPushOnce(t, srv, false, b.cookie, "dev-b-attack", []pushIsoMutationWire{
		pushIsoItemUpdateMutation("mut-b-attack-item-update", a.itemID, aItemBeforeAttack.Version, "HIJACKED BY B"),
		pushIsoWarrantyCreateMutation("mut-b-attack-warranty-create", "push-iso-b-attack-warranty-1", a.itemID, "HIJACKED HOLDER"),
		pushIsoItemLabelCreateMutation("mut-b-attack-item-label-create", "push-iso-b-attack-item-label-1", a.itemID, b.labelID),
		pushIsoItemLabelDeleteMutation("mut-b-attack-item-label-delete", a.itemID, a.labelID),
	})

	if len(attackResp.Applied) != 0 {
		t.Fatalf("B's cross-group attack batch applied %d mutation(s), want 0: %+v", len(attackResp.Applied), attackResp.Applied)
	}
	if len(attackResp.Skipped) != 0 {
		t.Fatalf("B's cross-group attack batch skipped %d mutation(s), want 0 (every mutation_id here is fresh): %+v", len(attackResp.Skipped), attackResp.Skipped)
	}
	wantConflictField := map[string]string{
		"mut-b-attack-item-update":       entityConflictFieldWire,
		"mut-b-attack-warranty-create":   "item_id",
		"mut-b-attack-item-label-create": "item_id",
		"mut-b-attack-item-label-delete": entityConflictFieldWire,
	}
	gotConflictField := pushIsoConflictFieldByMutationID(attackResp.Conflicts)
	for mutationID, wantField := range wantConflictField {
		if got, ok := gotConflictField[mutationID]; !ok {
			t.Errorf("B's attack batch has no conflicts[] entry for mutation_id %q; got conflicts: %+v", mutationID, attackResp.Conflicts)
		} else if got != wantField {
			t.Errorf("B's attack mutation %q conflicted on field_name %q, want %q", mutationID, got, wantField)
		}
	}
	if len(attackResp.Conflicts) != len(wantConflictField) {
		t.Errorf("B's attack batch produced %d conflicts, want exactly %d (one per attack mutation): %+v",
			len(attackResp.Conflicts), len(wantConflictField), attackResp.Conflicts)
	}

	pushIsoResponseBodyMustNotContain(t, "B's cross-group attack response", attackRec, aItemBeforeAttack.Name)

	for _, cred := range credentials {
		t.Run("cross_group_attack_wrote_nothing_to_A/"+cred.kind, func(t *testing.T) {
			aItemAfterAttack := pushIsoGetItem(t, srv, cred.bearer, pushIsoCredValue(a, cred.bearer), a.itemID)
			if aItemAfterAttack != aItemBeforeAttack {
				t.Fatalf("A's item changed after B's cross-group attack batch: before=%+v after=%+v", aItemBeforeAttack, aItemAfterAttack)
			}

			if code := pushIsoWarrantyStatus(t, srv, cred.bearer, pushIsoCredValue(a, cred.bearer), a.itemID); code != http.StatusNotFound {
				t.Fatalf("A's item now answers GET .../warranty with status %d, want 404 (no warranty block was ever created) -- B's cross-group warranty_block create landed", code)
			}

			labelIDs := pushIsoItemLabelIDs(t, srv, cred.bearer, pushIsoCredValue(a, cred.bearer), a.itemID)
			if len(labelIDs) != 1 || labelIDs[0] != a.labelID {
				t.Fatalf("A's item now carries label edge(s) %v, want exactly [%q] (A's own pre-existing edge, seeded before the attack) -- B's cross-group item_label create/delete landed",
					labelIDs, a.labelID)
			}
		})
	}

	respAItemUpdateControl, _ := pushIsoPushOnce(t, srv, false, a.cookie, "dev-a-control-item-update", []pushIsoMutationWire{
		pushIsoItemUpdateMutation("mut-a-control-item-update", a.itemID, aItemBeforeAttack.Version, "A's own legitimate update -- same entity_id/base_version B's attack just tried"),
	})
	if len(respAItemUpdateControl.Applied) != 1 || respAItemUpdateControl.Applied[0].Version != aItemBeforeAttack.Version+1 {
		t.Fatalf("A's own control update (identical entity_id=%q base_version=%d to B's attack) = %+v, want exactly one applied entry at version %d -- "+
			"if this fails, a.itemID/base_version was never a live, A-owned target and requirement 2's attack proved nothing about isolation",
			a.itemID, aItemBeforeAttack.Version, respAItemUpdateControl, aItemBeforeAttack.Version+1)
	}

	respAWarrantyControl, _ := pushIsoPushOnce(t, srv, false, a.cookie, "dev-a-control-warranty-create", []pushIsoMutationWire{
		pushIsoWarrantyCreateMutation("mut-a-control-warranty-create", "push-iso-a-control-warranty-1", a.itemID, "A's own legitimate warranty -- same fields.item_id B's attack just tried"),
	})
	if len(respAWarrantyControl.Applied) != 1 {
		t.Fatalf("A's own control warranty_block create (identical fields.item_id=%q to B's attack) = %+v, want exactly one applied entry -- "+
			"if this fails, a.itemID was never a live, A-owned item and requirement 2's attack proved nothing about isolation",
			a.itemID, respAWarrantyControl)
	}

	aControlLabelID := syncIsoCreateLabel(t, srv, a.cookie, "push-iso-alice-control-label-1")
	respAItemLabelCreateControl, _ := pushIsoPushOnce(t, srv, false, a.cookie, "dev-a-control-item-label-create", []pushIsoMutationWire{
		pushIsoItemLabelCreateMutation("mut-a-control-item-label-create", "push-iso-a-control-item-label-1", a.itemID, aControlLabelID),
	})
	if len(respAItemLabelCreateControl.Applied) != 1 {
		t.Fatalf("A's own control item_label create (identical fields.item_id=%q to B's attack) = %+v, want exactly one applied entry -- "+
			"if this fails, a.itemID was never a live, A-owned item and requirement 2's attack proved nothing about isolation",
			a.itemID, respAItemLabelCreateControl)
	}

	respAItemLabelDeleteControl, _ := pushIsoPushOnce(t, srv, false, a.cookie, "dev-a-control-item-label-delete", []pushIsoMutationWire{
		pushIsoItemLabelDeleteMutation("mut-a-control-item-label-delete", a.itemID, a.labelID),
	})
	if len(respAItemLabelDeleteControl.Applied) != 1 {
		t.Fatalf("A's own control item_label delete (identical item_id=%q label_id=%q to B's attack) = %+v, want exactly one applied entry -- "+
			"if this fails, that edge was never live and A-owned, and requirement 2's delete attack proved nothing about isolation",
			a.itemID, a.labelID, respAItemLabelDeleteControl)
	}

	const sharedMutationID = "push-iso-shared-mutation-id-across-groups"
	aIdemEntityID := "push-iso-a-idem-item"
	bIdemEntityID := "push-iso-b-idem-item"

	respAIdem, _ := pushIsoPushOnce(t, srv, false, a.cookie, "dev-a-idem", []pushIsoMutationWire{
		pushIsoItemCreateMutation(sharedMutationID, aIdemEntityID, "A's idempotency-ledger item"),
	})
	if len(respAIdem.Applied) != 1 || respAIdem.Applied[0].EntityID != aIdemEntityID {
		t.Fatalf("A's own create under mutation_id %q = %+v, want exactly one applied entry for entity_id %q", sharedMutationID, respAIdem, aIdemEntityID)
	}

	respBIdem, _ := pushIsoPushOnce(t, srv, false, b.cookie, "dev-b-idem", []pushIsoMutationWire{
		pushIsoItemCreateMutation(sharedMutationID, bIdemEntityID, "B's idempotency-ledger item"),
	})
	if len(respBIdem.Skipped) != 0 {
		t.Fatalf("B's push using the SAME mutation_id %q A already consumed was skipped: %+v -- the idempotency ledger is not scoped per group (B could be permanently blocked by guessing/replaying A's own mutation_id)",
			sharedMutationID, respBIdem.Skipped)
	}
	if len(respBIdem.Applied) != 1 || respBIdem.Applied[0].EntityID != bIdemEntityID {
		t.Fatalf("B's own create under mutation_id %q = %+v, want exactly one applied entry for entity_id %q", sharedMutationID, respBIdem, bIdemEntityID)
	}

	respAReplay, _ := pushIsoPushOnce(t, srv, false, a.cookie, "dev-a-idem-replay", []pushIsoMutationWire{
		pushIsoItemCreateMutation(sharedMutationID, aIdemEntityID, "A's REPLAYED idempotency-ledger item (should never apply)"),
	})
	if len(respAReplay.Skipped) != 1 || respAReplay.Skipped[0].MutationID != sharedMutationID {
		t.Fatalf("A's own replay of mutation_id %q = %+v, want exactly one skipped entry -- the idempotency ledger does not appear to work at all", sharedMutationID, respAReplay)
	}
	if len(respAReplay.Applied) != 0 {
		t.Fatalf("A's own replay of mutation_id %q applied %+v, want none", sharedMutationID, respAReplay.Applied)
	}

	aItemBeforeCollide := pushIsoGetItem(t, srv, false, a.cookie, a.itemID)

	collideResp, _ := pushIsoPushOnce(t, srv, false, b.cookie, "dev-b-collide", []pushIsoMutationWire{
		pushIsoItemCreateMutation("mut-b-collide-with-a-item-id", a.itemID, "B trying to steal A's item id"),
	})
	if len(collideResp.Applied) != 0 {
		t.Fatalf("B's create naming A's own real item id as entity_id applied: %+v, want none (a same-id create must never overwrite another tenant's row)", collideResp.Applied)
	}
	if len(collideResp.Conflicts) != 1 || collideResp.Conflicts[0].MutationID != "mut-b-collide-with-a-item-id" || collideResp.Conflicts[0].FieldName != entityConflictFieldWire {
		t.Fatalf("B's create naming A's own real item id as entity_id = conflicts %+v, want exactly one whole-entity (%q) conflict", collideResp.Conflicts, entityConflictFieldWire)
	}

	for _, cred := range credentials {
		t.Run("entity_id_collision_wrote_nothing_to_A/"+cred.kind, func(t *testing.T) {
			aItemAfterCollide := pushIsoGetItem(t, srv, cred.bearer, pushIsoCredValue(a, cred.bearer), a.itemID)
			if aItemAfterCollide != aItemBeforeCollide {
				t.Fatalf("A's item changed after B's client-supplied entity_id collision attempt: before=%+v after=%+v", aItemBeforeCollide, aItemAfterCollide)
			}
		})
	}

	changesBAfterCollide, _ := syncIsoPullAllPages(t, srv, false, b.cookie, "dev-b-collide-pull", 0, 500)
	if pushIsoChangesContainItemID(changesBAfterCollide, a.itemID) {
		t.Fatalf("B's own pull shows A's item id %q as one of B's rows after a REJECTED entity_id-collision create -- nothing should have been written", a.itemID)
	}
}

const entityConflictFieldWire = "_entity"

type pushIsoMutationWire struct {
	MutationID  string         `json:"mutation_id"`
	EntityType  string         `json:"entity_type"`
	EntityID    string         `json:"entity_id"`
	BaseVersion int64          `json:"base_version"`
	Fields      map[string]any `json:"fields"`
	Op          string         `json:"op,omitempty"`
}

type pushIsoRequestWire struct {
	DeviceID  string                `json:"device_id"`
	Mutations []pushIsoMutationWire `json:"mutations"`
}

type pushIsoAppliedWire struct {
	MutationID string `json:"mutation_id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Version    int64  `json:"version"`
}

type pushIsoSkippedWire struct {
	MutationID string `json:"mutation_id"`
}

type pushIsoConflictWire struct {
	MutationID string `json:"mutation_id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	FieldName  string `json:"field_name"`
}

type pushIsoResponseWire struct {
	Applied      []pushIsoAppliedWire  `json:"applied"`
	Skipped      []pushIsoSkippedWire  `json:"skipped"`
	Conflicts    []pushIsoConflictWire `json:"conflicts"`
	NewWatermark int64                 `json:"new_watermark"`
}

type pushIsoTenant struct {
	name    string
	groupID string
	cookie  string
	bearer  string

	itemID  string
	labelID string
}

func provisionPushIsoTenant(t *testing.T, srv *liveServer, username string) pushIsoTenant {
	t.Helper()
	tn := pushIsoTenant{name: username}
	password := username + "-password-1"

	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody(username, password)); rec.Code != http.StatusCreated {
		t.Fatalf("register %s: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody(username, password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var login loginResponse
	mustDecode(t, rec, &login)
	tn.groupID = login.GroupID
	tn.cookie = sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/device-tokens", tn.cookie,
		[]byte(fmt.Sprintf(`{"device_label":%q}`, username+"'s phone")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%s issue device token: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var device deviceTokenIssueResponse
	mustDecode(t, rec, &device)
	tn.bearer = device.Token
	if tn.bearer == "" {
		t.Fatalf("%s's device-token issue returned no token; the bearer half of this suite would authenticate as nobody", username)
	}

	itemRec := srv.do(t, http.MethodPost, "/api/v1/items", tn.cookie, itemCreateBody(username+"-push-iso-item-1"))
	if itemRec.Code != http.StatusCreated {
		t.Fatalf("%s create item: status = %d: %s", username, itemRec.Code, itemRec.Body.String())
	}
	var it itemResponse
	mustDecode(t, itemRec, &it)
	if it.ID == "" {
		t.Fatalf("%s create item: response carried no id", username)
	}
	tn.itemID = it.ID

	labelRec := srv.do(t, http.MethodPost, "/api/v1/labels", tn.cookie, labelCreateBody(username+"-push-iso-label-1", "#654321"))
	if labelRec.Code != http.StatusCreated {
		t.Fatalf("%s create label: status = %d: %s", username, labelRec.Code, labelRec.Body.String())
	}
	var lbl labelResponse
	mustDecode(t, labelRec, &lbl)
	if lbl.ID == "" {
		t.Fatalf("%s create label: response carried no id", username)
	}
	tn.labelID = lbl.ID

	return tn
}

func requirePushIsoDistinctTenantIDs(t *testing.T, a, b pushIsoTenant) {
	t.Helper()
	if a.groupID == b.groupID {
		t.Fatalf("%s and %s landed in the SAME group (%q); this suite proves nothing without two independent households", a.name, b.name, a.groupID)
	}
	pairs := []struct{ what, x, y string }{
		{"item id", a.itemID, b.itemID},
		{"label id", a.labelID, b.labelID},
	}
	for _, p := range pairs {
		if p.x == "" || p.y == "" {
			t.Fatalf("test bug: empty %s (a=%q b=%q)", p.what, p.x, p.y)
		}
		if p.x == p.y {
			t.Fatalf("both households got the same %s (%q); the exclusion assertions cannot distinguish them", p.what, p.x)
		}
	}
}

func pushIsoCredValue(tn pushIsoTenant, bearer bool) string {
	if bearer {
		return tn.bearer
	}
	return tn.cookie
}

func pushIsoPushOnce(t *testing.T, srv *liveServer, bearer bool, credValue, deviceID string, mutations []pushIsoMutationWire) (pushIsoResponseWire, *httptest.ResponseRecorder) {
	t.Helper()
	body, err := json.Marshal(pushIsoRequestWire{DeviceID: deviceID, Mutations: mutations})
	if err != nil {
		t.Fatalf("marshal sync push request body: %v", err)
	}
	var rec *httptest.ResponseRecorder
	if bearer {
		rec = srv.doBearer(t, http.MethodPost, "/api/v1/sync/push", credValue, body)
	} else {
		rec = srv.do(t, http.MethodPost, "/api/v1/sync/push", credValue, body)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /sync/push (device_id=%s, %d mutation(s)): status = %d, want 200: %s", deviceID, len(mutations), rec.Code, rec.Body.String())
	}
	var result pushIsoResponseWire
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode sync push result: %v (body: %s)", err, rec.Body.String())
	}
	return result, rec
}

func pushIsoWatermark(t *testing.T, srv *liveServer, bearer bool, credValue, deviceID string) int64 {
	t.Helper()
	resp, _ := pushIsoPushOnce(t, srv, bearer, credValue, deviceID, nil)
	return resp.NewWatermark
}

func pushIsoConflictFieldByMutationID(conflicts []pushIsoConflictWire) map[string]string {
	m := make(map[string]string, len(conflicts))
	for _, c := range conflicts {
		m[c.MutationID] = c.FieldName
	}
	return m
}

func pushIsoResponseBodyMustNotContain(t *testing.T, label string, rec *httptest.ResponseRecorder, needle string) {
	t.Helper()
	if needle == "" {
		t.Fatalf("test bug: empty needle passed to pushIsoResponseBodyMustNotContain (%s)", label)
	}
	if strings.Contains(rec.Body.String(), needle) {
		t.Fatalf("%s leaked %q: %s", label, needle, rec.Body.String())
	}
}

func pushIsoItemCreateMutation(mutationID, entityID, name string) pushIsoMutationWire {
	return pushIsoMutationWire{
		MutationID: mutationID, EntityType: "item", EntityID: entityID, BaseVersion: 0,
		Fields: map[string]any{"name": name, "description": "", "location_id": "", "quantity": int64(1)},
	}
}

func pushIsoItemUpdateMutation(mutationID, entityID string, baseVersion int64, name string) pushIsoMutationWire {
	return pushIsoMutationWire{
		MutationID: mutationID, EntityType: "item", EntityID: entityID, BaseVersion: baseVersion,
		Fields: map[string]any{"name": name},
	}
}

func pushIsoWarrantyCreateMutation(mutationID, entityID, itemID, holder string) pushIsoMutationWire {
	return pushIsoMutationWire{
		MutationID: mutationID, EntityType: "warranty_block", EntityID: entityID, BaseVersion: 0,
		Fields: map[string]any{
			"item_id": itemID, "holder": holder, "provider": "", "starts_on": "", "expires_on": "",
			"is_lifetime": false, "notes": "",
		},
	}
}

func pushIsoItemLabelCreateMutation(mutationID, entityID, itemID, labelID string) pushIsoMutationWire {
	return pushIsoMutationWire{
		MutationID: mutationID, EntityType: "item_label", EntityID: entityID, BaseVersion: 0,
		Fields: map[string]any{"item_id": itemID, "label_id": labelID},
	}
}

func pushIsoItemLabelDeleteMutation(mutationID, itemID, labelID string) pushIsoMutationWire {
	return pushIsoMutationWire{
		MutationID: mutationID, EntityType: "item_label", EntityID: "push-iso-delete-op-unused-entity-id",
		BaseVersion: 0, Op: "delete",
		Fields: map[string]any{"item_id": itemID, "label_id": labelID},
	}
}

func pushIsoDo(t *testing.T, srv *liveServer, bearer bool, credValue, path string) *httptest.ResponseRecorder {
	t.Helper()
	if bearer {
		return srv.doBearer(t, http.MethodGet, path, credValue, nil)
	}
	return srv.do(t, http.MethodGet, path, credValue, nil)
}

func pushIsoGetItem(t *testing.T, srv *liveServer, bearer bool, credValue, itemID string) itemResponse {
	t.Helper()
	rec := pushIsoDo(t, srv, bearer, credValue, "/api/v1/items/"+itemID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET item %q: status = %d, want 200: %s", itemID, rec.Code, rec.Body.String())
	}
	var it itemResponse
	mustDecode(t, rec, &it)
	return it
}

func pushIsoWarrantyStatus(t *testing.T, srv *liveServer, bearer bool, credValue, itemID string) int {
	t.Helper()
	rec := pushIsoDo(t, srv, bearer, credValue, "/api/v1/items/"+itemID+"/warranty")
	return rec.Code
}

type pushIsoItemLabelListWire struct {
	Labels []struct {
		ID string `json:"id"`
	} `json:"labels"`
}

func pushIsoItemLabelIDs(t *testing.T, srv *liveServer, bearer bool, credValue, itemID string) []string {
	t.Helper()
	rec := pushIsoDo(t, srv, bearer, credValue, "/api/v1/items/"+itemID+"/labels")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET item %q labels: status = %d, want 200: %s", itemID, rec.Code, rec.Body.String())
	}
	var resp pushIsoItemLabelListWire
	mustDecode(t, rec, &resp)
	ids := make([]string, 0, len(resp.Labels))
	for _, l := range resp.Labels {
		ids = append(ids, l.ID)
	}
	return ids
}

func pushIsoChangesContainItemID(changes []syncIsoChangeWire, itemID string) bool {
	for _, c := range changes {
		if c.EntityType == "item" && c.ID == itemID {
			return true
		}
	}
	return false
}
