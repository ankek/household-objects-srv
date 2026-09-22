package storage

import (
	"errors"
	"sort"
	"testing"
	"time"
)

func attachmentScope(t *testing.T) (scopeA, scopeB Scope) {
	t.Helper()
	s := newTestStorage(t)
	seedItem(t, s, "groupA", "itemA", 10)
	seedItem(t, s, "groupA", "itemA2", 11)
	seedItem(t, s, "groupB", "itemB", 20)

	scopeA, err := s.ForGroup(MustGroupID("groupA"))
	if err != nil {
		t.Fatalf("ForGroup(groupA): %v", err)
	}
	scopeB, err = s.ForGroup(MustGroupID("groupB"))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	return scopeA, scopeB
}

func validCreateAttachmentParams(id, itemID string) CreateAttachmentParams {
	return CreateAttachmentParams{
		ID:               id,
		ItemID:           itemID,
		Category:         "image",
		OriginalFilename: "receipt.jpg",
		ContentType:      "image/jpeg",
		SizeBytes:        1234,
		StoragePath:      "groupA/" + id,
		SHA256:           "deadbeef",
		Now:              time.Now().UnixMilli(),
	}
}

func TestAttachmentCreateStoresMetadataAndStartsAtVersionOne(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	p := validCreateAttachmentParams("att-1", "itemA")

	created, err := scopeA.Attachments().Create(t.Context(), p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Version != 1 {
		t.Errorf("Version = %d, want 1", created.Version)
	}
	if created.ChangeSeq <= 0 {
		t.Errorf("ChangeSeq = %d, want a positive allocated sequence (FR-090)", created.ChangeSeq)
	}
	if created.Category != p.Category {
		t.Errorf("Category = %q, want %q", created.Category, p.Category)
	}
	if created.OriginalFilename != p.OriginalFilename {
		t.Errorf("OriginalFilename = %q, want %q", created.OriginalFilename, p.OriginalFilename)
	}
	if created.ContentType != p.ContentType {
		t.Errorf("ContentType = %q, want %q", created.ContentType, p.ContentType)
	}
	if created.SizeBytes != p.SizeBytes {
		t.Errorf("SizeBytes = %d, want %d", created.SizeBytes, p.SizeBytes)
	}
	if created.StoragePath != p.StoragePath {
		t.Errorf("StoragePath = %q, want %q", created.StoragePath, p.StoragePath)
	}
	if created.Sha256 != p.SHA256 {
		t.Errorf("Sha256 = %q, want %q", created.Sha256, p.SHA256)
	}
	if created.ThumbnailPath.Valid {
		t.Errorf("ThumbnailPath = %+v, want NULL -- T083 never sets it (T085's own job)", created.ThumbnailPath)
	}
	if created.GroupID != "groupA" {
		t.Errorf("GroupID = %q, want groupA", created.GroupID)
	}
}

func TestAttachmentCreateAcceptsEveryCategory(t *testing.T) {
	for _, category := range []string{"image", "manual", "warranty", "receipt", "general"} {
		t.Run(category, func(t *testing.T) {
			scopeA, _ := attachmentScope(t)
			p := validCreateAttachmentParams("att-"+category, "itemA")
			p.Category = category

			created, err := scopeA.Attachments().Create(t.Context(), p)
			if err != nil {
				t.Fatalf("Create(category=%q): %v", category, err)
			}
			if created.Category != category {
				t.Errorf("Category = %q, want %q", created.Category, category)
			}
		})
	}
}

func TestAttachmentCreateRejectsAnInvalidCategory(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	p := validCreateAttachmentParams("att-bad-category", "itemA")
	p.Category = "not-a-real-category"

	_, err := scopeA.Attachments().Create(t.Context(), p)
	if !errors.Is(err, ErrCategoryInvalid) {
		t.Fatalf("Create(category=%q) error = %v, want ErrCategoryInvalid", p.Category, err)
	}
}

func TestAttachmentCreateRejectsAnItemFromAnotherGroup(t *testing.T) {
	scopeA, scopeB := attachmentScope(t)

	_, err := scopeB.Attachments().Create(t.Context(), validCreateAttachmentParams("att-x", "itemA"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Create on groupA's item = %v, want ErrNotFound (P-3)", err)
	}

	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-a", "itemA")); err != nil {
		t.Fatalf("groupA Create on its OWN item = %v, want success; without this the refusal above proves nothing about scope", err)
	}
}

func TestAttachmentCreateRejectsAnUnknownItem(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	_, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-unknown", "does-not-exist"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Create against an unknown item = %v, want ErrNotFound", err)
	}
}

