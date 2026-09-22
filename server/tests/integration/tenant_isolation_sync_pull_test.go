package integration

import (
	"encoding/json"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func TestTenantIsolationSyncPullDoesNotLeakAcrossGroups(t *testing.T) {
	srv := newLiveServer(t, true)
	a := provisionSyncIsoTenant(t, srv, "sync-iso-alice")
	b := provisionSyncIsoTenant(t, srv, "sync-iso-brenda")
	requireDistinctSyncIsoTenantIDs(t, a, b)

	credentials := []struct {
		kind   string
		bearer bool
	}{
		{"cookie", false},
		{"bearer", true},
	}

	var changesBBeforeDelete []syncIsoChangeWire
	for _, cred := range credentials {
		t.Run("all_eleven_entity_types_own_present_other_absent/"+cred.kind, func(t *testing.T) {
			changesA, tombstonesA := syncIsoPullAllPages(t, srv, cred.bearer, syncIsoCredValue(a, cred.bearer), "dev-a-baseline", 0, 3)
			changesB, tombstonesB := syncIsoPullAllPages(t, srv, cred.bearer, syncIsoCredValue(b, cred.bearer), "dev-b-baseline", 0, 3)
			if len(tombstonesA) != 0 {
				t.Fatalf("A's pre-deletion baseline pull carries %d tombstone(s), want 0 -- nothing has been deleted yet: %+v", len(tombstonesA), tombstonesA)
			}
			if len(tombstonesB) != 0 {
				t.Fatalf("B's pre-deletion baseline pull carries %d tombstone(s), want 0 -- nothing has been deleted yet: %+v", len(tombstonesB), tombstonesB)
			}
			assertSyncIsoEveryEntityTypeOwnPresentOtherAbsent(t, "A", a, b, changesA)
			assertSyncIsoEveryEntityTypeOwnPresentOtherAbsent(t, "B", b, a, changesB)
			if cred.kind == "cookie" {
				changesBBeforeDelete = changesB
			}
		})
	}
	if changesBBeforeDelete == nil {
		t.Fatalf("test bug: changesBBeforeDelete was never captured")
	}

	warranty2ID := findSyncIsoChangeIDByItemID(t, changesBBeforeDelete, "warranty_block", b.item2ID)
	sale2ID := findSyncIsoChangeIDByItemID(t, changesBBeforeDelete, "sold_to_block", b.item2ID)
	purchase2ID := findSyncIsoChangeIDByItemID(t, changesBBeforeDelete, "purchased_from_block", b.item2ID)
	itemLabel2ID := findSyncIsoChangeIDByItemLabel(t, changesBBeforeDelete, b.item2ID, b.labelID)

	overlappingSince := medianSyncIsoChangeSeq(t, changesBBeforeDelete)
	if overlappingSince <= 0 {
		t.Fatalf("test bug: could not find a positive change_seq in B's own history to reuse as A's `since`")
	}
	for _, cred := range credentials {
		t.Run("since_in_overlapping_watermark_space_stays_group_scoped/"+cred.kind, func(t *testing.T) {
			changesA, _ := syncIsoPullAllPages(t, srv, cred.bearer, syncIsoCredValue(a, cred.bearer), "dev-a-watermark", overlappingSince, 500)
			for _, c := range changesA {
				if syncIsoChangeMatchesTenant(t, c, b) {
					t.Fatalf("A pulling with since=%d (one of B's own real change_seq values, inside A's own overlapping range) returned B's %s (id %q): change_seq is not correctly scoped per group", overlappingSince, c.EntityType, c.ID)
				}
			}
		})
	}

	deleteSyncIsoTombstoneFixtures(t, srv, b)

	wantBTombstones := map[string]string{
		"item":                 b.item2ID,
		"warranty_block":       warranty2ID,
		"sold_to_block":        sale2ID,
		"purchased_from_block": purchase2ID,
		"item_identification":  b.identification2ID,
		"item_custom_field":    b.itemCustomField2ID,
		"item_label":           itemLabel2ID,
		"label":                b.label2ID,
		"location":             b.location2ID,
		"attachment":           b.attachment2ID,
	}
	for entityType, id := range wantBTombstones {
		if id == "" {
			t.Fatalf("test bug: no captured tombstone id for entity_type %q", entityType)
		}
	}

	for _, cred := range credentials {
		t.Run("tombstones_do_not_leak_across_groups/"+cred.kind, func(t *testing.T) {
			changesA, tombstonesA := syncIsoPullAllPages(t, srv, cred.bearer, syncIsoCredValue(a, cred.bearer), "dev-a-post-delete", 0, 3)
			_, tombstonesB := syncIsoPullAllPages(t, srv, cred.bearer, syncIsoCredValue(b, cred.bearer), "dev-b-post-delete", 0, 3)

			assertSyncIsoEveryEntityTypeOwnPresentOtherAbsent(t, "A", a, b, changesA)

			for entityType, id := range wantBTombstones {
				if !syncIsoTombstoneListContains(tombstonesB, entityType, id) {
					t.Errorf("B's own pull is missing its own %s tombstone %q: %+v", entityType, id, tombstonesB)
				}
				if syncIsoTombstoneListContains(tombstonesA, entityType, id) {
					t.Errorf("A's pull LEAKED B's %s tombstone %q across groups", entityType, id)
				}
			}
		})
	}

	bScope, err := srv.storage.ForGroupSync(storage.MustGroupID(b.groupID))
	if err != nil {
		t.Fatalf("ForGroupSync(B): %v", err)
	}
	const bumpedLowWatermark = int64(1_000_000_000)
	if err := bScope.SetLowWatermark(t.Context(), bumpedLowWatermark); err != nil {
		t.Fatalf("SetLowWatermark(B, %d): %v", bumpedLowWatermark, err)
	}
	const midSince = int64(1)

	for _, cred := range credentials {
		t.Run("low_watermark_is_per_group/"+cred.kind, func(t *testing.T) {
			resultA := syncIsoPullOnce(t, srv, cred.bearer, syncIsoCredValue(a, cred.bearer), "dev-a-watermark2", midSince, 10)
			if resultA.CursorTooOld {
				t.Fatalf("A's pull at since=%d answered cursor_too_old after ONLY B's low-watermark was bumped to %d -- the low-watermark check is not scoped per group", midSince, bumpedLowWatermark)
			}
			resultB := syncIsoPullOnce(t, srv, cred.bearer, syncIsoCredValue(b, cred.bearer), "dev-b-watermark2", midSince, 10)
			if !resultB.CursorTooOld {
				t.Fatalf("B's pull at since=%d did not answer cursor_too_old after SetLowWatermark(B, %d) -- the bump had no observable effect", midSince, bumpedLowWatermark)
			}
		})
	}
}

type syncIsoChangeWire struct {
	EntityType     string          `json:"entity_type"`
	ID             string          `json:"id"`
	GroupChangeSeq int64           `json:"group_change_seq"`
	Data           json.RawMessage `json:"data"`
}

type syncIsoTombstoneWire struct {
	EntityType string `json:"entity_type"`
	ID         string `json:"id"`
	DeletedAt  int64  `json:"deleted_at"`
}

type syncIsoPullResponseWire struct {
	CursorTooOld  bool                   `json:"cursor_too_old"`
	Changes       []syncIsoChangeWire    `json:"changes"`
	Tombstones    []syncIsoTombstoneWire `json:"tombstones"`
	NextWatermark int64                  `json:"next_watermark"`
	HasMore       bool                   `json:"has_more"`
}

var syncIsoEntityTypes = []string{
	"item", "warranty_block", "sold_to_block", "purchased_from_block",
	"item_identification", "item_custom_field", "stock_adjustment",
	"location", "label", "item_label", "attachment",
}

type syncIsoTenant struct {
	name    string
	groupID string
	cookie  string
	bearer  string

	itemID  string
	item2ID string

	identificationID   string
	identification2ID  string
	itemCustomFieldID  string
	itemCustomField2ID string
	stockAdjustmentID  string

	locationID  string
	location2ID string

	labelID  string
	label2ID string

	attachmentID  string
	attachment2ID string
}

func requireDistinctSyncIsoTenantIDs(t *testing.T, a, b syncIsoTenant) {
	t.Helper()
	if a.groupID == b.groupID {
		t.Fatalf("%s and %s landed in the SAME group (%q); this suite proves nothing without two independent households", a.name, b.name, a.groupID)
	}
	pairs := []struct{ what, x, y string }{
		{"item id", a.itemID, b.itemID},
		{"item2 id", a.item2ID, b.item2ID},
		{"identification id", a.identificationID, b.identificationID},
		{"identification2 id", a.identification2ID, b.identification2ID},
		{"item custom field id", a.itemCustomFieldID, b.itemCustomFieldID},
		{"item custom field2 id", a.itemCustomField2ID, b.itemCustomField2ID},
		{"stock adjustment id", a.stockAdjustmentID, b.stockAdjustmentID},
		{"location id", a.locationID, b.locationID},
		{"location2 id", a.location2ID, b.location2ID},
		{"label id", a.labelID, b.labelID},
		{"label2 id", a.label2ID, b.label2ID},
		{"attachment id", a.attachmentID, b.attachmentID},
		{"attachment2 id", a.attachment2ID, b.attachment2ID},
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

func provisionSyncIsoTenant(t *testing.T, srv *liveServer, username string) syncIsoTenant {
	t.Helper()
	tn := syncIsoTenant{name: username}
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

	tn.itemID = syncIsoCreateItem(t, srv, tn.cookie, username+"-item-1")
	syncIsoCreateWarranty(t, srv, tn.cookie, tn.itemID, username+"-warranty-1")
	syncIsoCreateSale(t, srv, tn.cookie, tn.itemID, username+"-buyer-1")
	syncIsoCreatePurchase(t, srv, tn.cookie, tn.itemID, username+"-vendor-1")
	tn.identificationID = syncIsoCreateIdentification(t, srv, tn.cookie, tn.itemID, username+"-serial-1")
	tn.itemCustomFieldID = syncIsoCreateItemCustomField(t, srv, tn.cookie, tn.itemID, username+"-colour-1")
	tn.stockAdjustmentID = syncIsoCreateStockAdjustment(t, srv, tn.cookie, tn.itemID, 3, username+"-stock-1")
	tn.locationID = syncIsoCreateLocation(t, srv, tn.cookie, username+"-location-1")
	tn.labelID = syncIsoCreateLabel(t, srv, tn.cookie, username+"-label-1")
	syncIsoAttachItemLabel(t, srv, tn.cookie, tn.itemID, tn.labelID)
	tn.attachmentID = syncIsoUploadAttachment(t, srv, tn.cookie, tn.itemID, username+"-photo-1.jpg")

	tn.item2ID = syncIsoCreateItem(t, srv, tn.cookie, username+"-item-2")
	syncIsoCreateWarranty(t, srv, tn.cookie, tn.item2ID, username+"-warranty-2")
	syncIsoCreateSale(t, srv, tn.cookie, tn.item2ID, username+"-buyer-2")
	syncIsoCreatePurchase(t, srv, tn.cookie, tn.item2ID, username+"-vendor-2")
	tn.identification2ID = syncIsoCreateIdentification(t, srv, tn.cookie, tn.item2ID, username+"-serial-2")
	tn.itemCustomField2ID = syncIsoCreateItemCustomField(t, srv, tn.cookie, tn.item2ID, username+"-colour-2")
	syncIsoAttachItemLabel(t, srv, tn.cookie, tn.item2ID, tn.labelID)
	tn.label2ID = syncIsoCreateLabel(t, srv, tn.cookie, username+"-label-2")
	tn.location2ID = syncIsoCreateLocation(t, srv, tn.cookie, username+"-location-2")
	tn.attachment2ID = syncIsoUploadAttachment(t, srv, tn.cookie, tn.itemID, username+"-photo-2.jpg")

	return tn
}

func deleteSyncIsoTombstoneFixtures(t *testing.T, srv *liveServer, tn syncIsoTenant) {
	t.Helper()
	if rec := srv.do(t, http.MethodDelete, "/api/v1/items/"+tn.item2ID, tn.cookie, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("%s delete item2 %q: status = %d, want 204: %s", tn.name, tn.item2ID, rec.Code, rec.Body.String())
	}
	if rec := srv.do(t, http.MethodDelete, "/api/v1/labels/"+tn.label2ID, tn.cookie, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("%s delete label2 %q: status = %d, want 204: %s", tn.name, tn.label2ID, rec.Code, rec.Body.String())
	}
	if rec := srv.do(t, http.MethodDelete, "/api/v1/locations/"+tn.location2ID, tn.cookie, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("%s delete location2 %q: status = %d, want 204: %s", tn.name, tn.location2ID, rec.Code, rec.Body.String())
	}
	if rec := srv.do(t, http.MethodDelete, "/api/v1/items/"+tn.itemID+"/attachments/"+tn.attachment2ID, tn.cookie, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("%s delete attachment2 %q: status = %d, want 204: %s", tn.name, tn.attachment2ID, rec.Code, rec.Body.String())
	}
}

func syncIsoCreateItem(t *testing.T, srv *liveServer, cookie, name string) string {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody(name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item %q: status = %d: %s", name, rec.Code, rec.Body.String())
	}
	var it itemResponse
	mustDecode(t, rec, &it)
	if it.ID == "" {
		t.Fatalf("create item %q: response carried no id", name)
	}
	return it.ID
}

func syncIsoCreateWarranty(t *testing.T, srv *liveServer, cookie, itemID, holder string) {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items/"+itemID+"/warranty", cookie, warrantyCreateBody(holder))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create warranty on item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
}

func syncIsoCreateSale(t *testing.T, srv *liveServer, cookie, itemID, buyerName string) {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items/"+itemID+"/sale", cookie, saleCreateBody(buyerName))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create sale on item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
}

func syncIsoCreatePurchase(t *testing.T, srv *liveServer, cookie, itemID, vendor string) {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items/"+itemID+"/purchase", cookie, purchaseCreateBody(vendor))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create purchase on item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
}

func syncIsoCreateIdentification(t *testing.T, srv *liveServer, cookie, itemID, value string) string {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items/"+itemID+"/identifications", cookie, identificationCreateBody("serial", value))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create identification on item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
	var ident identificationResponse
	mustDecode(t, rec, &ident)
	if ident.ID == "" {
		t.Fatalf("create identification on item %q: response carried no id", itemID)
	}
	return ident.ID
}

func syncIsoCreateItemCustomField(t *testing.T, srv *liveServer, cookie, itemID, value string) string {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items/"+itemID+"/custom-fields", cookie, itemCustomFieldCreateBody("Colour", value))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item custom field on item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
	var cf itemCustomFieldResponse
	mustDecode(t, rec, &cf)
	if cf.ID == "" {
		t.Fatalf("create item custom field on item %q: response carried no id", itemID)
	}
	return cf.ID
}

func syncIsoCreateStockAdjustment(t *testing.T, srv *liveServer, cookie, itemID string, delta int64, note string) string {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items/"+itemID+"/stock-adjustments", cookie, stockAdjustmentCreateBody(delta, "initial stock", note))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create stock adjustment on item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
	var sa stockAdjustmentResponse
	mustDecode(t, rec, &sa)
	if sa.ID == "" {
		t.Fatalf("create stock adjustment on item %q: response carried no id", itemID)
	}
	return sa.ID
}

func syncIsoCreateLocation(t *testing.T, srv *liveServer, cookie, name string) string {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/locations", cookie, locationCreateBody(name, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create location %q: status = %d: %s", name, rec.Code, rec.Body.String())
	}
	var loc locationResponse
	mustDecode(t, rec, &loc)
	if loc.ID == "" {
		t.Fatalf("create location %q: response carried no id", name)
	}
	return loc.ID
}

func syncIsoCreateLabel(t *testing.T, srv *liveServer, cookie, name string) string {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/labels", cookie, labelCreateBody(name, "#123456"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create label %q: status = %d: %s", name, rec.Code, rec.Body.String())
	}
	var lbl labelResponse
	mustDecode(t, rec, &lbl)
	if lbl.ID == "" {
		t.Fatalf("create label %q: response carried no id", name)
	}
	return lbl.ID
}

