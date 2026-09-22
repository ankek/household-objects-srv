package sync

import (
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"path/filepath"
	"testing"
)

func newSyncTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(t.TempDir(), "db", "hho.db"), ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func seedGroup(t *testing.T, s *storage.Storage, groupID string) storage.Scope {
	t.Helper()
	if err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    groupID,
		UserID:       groupID + "-owner",
		Username:     groupID + "-owner",
		PasswordHash: "not-a-real-hash",
		Now:          1,
	}); err != nil {
		t.Fatalf("RegisterNewGroup(%s): %v", groupID, err)
	}
	scope, err := s.ForGroup(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%s): %v", groupID, err)
	}
	return scope
}

func readerFor(t *testing.T, s *storage.Storage, groupID string) *Reader {
	t.Helper()
	repo, err := s.ForGroupSync(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupSync(%s): %v", groupID, err)
	}
	return NewReader(repo)
}

func seedItem(t *testing.T, scope storage.Scope, id, name string, now int64) storage.Item {
	t.Helper()
	item, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID: id, Name: name, ShortCode: id, Now: now,
	})
	if err != nil {
		t.Fatalf("Items().Create(%s): %v", id, err)
	}
	return item
}

func seedLocation(t *testing.T, scope storage.Scope, id, name string, now int64) storage.Location {
	t.Helper()
	loc, err := scope.Locations().Create(t.Context(), storage.CreateLocationParams{
		ID: id, Name: name, Now: now,
	})
	if err != nil {
		t.Fatalf("Locations().Create(%s): %v", id, err)
	}
	return loc
}

func TestPullOrdersAcrossTablesByChangeSeq(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-order")

	loc := seedLocation(t, scope, "loc-1", "Garage", 10)
	item := seedItem(t, scope, "item-1", "Drill", 20)

	page, err := readerFor(t, s, "grp-order").Pull(t.Context(), 0, 10)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(page.Changes) != 2 {
		t.Fatalf("Changes = %d entries, want 2: %+v", len(page.Changes), page.Changes)
	}
	if page.Changes[0].EntityType != EntityLocation || page.Changes[0].ID != loc.ID {
		t.Errorf("Changes[0] = %+v, want the location (lower change_seq, allocated first)", page.Changes[0])
	}
	if page.Changes[1].EntityType != EntityItem || page.Changes[1].ID != item.ID {
		t.Errorf("Changes[1] = %+v, want the item (higher change_seq, allocated second)", page.Changes[1])
	}
	if page.Changes[0].GroupChangeSeq >= page.Changes[1].GroupChangeSeq {
		t.Errorf("Changes are not strictly increasing by change_seq: %d then %d",
			page.Changes[0].GroupChangeSeq, page.Changes[1].GroupChangeSeq)
	}
	if page.HasMore {
		t.Errorf("HasMore = true, want false: everything fit in one page")
	}
	if page.NextWatermark != page.Changes[1].GroupChangeSeq {
		t.Errorf("NextWatermark = %d, want %d (the last row actually returned)", page.NextWatermark, page.Changes[1].GroupChangeSeq)
	}
}