func TestAttachmentCreateAllowsMultipleRowsOnTheSameItem(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	p2 := validCreateAttachmentParams("att-2", "itemA")
	p2.Category = "manual"
	if _, err := scopeA.Attachments().Create(t.Context(), p2); err != nil {
		t.Fatalf("second Create (different category, same item) = %v, want success -- FR-040 is 0..N", err)
	}
}

func TestAttachmentCreateRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	cases := []struct {
		name   string
		mutate func(p *CreateAttachmentParams)
	}{
		{"empty ID", func(p *CreateAttachmentParams) { p.ID = "" }},
		{"empty ItemID", func(p *CreateAttachmentParams) { p.ItemID = "" }},
		{"empty OriginalFilename", func(p *CreateAttachmentParams) { p.OriginalFilename = "" }},
		{"empty ContentType", func(p *CreateAttachmentParams) { p.ContentType = "" }},
		{"empty StoragePath", func(p *CreateAttachmentParams) { p.StoragePath = "" }},
		{"empty SHA256", func(p *CreateAttachmentParams) { p.SHA256 = "" }},
		{"negative SizeBytes", func(p *CreateAttachmentParams) { p.SizeBytes = -1 }},
		{"zero Now", func(p *CreateAttachmentParams) { p.Now = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validCreateAttachmentParams("att-incomplete", "itemA")
			tc.mutate(&p)
			if _, err := scopeA.Attachments().Create(t.Context(), p); err == nil {
				t.Errorf("Create(%s) succeeded, want a validation error", tc.name)
			}
		})
	}
}

func TestSetThumbnailPathRecordsThePathAndBumpsVersion(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	created, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ThumbnailPath.Valid {
		t.Fatalf("newly created attachment already has ThumbnailPath = %+v, want NULL", created.ThumbnailPath)
	}

	now := time.Now().UnixMilli()
	updated, err := scopeA.Attachments().SetThumbnailPath(t.Context(), "itemA", "att-1", "groupA/att-1-thumb.jpg", now)
	if err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}
	if !updated.ThumbnailPath.Valid || updated.ThumbnailPath.String != "groupA/att-1-thumb.jpg" {
		t.Errorf("ThumbnailPath = %+v, want valid \"groupA/att-1-thumb.jpg\"", updated.ThumbnailPath)
	}
	if updated.Version != created.Version+1 {
		t.Errorf("Version = %d, want %d (one more than the pre-thumbnail row's %d)", updated.Version, created.Version+1, created.Version)
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want strictly greater than the pre-thumbnail row's %d (FR-090)", updated.ChangeSeq, created.ChangeSeq)
	}
	if updated.UpdatedAt < updated.CreatedAt {
		t.Errorf("UpdatedAt = %d, want >= CreatedAt = %d", updated.UpdatedAt, updated.CreatedAt)
	}

	if updated.ID != "att-1" || updated.ItemID != "itemA" || updated.GroupID != "groupA" {
		t.Errorf("SetThumbnailPath returned a row for the wrong attachment: %+v", updated)
	}
}

func TestSetThumbnailPathRejectsAnAttachmentFromAnotherGroup(t *testing.T) {
	scopeA, scopeB := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := scopeB.Attachments().SetThumbnailPath(t.Context(), "itemA", "att-1", "groupA/att-1-thumb.jpg", time.Now().UnixMilli())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB SetThumbnailPath on groupA's attachment = %v, want ErrNotFound (P-3)", err)
	}
}