func syncIsoAttachItemLabel(t *testing.T, srv *liveServer, cookie, itemID, labelID string) {
	t.Helper()
	rec := srv.do(t, http.MethodPut, "/api/v1/items/"+itemID+"/labels/"+labelID, cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("attach label %q to item %q: status = %d, want 204: %s", labelID, itemID, rec.Code, rec.Body.String())
	}
}

func syncIsoUploadAttachment(t *testing.T, srv *liveServer, cookie, itemID, filename string) string {
	t.Helper()
	body, contentType := attachmentUploadMultipartBody(t, attachments.CategoryImage, filename, testImageJPEGBytes(t))
	rec := srv.doMultipart(t, http.MethodPost, "/api/v1/items/"+itemID+"/attachments", cookie, body, contentType)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload attachment %q on item %q: status = %d: %s", filename, itemID, rec.Code, rec.Body.String())
	}
	var att attachmentResponse
	mustDecode(t, rec, &att)
	if att.ID == "" {
		t.Fatalf("upload attachment %q on item %q: response carried no id", filename, itemID)
	}
	return att.ID
}

func syncIsoCredValue(tn syncIsoTenant, bearer bool) string {
	if bearer {
		return tn.bearer
	}
	return tn.cookie
}

func syncIsoPullRequestBodyJSON(t *testing.T, deviceID string, since, limit int64) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"device_id": deviceID, "since": since, "limit": limit})
	if err != nil {
		t.Fatalf("marshal sync pull request body: %v", err)
	}
	return b
}