func TestPullNextWatermarkNeverSkipsAnUnreturnedRow(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-watermark")

	item1 := seedItem(t, scope, "item-1", "Drill", 10)
	item2 := seedItem(t, scope, "item-2", "Saw", 20)
	item3 := seedItem(t, scope, "item-3", "Hammer", 30)
	item4 := seedItem(t, scope, "item-4", "Wrench", 40)
	loc := seedLocation(t, scope, "loc-1", "Garage", 50)

	if !(item1.ChangeSeq < item2.ChangeSeq && item2.ChangeSeq < item3.ChangeSeq &&
		item3.ChangeSeq < item4.ChangeSeq && item4.ChangeSeq < loc.ChangeSeq) {
		t.Fatalf("fixture is not load-bearing: change_seq must be strictly increasing in creation order, got %d %d %d %d %d",
			item1.ChangeSeq, item2.ChangeSeq, item3.ChangeSeq, item4.ChangeSeq, loc.ChangeSeq)
	}

	r := readerFor(t, s, "grp-watermark")
	page, err := r.Pull(t.Context(), 0, 3)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(page.Changes) != 3 {
		t.Fatalf("Changes = %d entries, want 3: %+v", len(page.Changes), page.Changes)
	}
	wantIDs := []string{item1.ID, item2.ID, item3.ID}
	for i, c := range page.Changes {
		if c.EntityType != EntityItem || c.ID != wantIDs[i] {
			t.Fatalf("Changes[%d] = %+v, want item %q", i, c, wantIDs[i])
		}
	}
	if page.NextWatermark != item3.ChangeSeq {
		t.Fatalf("NextWatermark = %d, want %d (item-3's own change_seq, the last row actually returned); "+
			"a watermark of %d (item-4, truncated away) or %d (the location, fetched from a different "+
			"table's own window) would permanently skip an unreturned row",
			page.NextWatermark, item3.ChangeSeq, item4.ChangeSeq, loc.ChangeSeq)
	}
	if !page.HasMore {
		t.Fatalf("HasMore = false, want true: item-4 and the location both remain")
	}

	next, err := r.Pull(t.Context(), page.NextWatermark, 10)
	if err != nil {
		t.Fatalf("Pull(next): %v", err)
	}
	if len(next.Changes) != 2 {
		t.Fatalf("second page Changes = %d entries, want 2 (item-4, then the location): %+v", len(next.Changes), next.Changes)
	}
	if next.Changes[0].EntityType != EntityItem || next.Changes[0].ID != item4.ID {
		t.Errorf("second page's first entry = %+v, want item-4", next.Changes[0])
	}
	if next.Changes[1].EntityType != EntityLocation || next.Changes[1].ID != loc.ID {
		t.Errorf("second page's second entry = %+v, want the location", next.Changes[1])
	}
	if next.HasMore {
		t.Errorf("second page HasMore = true, want false: nothing remains")
	}
}

func TestPullHasMoreRequiresFetchLimitPlusOne(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-hasmore")

	const limit = 3
	var ids []string
	for i := 0; i < limit+1; i++ {
		it := seedItem(t, scope, "item-"+string(rune('A'+i)), "item", int64(10+i))
		ids = append(ids, it.ID)
	}

	page, err := readerFor(t, s, "grp-hasmore").Pull(t.Context(), 0, limit)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(page.Changes) != limit {
		t.Fatalf("Changes = %d entries, want %d", len(page.Changes), limit)
	}
	if !page.HasMore {
		t.Fatalf("HasMore = false, want true: a %dth item exists beyond this page", limit+1)
	}
	for i, c := range page.Changes {
		if c.ID != ids[i] {
			t.Errorf("Changes[%d].ID = %q, want %q (oldest-first)", i, c.ID, ids[i])
		}
	}

	next, err := readerFor(t, s, "grp-hasmore").Pull(t.Context(), page.NextWatermark, limit)
	if err != nil {
		t.Fatalf("Pull(next): %v", err)
	}
	if len(next.Changes) != 1 || next.Changes[0].ID != ids[limit] {
		t.Fatalf("second page = %+v, want exactly the %dth item (%q)", next.Changes, limit+1, ids[limit])
	}
	if next.HasMore {
		t.Errorf("second page HasMore = true, want false: exactly one row remained and this page returned it")
	}
}

func TestPullEmptyPageWatermarkStaysAtSince(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "grp-empty")

	page, err := readerFor(t, s, "grp-empty").Pull(t.Context(), 42, 10)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(page.Changes) != 0 || len(page.Tombstones) != 0 {
		t.Fatalf("page = %+v, want an empty page", page)
	}
	if page.NextWatermark != 42 {
		t.Errorf("NextWatermark = %d, want 42 (the request's own since, unchanged)", page.NextWatermark)
	}
	if page.HasMore {
		t.Errorf("HasMore = true, want false")
	}
}