func TestSetThumbnailPathRejectsAnAttachmentFromAnotherItem(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := scopeA.Attachments().SetThumbnailPath(t.Context(), "itemA2", "att-1", "groupA/att-1-thumb.jpg", time.Now().UnixMilli())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetThumbnailPath against the wrong item = %v, want ErrNotFound", err)
	}
}

func TestSetThumbnailPathRejectsAnUnknownID(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	_, err := scopeA.Attachments().SetThumbnailPath(t.Context(), "itemA", "does-not-exist", "groupA/does-not-exist-thumb.jpg", time.Now().UnixMilli())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetThumbnailPath against an unknown id = %v, want ErrNotFound", err)
	}
}

func TestSetThumbnailPathRejectsIncompleteArguments(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	cases := []struct {
		name          string
		itemID        string
		id            string
		thumbnailPath string
		now           int64
	}{
		{"empty itemID", "", "att-1", "groupA/att-1-thumb.jpg", time.Now().UnixMilli()},
		{"empty id", "itemA", "", "groupA/att-1-thumb.jpg", time.Now().UnixMilli()},
		{"empty thumbnailPath", "itemA", "att-1", "", time.Now().UnixMilli()},
		{"zero now", "itemA", "att-1", "groupA/att-1-thumb.jpg", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := scopeA.Attachments().SetThumbnailPath(t.Context(), tc.itemID, tc.id, tc.thumbnailPath, tc.now); err == nil {
				t.Errorf("SetThumbnailPath(%s) succeeded, want a validation error", tc.name)
			}
		})
	}
}

func TestAttachmentGetReturnsTheStoredRow(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	created, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := scopeA.Attachments().Get(t.Context(), "itemA", "att-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != created {
		t.Errorf("Get() = %+v, want %+v (the row Create returned)", got, created)
	}
}

func TestAttachmentGetRejectsAnAttachmentFromAnotherGroup(t *testing.T) {
	scopeA, scopeB := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := scopeB.Attachments().Get(t.Context(), "itemA", "att-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Get of groupA's attachment = %v, want ErrNotFound (P-3)", err)
	}
}

func TestAttachmentGetRejectsAnAttachmentFromAnotherItem(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := scopeA.Attachments().Get(t.Context(), "itemA2", "att-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get against the wrong item = %v, want ErrNotFound", err)
	}
}

func TestAttachmentGetRejectsAnUnknownID(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	_, err := scopeA.Attachments().Get(t.Context(), "itemA", "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get against an unknown id = %v, want ErrNotFound", err)
	}
}

func TestAttachmentGetRejectsATombstonedRow(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	sa := scopeA.(groupScope)
	created, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := sa.store.Writer().ExecContext(t.Context(),
		`UPDATE attachments SET deleted_at = ? WHERE group_id = ? AND id = ?`,
		time.Now().UnixMilli(), "groupA", created.ID,
	); err != nil {
		t.Fatalf("tombstone attachment directly: %v", err)
	}

	_, err = scopeA.Attachments().Get(t.Context(), "itemA", "att-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get against a tombstoned row = %v, want ErrNotFound", err)
	}
}

func TestAttachmentDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := scopeA.Attachments().Delete(t.Context(), "itemA", created.ID, now+1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.Attachments().Get(t.Context(), "itemA", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.Attachments().Delete(t.Context(), "itemA", created.ID, now+2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}

	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-2", "itemA")); err != nil {
		t.Fatalf("Create on the same item after Delete = %v, want success", err)
	}
}

func TestAttachmentDeleteRejectsAnAttachmentFromAnotherGroup(t *testing.T) {
	scopeA, scopeB := attachmentScope(t)
	created, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := scopeB.Attachments().Delete(t.Context(), "itemA", created.ID, time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Delete of groupA's attachment = %v, want ErrNotFound", err)
	}
	if _, err := scopeA.Attachments().Get(t.Context(), "itemA", created.ID); err != nil {
		t.Fatalf("groupA Get after groupB's delete attempt = %v, want the row still there", err)
	}
}