func syncIsoPullOnce(t *testing.T, srv *liveServer, bearer bool, credValue, deviceID string, since, limit int64) syncIsoPullResponseWire {
	t.Helper()
	body := syncIsoPullRequestBodyJSON(t, deviceID, since, limit)
	var rec *httptest.ResponseRecorder
	if bearer {
		rec = srv.doBearer(t, http.MethodPost, "/api/v1/sync/pull", credValue, body)
	} else {
		rec = srv.do(t, http.MethodPost, "/api/v1/sync/pull", credValue, body)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /sync/pull (device_id=%s since=%d limit=%d): status = %d, want 200: %s", deviceID, since, limit, rec.Code, rec.Body.String())
	}
	var result syncIsoPullResponseWire
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode sync pull result: %v (body: %s)", err, rec.Body.String())
	}
	return result
}

func syncIsoPullAllPages(t *testing.T, srv *liveServer, bearer bool, credValue, deviceID string, startSince, limit int64) ([]syncIsoChangeWire, []syncIsoTombstoneWire) {
	t.Helper()
	var changes []syncIsoChangeWire
	var tombstones []syncIsoTombstoneWire
	since := startSince
	for page := 0; page < 200; page++ {
		result := syncIsoPullOnce(t, srv, bearer, credValue, deviceID, since, limit)
		if result.CursorTooOld {
			t.Fatalf("pull (device_id=%s since=%d) unexpectedly answered cursor_too_old", deviceID, since)
		}
		changes = append(changes, result.Changes...)
		tombstones = append(tombstones, result.Tombstones...)
		if !result.HasMore {
			return changes, tombstones
		}
		if result.NextWatermark <= since {
			t.Fatalf("pull (device_id=%s) did not advance: since=%d next_watermark=%d has_more=true -- would loop forever", deviceID, since, result.NextWatermark)
		}
		since = result.NextWatermark
	}
	t.Fatalf("pull (device_id=%s) did not terminate after 200 pages starting at since=%d", deviceID, startSince)
	return nil, nil
}