func TestPullSplitsTombstonesFromLiveChanges(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-tombstone")

	loc := seedLocation(t, scope, "loc-1", "Garage", 10)
	if err := scope.Locations().Delete(t.Context(), storage.DeleteLocationParams{
		LocationID: loc.ID, Now: 20,
	}); err != nil {
		t.Fatalf("Locations().Delete: %v", err)
	}

	page, err := readerFor(t, s, "grp-tombstone").Pull(t.Context(), 0, 10)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(page.Changes) != 0 {
		t.Fatalf("Changes = %+v, want none: the location's only surviving state is its tombstone", page.Changes)
	}
	if len(page.Tombstones) != 1 {
		t.Fatalf("Tombstones = %d entries, want 1: %+v", len(page.Tombstones), page.Tombstones)
	}
	tomb := page.Tombstones[0]
	if tomb.EntityType != EntityLocation || tomb.ID != loc.ID {
		t.Errorf("Tombstones[0] = %+v, want {%s %s ...}", tomb, EntityLocation, loc.ID)
	}
	if tomb.DeletedAt != 20 {
		t.Errorf("Tombstones[0].DeletedAt = %d, want 20", tomb.DeletedAt)
	}
}

func TestPullDetailBlockEnvelopeIDIsTheRowsOwnID(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-warranty")

	item := seedItem(t, scope, "item-1", "Drill", 10)
	warranty, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: "warranty-row-1", ItemID: item.ID, Holder: "Acme", Now: 20,
	})
	if err != nil {
		t.Fatalf("Warranty().Create: %v", err)
	}
	if warranty.ID == item.ID {
		t.Fatalf("fixture is not load-bearing: warranty row id %q must differ from item id %q", warranty.ID, item.ID)
	}

	page, err := readerFor(t, s, "grp-warranty").Pull(t.Context(), 0, 10)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	var found bool
	for _, c := range page.Changes {
		if c.EntityType != EntityWarrantyBlock {
			continue
		}
		found = true
		if c.ID != warranty.ID {
			t.Errorf("warranty_block Change.ID = %q, want the block's own row id %q, not the item id %q", c.ID, warranty.ID, item.ID)
		}
		data, ok := c.Data.(storage.Warranty)
		if !ok {
			t.Fatalf("warranty_block Change.Data = %T, want storage.Warranty", c.Data)
		}
		if data.ItemID != item.ID {
			t.Errorf("warranty_block Change.Data.ItemID = %q, want %q", data.ItemID, item.ID)
		}
	}
	if !found {
		t.Fatalf("no warranty_block entry in %+v", page.Changes)
	}
}

func TestPullItemLabelEnvelopeIDIsTheAssignmentRowID(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-itemlabel")

	item := seedItem(t, scope, "item-1", "Drill", 10)
	label, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{
		ID: "label-1", Name: "Fragile", Color: "#ff0000", Now: 20,
	})
	if err != nil {
		t.Fatalf("Labels().Create: %v", err)
	}
	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{
		ID: "assignment-1", ItemID: item.ID, LabelID: label.ID, Now: 30,
	}); err != nil {
		t.Fatalf("ItemLabels().Attach: %v", err)
	}

	page, err := readerFor(t, s, "grp-itemlabel").Pull(t.Context(), 0, 10)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	var found bool
	for _, c := range page.Changes {
		if c.EntityType != EntityItemLabel {
			continue
		}
		found = true
		if c.ID != "assignment-1" {
			t.Errorf("item_label Change.ID = %q, want the assignment row's own id %q", c.ID, "assignment-1")
		}
		data, ok := c.Data.(storage.ItemLabelAssignment)
		if !ok {
			t.Fatalf("item_label Change.Data = %T, want storage.ItemLabelAssignment", c.Data)
		}
		if data.ItemID != item.ID || data.LabelID != label.ID {
			t.Errorf("item_label Change.Data = %+v, want ItemID=%q LabelID=%q", data, item.ID, label.ID)
		}
	}
	if !found {
		t.Fatalf("no item_label entry in %+v", page.Changes)
	}
}