func TestAttachmentDeleteRejectsAnAttachmentFromAnotherItem(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	created, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA"))
	if err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}

	if err := scopeA.Attachments().Delete(t.Context(), "itemA2", created.ID, time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA Delete naming itemA2 for itemA's attachment = %v, want ErrNotFound", err)
	}
	if _, err := scopeA.Attachments().Get(t.Context(), "itemA", created.ID); err != nil {
		t.Fatalf("groupA Get (naming the correct item) after the cross-item delete attempt: %v", err)
	}
}

func TestAttachmentDeleteRejectsAnUnknownID(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	if err := scopeA.Attachments().Delete(t.Context(), "itemA", "does-not-exist", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete against an unknown id = %v, want ErrNotFound", err)
	}
}

func TestAttachmentDeleteRemovesExactlyOneRow(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	now := time.Now().UnixMilli()

	categories := []string{"image", "manual", "receipt"}
	for i, category := range categories {
		p := validCreateAttachmentParams("att-"+category, "itemA")
		p.Category = category
		p.Now = now + int64(i)
		if _, err := scopeA.Attachments().Create(t.Context(), p); err != nil {
			t.Fatalf("Create %s: %v", category, err)
		}
	}

	if err := scopeA.Attachments().Delete(t.Context(), "itemA", "att-image", now+10); err != nil {
		t.Fatalf("Delete att-image: %v", err)
	}

	if _, err := scopeA.Attachments().Get(t.Context(), "itemA", "att-image"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get att-image after its own delete = %v, want ErrNotFound", err)
	}
	for _, category := range []string{"manual", "receipt"} {
		id := "att-" + category
		if _, err := scopeA.Attachments().Get(t.Context(), "itemA", id); err != nil {
			t.Errorf("Get %s after deleting a DIFFERENT attachment = %v, want it to still be live -- the delete matched more than the row it named", id, err)
		}
	}
}

func TestAttachmentDeleteRejectsIncompleteArguments(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	cases := []struct {
		name   string
		itemID string
		id     string
		now    int64
	}{
		{"empty itemID", "", "att-1", time.Now().UnixMilli()},
		{"empty id", "itemA", "", time.Now().UnixMilli()},
		{"zero now", "itemA", "att-1", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := scopeA.Attachments().Delete(t.Context(), tc.itemID, tc.id, tc.now); err == nil {
				t.Errorf("Delete(%s) succeeded, want a validation error", tc.name)
			}
		})
	}
}

func attachmentIDs(rows []Attachment) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestAttachmentListAllIsEmptyForAFreshGroup(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	rows, err := scopeA.Attachments().ListAll(t.Context())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListAll on a fresh group returned %d rows, want 0", len(rows))
	}
}

func TestAttachmentListAllReturnsLiveAndTombstonedRows(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-live", "itemA")); err != nil {
		t.Fatalf("Create att-live: %v", err)
	}
	tombstoned := validCreateAttachmentParams("att-dead", "itemA")
	tombstoned.Now = now
	if _, err := scopeA.Attachments().Create(t.Context(), tombstoned); err != nil {
		t.Fatalf("Create att-dead: %v", err)
	}
	if err := scopeA.Attachments().Delete(t.Context(), "itemA", "att-dead", now+1); err != nil {
		t.Fatalf("Delete att-dead: %v", err)
	}

	rows, err := scopeA.Attachments().ListAll(t.Context())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if got := attachmentIDs(rows); len(got) != 2 || got[0] != "att-dead" || got[1] != "att-live" {
		t.Fatalf("ListAll ids = %v, want [att-dead att-live] -- the tombstoned row must still be present", got)
	}
	for _, r := range rows {
		switch r.ID {
		case "att-live":
			if r.DeletedAt.Valid {
				t.Errorf("att-live has DeletedAt = %+v, want NULL", r.DeletedAt)
			}
		case "att-dead":
			if !r.DeletedAt.Valid {
				t.Errorf("att-dead has DeletedAt = %+v, want a tombstone instant -- ListAll must not filter it out", r.DeletedAt)
			}
		}
	}
}