func syncIsoDataItemID(t *testing.T, data json.RawMessage) string {
	t.Helper()
	var v struct {
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decode data.item_id: %v (data: %s)", err, data)
	}
	return v.ItemID
}

func syncIsoDataItemLabel(t *testing.T, data json.RawMessage) (itemID, labelID string) {
	t.Helper()
	var v struct {
		ItemID  string `json:"item_id"`
		LabelID string `json:"label_id"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decode item_label data: %v (data: %s)", err, data)
	}
	return v.ItemID, v.LabelID
}

func syncIsoChangeMatchesTenant(t *testing.T, c syncIsoChangeWire, tn syncIsoTenant) bool {
	t.Helper()
	switch c.EntityType {
	case "item":
		return c.ID == tn.itemID || c.ID == tn.item2ID
	case "item_identification":
		return c.ID == tn.identificationID || c.ID == tn.identification2ID
	case "item_custom_field":
		return c.ID == tn.itemCustomFieldID || c.ID == tn.itemCustomField2ID
	case "stock_adjustment":
		return c.ID == tn.stockAdjustmentID
	case "location":
		return c.ID == tn.locationID || c.ID == tn.location2ID
	case "label":
		return c.ID == tn.labelID || c.ID == tn.label2ID
	case "attachment":
		return c.ID == tn.attachmentID || c.ID == tn.attachment2ID
	case "warranty_block", "sold_to_block", "purchased_from_block":
		itemID := syncIsoDataItemID(t, c.Data)
		return itemID == tn.itemID || itemID == tn.item2ID
	case "item_label":
		itemID, labelID := syncIsoDataItemLabel(t, c.Data)
		return (itemID == tn.itemID || itemID == tn.item2ID) && labelID == tn.labelID
	default:
		t.Fatalf("unrecognised entity_type %q in sync pull response (id=%q)", c.EntityType, c.ID)
		return false
	}
}

func assertSyncIsoEveryEntityTypeOwnPresentOtherAbsent(t *testing.T, label string, tn, other syncIsoTenant, changes []syncIsoChangeWire) {
	t.Helper()
	present := make(map[string]bool, len(syncIsoEntityTypes))
	for _, c := range changes {
		if syncIsoChangeMatchesTenant(t, c, other) {
			t.Errorf("%s's pull leaked %s's %s (id %q) across groups", label, other.name, c.EntityType, c.ID)
		}
		if syncIsoChangeMatchesTenant(t, c, tn) {
			present[c.EntityType] = true
		}
	}
	for _, et := range syncIsoEntityTypes {
		if !present[et] {
			t.Errorf("%s's own pull is missing its own %s row entirely (%d changes total)", label, et, len(changes))
		}
	}
}

func findSyncIsoChangeIDByItemID(t *testing.T, changes []syncIsoChangeWire, entityType, itemID string) string {
	t.Helper()
	for _, c := range changes {
		if c.EntityType != entityType {
			continue
		}
		if syncIsoDataItemID(t, c.Data) == itemID {
			return c.ID
		}
	}
	t.Fatalf("no %s change found with item_id=%q among %d changes", entityType, itemID, len(changes))
	return ""
}

func findSyncIsoChangeIDByItemLabel(t *testing.T, changes []syncIsoChangeWire, itemID, labelID string) string {
	t.Helper()
	for _, c := range changes {
		if c.EntityType != "item_label" {
			continue
		}
		gotItemID, gotLabelID := syncIsoDataItemLabel(t, c.Data)
		if gotItemID == itemID && gotLabelID == labelID {
			return c.ID
		}
	}
	t.Fatalf("no item_label change found with item_id=%q label_id=%q among %d changes", itemID, labelID, len(changes))
	return ""
}

func syncIsoTombstoneListContains(tombstones []syncIsoTombstoneWire, entityType, id string) bool {
	for _, ts := range tombstones {
		if ts.EntityType == entityType && ts.ID == id {
			return true
		}
	}
	return false
}

func medianSyncIsoChangeSeq(t *testing.T, changes []syncIsoChangeWire) int64 {
	t.Helper()
	if len(changes) == 0 {
		t.Fatalf("test bug: no changes to pick a median change_seq from")
	}
	seqs := make([]int64, len(changes))
	for i, c := range changes {
		seqs[i] = c.GroupChangeSeq
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	return seqs[len(seqs)/2]
}