func TestPullAttachmentEntryCarriesMetadataOnly(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-attachment")

	item := seedItem(t, scope, "item-1", "Drill", 10)
	att, err := scope.Attachments().Create(t.Context(), storage.CreateAttachmentParams{
		ID: "att-1", ItemID: item.ID, Category: "general",
		OriginalFilename: "manual.pdf", ContentType: "application/pdf",
		SizeBytes: 1024, StoragePath: "grp-attachment/att-1", SHA256: "deadbeef", Now: 20,
	})
	if err != nil {
		t.Fatalf("Attachments().Create: %v", err)
	}

	page, err := readerFor(t, s, "grp-attachment").Pull(t.Context(), 0, 10)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	var found bool
	for _, c := range page.Changes {
		if c.EntityType != EntityAttachment {
			continue
		}
		found = true
		if c.ID != att.ID {
			t.Errorf("attachment Change.ID = %q, want %q", c.ID, att.ID)
		}
		data, ok := c.Data.(storage.Attachment)
		if !ok {
			t.Fatalf("attachment Change.Data = %T, want storage.Attachment", c.Data)
		}
		if data.OriginalFilename != "manual.pdf" || data.SizeBytes != 1024 {
			t.Errorf("attachment Change.Data = %+v, unexpected metadata", data)
		}
	}
	if !found {
		t.Fatalf("no attachment entry in %+v", page.Changes)
	}
}

func TestPullCoversTheWholeClosedEntitySet(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-fullset")

	item := seedItem(t, scope, "item-1", "Drill", 10)

	if _, err := scope.Sale().Create(t.Context(), storage.CreateSaleParams{
		ID: "sale-1", ItemID: item.ID, BuyerName: "Bob", Now: 20,
	}); err != nil {
		t.Fatalf("Sale().Create: %v", err)
	}
	if _, err := scope.Purchase().Create(t.Context(), storage.CreatePurchaseParams{
		ID: "purchase-1", ItemID: item.ID, Vendor: "Acme", Now: 30,
	}); err != nil {
		t.Fatalf("Purchase().Create: %v", err)
	}
	if _, err := scope.Identifications().Create(t.Context(), storage.CreateIdentificationParams{
		ID: "ident-1", ItemID: item.ID, Kind: "serial", Value: "SN123", Now: 40,
	}); err != nil {
		t.Fatalf("Identifications().Create: %v", err)
	}
	text := "blue"
	if _, err := scope.ItemCustomFields().Create(t.Context(), storage.CreateItemCustomFieldParams{
		ID: "field-1", ItemID: item.ID, Name: "Color", FieldType: "text", TextValue: &text, Now: 50,
	}); err != nil {
		t.Fatalf("ItemCustomFields().Create: %v", err)
	}
	if _, err := scope.StockAdjustments().Create(t.Context(), storage.CreateStockAdjustmentParams{
		ID: "adj-1", ItemID: item.ID, Delta: 5, Reason: "restock", Now: 60,
	}); err != nil {
		t.Fatalf("StockAdjustments().Create: %v", err)
	}
	if _, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{
		ID: "label-1", Name: "Fragile", Color: "#ff0000", Now: 70,
	}); err != nil {
		t.Fatalf("Labels().Create: %v", err)
	}

	page, err := readerFor(t, s, "grp-fullset").Pull(t.Context(), 0, 100)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	want := map[EntityType]string{
		EntityItem:               item.ID,
		EntitySoldToBlock:        "sale-1",
		EntityPurchasedFromBlock: "purchase-1",
		EntityItemIdentification: "ident-1",
		EntityItemCustomField:    "field-1",
		EntityStockAdjustment:    "adj-1",
		EntityLabel:              "label-1",
	}
	got := map[EntityType]string{}
	for _, c := range page.Changes {
		if _, dup := got[c.EntityType]; dup {
			t.Errorf("entity_type %q appeared more than once in %+v", c.EntityType, page.Changes)
		}
		got[c.EntityType] = c.ID
	}
	for entityType, wantID := range want {
		gotID, ok := got[entityType]
		if !ok {
			t.Errorf("no %q entry in %+v", entityType, page.Changes)
			continue
		}
		if gotID != wantID {
			t.Errorf("%q Change.ID = %q, want %q", entityType, gotID, wantID)
		}
	}
	if len(got) != len(want) {
		t.Errorf("Changes had %d distinct entity types, want %d: %+v", len(got), len(want), got)
	}
}