func TestAttachmentListAllOnlyReturnsThisGroupsRows(t *testing.T) {
	scopeA, scopeB := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-a", "itemA")); err != nil {
		t.Fatalf("Create in groupA: %v", err)
	}
	bParams := validCreateAttachmentParams("att-b", "itemB")
	bParams.StoragePath = "groupB/att-b"
	if _, err := scopeB.Attachments().Create(t.Context(), bParams); err != nil {
		t.Fatalf("Create in groupB: %v", err)
	}

	rowsA, err := scopeA.Attachments().ListAll(t.Context())
	if err != nil {
		t.Fatalf("groupA ListAll: %v", err)
	}
	if got := attachmentIDs(rowsA); len(got) != 1 || got[0] != "att-a" {
		t.Fatalf("groupA ListAll = %v, want [att-a] -- group B's attachment leaked across tenants", got)
	}

	rowsB, err := scopeB.Attachments().ListAll(t.Context())
	if err != nil {
		t.Fatalf("groupB ListAll: %v", err)
	}
	if got := attachmentIDs(rowsB); len(got) != 1 || got[0] != "att-b" {
		t.Fatalf("groupB ListAll = %v, want [att-b] -- group A's attachment leaked across tenants", got)
	}
}

func TestAttachmentListForItemRejectsAnotherGroupsItem(t *testing.T) {
	scopeA, scopeB := attachmentScope(t)

	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scopeB.Attachments().ListForItem(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB ListForItem of groupA's item = %v, want ErrNotFound", err)
	}
	rows, err := scopeA.Attachments().ListForItem(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("groupA ListForItem of its OWN item = %v, want success", err)
	}
	if got := attachmentIDs(rows); len(got) != 1 || got[0] != "att-1" {
		t.Fatalf("groupA's own list = %v, want exactly [att-1]", got)
	}
}

func TestAttachmentListForItemOnAnItemWithNoAttachmentsIsAnEmptySlice(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	rows, err := scopeA.Attachments().ListForItem(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("ListForItem on an item with no attachments = %v, want success", err)
	}
	if len(rows) != 0 {
		t.Errorf("ListForItem = %+v, want an empty slice", rows)
	}

	if _, err := scopeA.Attachments().ListForItem(t.Context(), "no-such-item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListForItem on an unknown item = %v, want ErrNotFound", err)
	}
}

func TestAttachmentListForItemExcludesTombstonedAndOtherItemsRows(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-live", "itemA")); err != nil {
		t.Fatalf("Create att-live on itemA: %v", err)
	}
	tombstoned := validCreateAttachmentParams("att-dead", "itemA")
	tombstoned.Now = now
	if _, err := scopeA.Attachments().Create(t.Context(), tombstoned); err != nil {
		t.Fatalf("Create att-dead on itemA: %v", err)
	}
	if err := scopeA.Attachments().Delete(t.Context(), "itemA", "att-dead", now+1); err != nil {
		t.Fatalf("Delete att-dead: %v", err)
	}
	otherItem := validCreateAttachmentParams("att-other-item", "itemA2")
	if _, err := scopeA.Attachments().Create(t.Context(), otherItem); err != nil {
		t.Fatalf("Create att-other-item on itemA2: %v", err)
	}

	rows, err := scopeA.Attachments().ListForItem(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("ListForItem(itemA): %v", err)
	}
	if got := attachmentIDs(rows); len(got) != 1 || got[0] != "att-live" {
		t.Fatalf("ListForItem(itemA) = %v, want exactly [att-live] -- the tombstoned row and/or itemA2's row leaked", got)
	}

	otherRows, err := scopeA.Attachments().ListForItem(t.Context(), "itemA2")
	if err != nil {
		t.Fatalf("ListForItem(itemA2): %v", err)
	}
	if got := attachmentIDs(otherRows); len(got) != 1 || got[0] != "att-other-item" {
		t.Fatalf("ListForItem(itemA2) = %v, want exactly [att-other-item]", got)
	}
}

func TestAttachmentListForItemOrdersByCreatedAtThenID(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	base := time.Now().UnixMilli()

	third := validCreateAttachmentParams("att-c", "itemA")
	third.Now = base + 20
	if _, err := scopeA.Attachments().Create(t.Context(), third); err != nil {
		t.Fatalf("Create att-c: %v", err)
	}
	firstOfTie := validCreateAttachmentParams("att-a", "itemA")
	firstOfTie.Now = base
	if _, err := scopeA.Attachments().Create(t.Context(), firstOfTie); err != nil {
		t.Fatalf("Create att-a: %v", err)
	}
	secondOfTie := validCreateAttachmentParams("att-b", "itemA")
	secondOfTie.Now = base
	if _, err := scopeA.Attachments().Create(t.Context(), secondOfTie); err != nil {
		t.Fatalf("Create att-b: %v", err)
	}

	rows, err := scopeA.Attachments().ListForItem(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("ListForItem: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("ListForItem = %d rows, want 3", len(rows))
	}
	var gotOrder []string
	for _, r := range rows {
		gotOrder = append(gotOrder, r.ID)
	}
	if wantOrder := []string{"att-a", "att-b", "att-c"}; gotOrder[0] != wantOrder[0] || gotOrder[1] != wantOrder[1] || gotOrder[2] != wantOrder[2] {
		t.Fatalf("order = %v, want %v (created_at ASC, id ASC)", gotOrder, wantOrder)
	}
}

func TestClearThumbnailPathClearsThePathAndBumpsVersion(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	created, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	withThumb, err := scopeA.Attachments().SetThumbnailPath(t.Context(), "itemA", "att-1", "groupA/att-1-thumb.jpg", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	if err := scopeA.Attachments().ClearThumbnailPath(t.Context(), "itemA", "att-1", time.Now().UnixMilli()); err != nil {
		t.Fatalf("ClearThumbnailPath: %v", err)
	}

	got, err := scopeA.Attachments().Get(t.Context(), "itemA", "att-1")
	if err != nil {
		t.Fatalf("Get after ClearThumbnailPath: %v", err)
	}
	if got.ThumbnailPath.Valid {
		t.Errorf("ThumbnailPath = %+v, want NULL after ClearThumbnailPath", got.ThumbnailPath)
	}
	if got.StoragePath != created.StoragePath {
		t.Errorf("StoragePath = %q, want unchanged %q -- ClearThumbnailPath must never touch the original", got.StoragePath, created.StoragePath)
	}
	if got.DeletedAt.Valid {
		t.Errorf("DeletedAt = %+v, want NULL -- ClearThumbnailPath must never tombstone the row", got.DeletedAt)
	}
	if got.Version != withThumb.Version+1 {
		t.Errorf("Version = %d, want %d (one more than the with-thumbnail row's %d)", got.Version, withThumb.Version+1, withThumb.Version)
	}
	if got.ChangeSeq <= withThumb.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want strictly greater than %d (FR-090)", got.ChangeSeq, withThumb.ChangeSeq)
	}
}

func TestClearThumbnailPathRemovesExactlyOneRowsPath(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	now := time.Now().UnixMilli()

	ids := []string{"att-1", "att-2", "att-3"}
	for i, id := range ids {
		p := validCreateAttachmentParams(id, "itemA")
		p.Now = now + int64(i)
		if _, err := scopeA.Attachments().Create(t.Context(), p); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		if _, err := scopeA.Attachments().SetThumbnailPath(t.Context(), "itemA", id, "groupA/"+id+"-thumb.jpg", now+10+int64(i)); err != nil {
			t.Fatalf("SetThumbnailPath %s: %v", id, err)
		}
	}

	if err := scopeA.Attachments().ClearThumbnailPath(t.Context(), "itemA", "att-1", now+20); err != nil {
		t.Fatalf("ClearThumbnailPath att-1: %v", err)
	}

	got1, err := scopeA.Attachments().Get(t.Context(), "itemA", "att-1")
	if err != nil {
		t.Fatalf("Get att-1: %v", err)
	}
	if got1.ThumbnailPath.Valid {
		t.Errorf("att-1 ThumbnailPath = %+v, want NULL", got1.ThumbnailPath)
	}
	for _, id := range []string{"att-2", "att-3"} {
		got, err := scopeA.Attachments().Get(t.Context(), "itemA", id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if !got.ThumbnailPath.Valid {
			t.Errorf("%s ThumbnailPath = %+v, want still set -- ClearThumbnailPath matched more than the row it named", id, got.ThumbnailPath)
		}
	}
}

func TestClearThumbnailPathRejectsARowWithNoThumbnailSet(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := scopeA.Attachments().ClearThumbnailPath(t.Context(), "itemA", "att-1", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClearThumbnailPath on a row with no thumbnail = %v, want ErrNotFound", err)
	}
	got, err := scopeA.Attachments().Get(t.Context(), "itemA", "att-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want unchanged 1 -- the rejected clear must not have bumped it", got.Version)
	}
}

func TestClearThumbnailPathRejectsAnAttachmentFromAnotherGroup(t *testing.T) {
	scopeA, scopeB := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.Attachments().SetThumbnailPath(t.Context(), "itemA", "att-1", "groupA/att-1-thumb.jpg", time.Now().UnixMilli()); err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	if err := scopeB.Attachments().ClearThumbnailPath(t.Context(), "itemA", "att-1", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB ClearThumbnailPath on groupA's attachment = %v, want ErrNotFound (P-3)", err)
	}
	got, err := scopeA.Attachments().Get(t.Context(), "itemA", "att-1")
	if err != nil {
		t.Fatalf("groupA Get after groupB's attempt: %v", err)
	}
	if !got.ThumbnailPath.Valid {
		t.Errorf("ThumbnailPath = %+v, want still set -- groupB's rejected call must not have cleared it", got.ThumbnailPath)
	}
}

func TestClearThumbnailPathRejectsAnAttachmentFromAnotherItem(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.Attachments().SetThumbnailPath(t.Context(), "itemA", "att-1", "groupA/att-1-thumb.jpg", time.Now().UnixMilli()); err != nil {
		t.Fatalf("SetThumbnailPath: %v", err)
	}

	if err := scopeA.Attachments().ClearThumbnailPath(t.Context(), "itemA2", "att-1", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClearThumbnailPath naming itemA2 for itemA's attachment = %v, want ErrNotFound", err)
	}
}

func TestClearThumbnailPathRejectsAnUnknownID(t *testing.T) {
	scopeA, _ := attachmentScope(t)

	if err := scopeA.Attachments().ClearThumbnailPath(t.Context(), "itemA", "does-not-exist", time.Now().UnixMilli()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClearThumbnailPath against an unknown id = %v, want ErrNotFound", err)
	}
}

func TestClearThumbnailPathRejectsIncompleteArguments(t *testing.T) {
	scopeA, _ := attachmentScope(t)
	if _, err := scopeA.Attachments().Create(t.Context(), validCreateAttachmentParams("att-1", "itemA")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	cases := []struct {
		name   string
		itemID string
		id     string
		now    int64
	}{
		{"empty itemID", "", "att-1", time.Now().UnixMilli()},
		{"empty id", "itemA", "", time.Now().UnixMilli()},
		{"zero now", "itemA", "att-1", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := scopeA.Attachments().ClearThumbnailPath(t.Context(), tc.itemID, tc.id, tc.now); err == nil {
				t.Errorf("ClearThumbnailPath(%s) succeeded, want a validation error", tc.name)
			}
		})
	}
}