func TestPullNeverCrossesAGroupBoundary(t *testing.T) {
	s := newSyncTestStorage(t)
	scopeA := seedGroup(t, s, "grp-a")
	scopeB := seedGroup(t, s, "grp-b")

	itemA := seedItem(t, scopeA, "item-a", "A's drill", 10)
	itemB := seedItem(t, scopeB, "item-b", "B's drill", 10)

	pageA, err := readerFor(t, s, "grp-a").Pull(t.Context(), 0, 10)
	if err != nil {
		t.Fatalf("Pull(grp-a): %v", err)
	}
	pageB, err := readerFor(t, s, "grp-b").Pull(t.Context(), 0, 10)
	if err != nil {
		t.Fatalf("Pull(grp-b): %v", err)
	}

	for _, c := range pageA.Changes {
		if c.ID == itemB.ID {
			t.Fatalf("group A's pull returned group B's item %q", itemB.ID)
		}
	}
	for _, c := range pageB.Changes {
		if c.ID == itemA.ID {
			t.Fatalf("group B's pull returned group A's item %q", itemA.ID)
		}
	}
	if len(pageA.Changes) != 1 || pageA.Changes[0].ID != itemA.ID {
		t.Fatalf("group A's pull = %+v, want exactly its own item", pageA.Changes)
	}
	if len(pageB.Changes) != 1 || pageB.Changes[0].ID != itemB.ID {
		t.Fatalf("group B's pull = %+v, want exactly its own item", pageB.Changes)
	}
}

func TestPullRejectsInvalidLimitAndSince(t *testing.T) {
	s := newSyncTestStorage(t)
	seedGroup(t, s, "grp-validate")
	r := readerFor(t, s, "grp-validate")

	cases := []struct {
		name    string
		since   int64
		limit   int64
		wantErr error
	}{
		{"zero limit", 0, 0, ErrInvalidLimit},
		{"negative limit", 0, -1, ErrInvalidLimit},
		{"negative since", -1, 10, ErrInvalidSince},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Pull(t.Context(), tc.since, tc.limit)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Pull(since=%d, limit=%d) error = %v, want %v", tc.since, tc.limit, err, tc.wantErr)
			}
		})
	}
}

func TestPullRefusesANilRepository(t *testing.T) {
	r := NewReader(nil)
	if _, err := r.Pull(t.Context(), 0, 10); !errors.Is(err, ErrNoRepository) {
		t.Fatalf("Pull on a nil-repository Reader: error = %v, want ErrNoRepository", err)
	}
}

func seedIdentification(t *testing.T, scope storage.Scope, id, itemID, value string, now int64) storage.Identification {
	t.Helper()
	ident, err := scope.Identifications().Create(t.Context(), storage.CreateIdentificationParams{
		ID: id, ItemID: itemID, Kind: "serial", Value: value, Now: now,
	})
	if err != nil {
		t.Fatalf("Identifications().Create(%s): %v", id, err)
	}
	return ident
}

func collectAllPages(t *testing.T, r *Reader, limit int64) ([]Change, []Tombstone) {
	t.Helper()
	var changes []Change
	var tombstones []Tombstone
	since := int64(0)
	for pageNum := 0; ; pageNum++ {
		if pageNum > 1000 {
			t.Fatalf("collectAllPages: did not converge after 1000 pages; HasMore is stuck true")
		}
		page, err := r.Pull(t.Context(), since, limit)
		if err != nil {
			t.Fatalf("Pull(since=%d, limit=%d): %v", since, limit, err)
		}
		changes = append(changes, page.Changes...)
		tombstones = append(tombstones, page.Tombstones...)
		if !page.HasMore {
			return changes, tombstones
		}
		if page.NextWatermark == since {
			t.Fatalf("Pull(since=%d): HasMore=true but NextWatermark did not advance; this would loop forever", since)
		}
		since = page.NextWatermark
	}
}

func idsOf(changes []Change, tombstones []Tombstone) map[string]bool {
	out := map[string]bool{}
	for _, c := range changes {
		out[string(c.EntityType)+":"+c.ID] = true
	}
	for _, tomb := range tombstones {
		out[string(tomb.EntityType)+":"+tomb.ID] = true
	}
	return out
}

func TestPullTieGroupSplitAcrossPagesIsNeverLost(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-tie-repro")

	item := seedItem(t, scope, "item-1", "Drill", 10)
	warranty, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: "warranty-1", ItemID: item.ID, Holder: "Acme", Now: 20,
	})
	if err != nil {
		t.Fatalf("Warranty().Create: %v", err)
	}
	if err := scope.Items().Delete(t.Context(), item.ID, 30); err != nil {
		t.Fatalf("Items().Delete: %v", err)
	}

	whole, err := readerFor(t, s, "grp-tie-repro").Pull(t.Context(), 0, 1000)
	if err != nil {
		t.Fatalf("Pull(limit=1000): %v", err)
	}
	if len(whole.Changes) != 0 || len(whole.Tombstones) != 2 {
		t.Fatalf("fixture is not load-bearing: single unbounded pull = %d changes, %d tombstones, want 0 and 2: %+v / %+v",
			len(whole.Changes), len(whole.Tombstones), whole.Changes, whole.Tombstones)
	}
	wantIDs := idsOf(whole.Changes, whole.Tombstones)
	if !wantIDs["item:"+item.ID] || !wantIDs["warranty_block:"+warranty.ID] {
		t.Fatalf("fixture is not load-bearing: ground-truth tombstones = %+v, want item %q and warranty_block %q", whole.Tombstones, item.ID, warranty.ID)
	}

	pagedChanges, pagedTombstones := collectAllPages(t, readerFor(t, s, "grp-tie-repro"), 1)
	gotIDs := idsOf(pagedChanges, pagedTombstones)

	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("paged collection has %d distinct entries, want %d: got %v, want %v", len(gotIDs), len(wantIDs), gotIDs, wantIDs)
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Errorf("paged collection is missing %q -- lost across a page boundary inside its own change_seq group", id)
		}
	}
}

func TestPullBacksOffBeforeSplittingALaterGroup(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-backoff")

	item1 := seedItem(t, scope, "item-1", "Drill", 10)

	item2 := seedItem(t, scope, "item-2", "Saw", 20)
	warranty2, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: "warranty-2", ItemID: item2.ID, Holder: "Acme", Now: 30,
	})
	if err != nil {
		t.Fatalf("Warranty().Create: %v", err)
	}
	if err := scope.Items().Delete(t.Context(), item2.ID, 40); err != nil {
		t.Fatalf("Items().Delete(item-2): %v", err)
	}

	r := readerFor(t, s, "grp-backoff")
	page, err := r.Pull(t.Context(), 0, 2)
	if err != nil {
		t.Fatalf("Pull(limit=2): %v", err)
	}
	if len(page.Changes) != 1 || len(page.Tombstones) != 0 {
		t.Fatalf("page = %d changes, %d tombstones, want exactly item-1 alone (1 change, 0 tombstones) -- "+
			"a page of 2 would mean the item-2/warranty-2 tie group got split: %+v / %+v",
			len(page.Changes), len(page.Tombstones), page.Changes, page.Tombstones)
	}
	if page.Changes[0].ID != item1.ID {
		t.Fatalf("page.Changes[0].ID = %q, want %q", page.Changes[0].ID, item1.ID)
	}
	if page.NextWatermark != item1.ChangeSeq {
		t.Fatalf("NextWatermark = %d, want %d (item-1's own change_seq, strictly before the excluded tie group)", page.NextWatermark, item1.ChangeSeq)
	}
	if !page.HasMore {
		t.Fatalf("HasMore = false, want true: the item-2/warranty-2 tombstone group was deliberately excluded")
	}

	next, err := r.Pull(t.Context(), page.NextWatermark, 10)
	if err != nil {
		t.Fatalf("Pull(next): %v", err)
	}
	if len(next.Changes) != 0 || len(next.Tombstones) != 2 {
		t.Fatalf("second page = %d changes, %d tombstones, want 0 and 2 (item-2 and warranty-2 tombstones): %+v / %+v",
			len(next.Changes), len(next.Tombstones), next.Changes, next.Tombstones)
	}
	got := idsOf(next.Changes, next.Tombstones)
	if !got["item:"+item2.ID] || !got["warranty_block:"+warranty2.ID] {
		t.Fatalf("second page tombstones = %+v, want item %q and warranty_block %q", next.Tombstones, item2.ID, warranty2.ID)
	}
	if next.HasMore {
		t.Errorf("second page HasMore = true, want false: nothing remains")
	}
}

func TestPullOversizedGroupExceedsLimitButIsComplete(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-oversized")

	item := seedItem(t, scope, "item-1", "Drill", 10)
	warranty, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: "warranty-1", ItemID: item.ID, Holder: "Acme", Now: 20,
	})
	if err != nil {
		t.Fatalf("Warranty().Create: %v", err)
	}
	ident1 := seedIdentification(t, scope, "ident-1", item.ID, "SN1", 30)
	ident2 := seedIdentification(t, scope, "ident-2", item.ID, "SN2", 40)
	ident3 := seedIdentification(t, scope, "ident-3", item.ID, "SN3", 50)

	if err := scope.Items().Delete(t.Context(), item.ID, 60); err != nil {
		t.Fatalf("Items().Delete: %v", err)
	}

	page, err := readerFor(t, s, "grp-oversized").Pull(t.Context(), 0, 1)
	if err != nil {
		t.Fatalf("Pull(limit=1): %v", err)
	}
	if len(page.Changes) != 0 {
		t.Fatalf("Changes = %+v, want none: every row in this fixture's only group is a tombstone", page.Changes)
	}
	const wantGroupSize = 5
	if len(page.Tombstones) != wantGroupSize {
		t.Fatalf("Tombstones = %d entries, want %d (limit=1 must not truncate a single group with no earlier boundary): %+v",
			len(page.Tombstones), wantGroupSize, page.Tombstones)
	}
	got := idsOf(nil, page.Tombstones)
	for _, want := range []struct {
		entityType EntityType
		id         string
	}{
		{EntityItem, item.ID},
		{EntityWarrantyBlock, warranty.ID},
		{EntityItemIdentification, ident1.ID},
		{EntityItemIdentification, ident2.ID},
		{EntityItemIdentification, ident3.ID},
	} {
		if !got[string(want.entityType)+":"+want.id] {
			t.Errorf("Tombstones is missing %s %q: %+v", want.entityType, want.id, page.Tombstones)
		}
	}
	if page.HasMore {
		t.Errorf("HasMore = true, want false: this fixture's only group is exactly what this page returned")
	}
	for _, tomb := range page.Tombstones {
		if tomb.DeletedAt != 60 {
			t.Errorf("Tombstones[...].DeletedAt = %d, want 60 for %+v", tomb.DeletedAt, tomb)
		}
	}
}

func TestPullAtSeqCompletesATableLargerThanFetchLimit(t *testing.T) {
	s := newSyncTestStorage(t)
	scope := seedGroup(t, s, "grp-atseq")

	item := seedItem(t, scope, "item-1", "Drill", 10)
	const identCount = 6
	wantIdentIDs := make([]string, 0, identCount)
	for i := 0; i < identCount; i++ {
		id := "ident-" + string(rune('A'+i))
		seedIdentification(t, scope, id, item.ID, id, int64(20+i))
		wantIdentIDs = append(wantIdentIDs, id)
	}
	if err := scope.Items().Delete(t.Context(), item.ID, 100); err != nil {
		t.Fatalf("Items().Delete: %v", err)
	}

	const limit = 2
	page, err := readerFor(t, s, "grp-atseq").Pull(t.Context(), 0, limit)
	if err != nil {
		t.Fatalf("Pull(limit=%d): %v", limit, err)
	}
	if len(page.Changes) != 0 {
		t.Fatalf("Changes = %+v, want none", page.Changes)
	}
	const wantGroupSize = 1 + identCount
	if len(page.Tombstones) != wantGroupSize {
		t.Fatalf("Tombstones = %d entries, want %d (all %d identifications plus the item, despite fetchLimit=%d): %+v",
			len(page.Tombstones), wantGroupSize, identCount, limit+1, page.Tombstones)
	}
	got := idsOf(nil, page.Tombstones)
	if !got["item:"+item.ID] {
		t.Errorf("Tombstones is missing the item %q: %+v", item.ID, page.Tombstones)
	}
	for _, id := range wantIdentIDs {
		if !got["item_identification:"+id] {
			t.Errorf("Tombstones is missing identification %q (fetchLimit=%d could only see %d of %d from this table alone): %+v",
				id, limit+1, limit+1, identCount, page.Tombstones)
		}
	}
	if page.HasMore {
		t.Errorf("HasMore = true, want false: nothing remains beyond this one oversized group")
	}
}
