package storage

import (
	"database/sql"
	"errors"
	"testing"
)

func importCommitScope(t *testing.T, groupID string) (ImportCommitRepository, Scope, *Storage) {
	t.Helper()
	s := newTestStorage(t)
	seedGroupRow(t, s, groupID)

	commit, err := s.ForGroupImportCommit(MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupImportCommit(%s): %v", groupID, err)
	}
	scope, err := s.ForGroup(MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%s): %v", groupID, err)
	}
	return commit, scope, s
}

func stageSession(t *testing.T, s *Storage, groupID, importID string, now int64) {
	t.Helper()
	sessions, err := s.ForGroupImportSessions(MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroupImportSessions(%s): %v", groupID, err)
	}
	if _, err := sessions.Create(t.Context(), CreateImportSessionParams{ID: importID, Source: "native", Now: now}); err != nil {
		t.Fatalf("sessions.Create(%s): %v", importID, err)
	}
}

func readImportSessionStatus(t *testing.T, s *Storage, importID string) string {
	t.Helper()
	var status string
	if err := s.store.Reader().QueryRowContext(t.Context(), `SELECT status FROM import_sessions WHERE id = ?`, importID).Scan(&status); err != nil {
		t.Fatalf("read import_sessions status: %v", err)
	}
	return status
}

func readGroupChangeSeqCounter(t *testing.T, s *Storage, groupID string) int64 {
	t.Helper()
	var seq int64
	if err := s.store.Reader().QueryRowContext(t.Context(), `SELECT change_seq_counter FROM groups WHERE id = ?`, groupID).Scan(&seq); err != nil {
		t.Fatalf("read groups.change_seq_counter: %v", err)
	}
	return seq
}

func countRowsInGroup(t *testing.T, s *Storage, table, groupID string) int {
	t.Helper()
	var n int
	if err := s.store.Reader().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table+` WHERE group_id = ?`, groupID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestImportCommitCreateUpdateUnchanged(t *testing.T) {
	const group = "grp-commit-mixed"
	commit, scope, s := importCommitScope(t, group)

	itemToUpdate, err := scope.Items().Create(t.Context(), CreateItemParams{ID: "item-upd", Name: "Old Name", ShortCode: "UPD0001", Now: 1000})
	if err != nil {
		t.Fatalf("create item-upd: %v", err)
	}
	itemUnchanged, err := scope.Items().Create(t.Context(), CreateItemParams{ID: "item-same", Name: "Same Name", Quantity: 3, ShortCode: "SAM0001", Now: 1000})
	if err != nil {
		t.Fatalf("create item-same: %v", err)
	}

	stageSession(t, s, group, "import-1", 2000)
	beforeSeq := readGroupChangeSeqCounter(t, s, group)

	result, err := commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-1",
		Now:             3000,
		Rows: []ImportCommitRow{
			{
				Line:   2,
				Action: ImportCommitRowCreate,
				Proposed: ImportCommitProposedRow{
					Name: "Brand New Item", Quantity: 5,
					Labels:          []string{"Fragile"},
					Identifications: []ImportCommitIdentification{{Kind: "serial", Value: "SN-1"}},
					Warranty:        &ImportCommitWarranty{Holder: "Alice", IsLifetime: true},
				},
			},
			{
				Line:     3,
				Action:   ImportCommitRowUpdate,
				ItemID:   itemToUpdate.ID,
				Changes:  []string{"name"},
				Proposed: ImportCommitProposedRow{Name: "New Name", Quantity: itemToUpdate.Quantity},
			},
			{
				Line:   4,
				Action: ImportCommitRowUnchanged,
				ItemID: itemUnchanged.ID,
			},
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if result.Created != 1 || result.Updated != 1 || result.Unchanged != 1 {
		t.Fatalf("result = %+v, want {Created:1 Updated:1 Unchanged:1}", result)
	}
	if len(result.CreatedItems) != 1 || result.CreatedItems[0].Line != 2 || result.CreatedItems[0].ItemID == "" {
		t.Fatalf("CreatedItems = %+v, want one entry for line 2 with a non-empty item id", result.CreatedItems)
	}
	newItemID := result.CreatedItems[0].ItemID

	newItem, err := scope.Items().Get(t.Context(), newItemID)
	if err != nil {
		t.Fatalf("Get(newItem): %v", err)
	}
	if newItem.Name != "Brand New Item" || newItem.Quantity != 5 {
		t.Errorf("created item = %+v, want Name=%q Quantity=5", newItem, "Brand New Item")
	}
	if newItem.ShortCode == "" {
		t.Error("created item has no short code")
	}
	labels, err := scope.ItemLabels().ListForItem(t.Context(), newItemID)
	if err != nil {
		t.Fatalf("ListForItem: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "Fragile" {
		t.Errorf("created item labels = %+v, want exactly [Fragile]", labels)
	}
	idents, err := scope.Identifications().List(t.Context(), newItemID)
	if err != nil {
		t.Fatalf("Identifications.List: %v", err)
	}
	if len(idents) != 1 || idents[0].Kind != "serial" || idents[0].Value != "SN-1" {
		t.Errorf("created item identifications = %+v, want exactly [{serial SN-1}]", idents)
	}
	warranty, err := scope.Warranty().Get(t.Context(), newItemID)
	if err != nil {
		t.Fatalf("Warranty.Get: %v", err)
	}
	if warranty.Holder != "Alice" || warranty.IsLifetime == 0 {
		t.Errorf("created item warranty = %+v, want Holder=Alice IsLifetime=true", warranty)
	}

	updated, err := scope.Items().Get(t.Context(), itemToUpdate.ID)
	if err != nil {
		t.Fatalf("Get(updated): %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("updated item Name = %q, want %q", updated.Name, "New Name")
	}
	if updated.Version != itemToUpdate.Version+1 {
		t.Errorf("updated item Version = %d, want %d", updated.Version, itemToUpdate.Version+1)
	}
	if updated.UpdatedAt != 3000 {
		t.Errorf("updated item UpdatedAt = %d, want 3000", updated.UpdatedAt)
	}

	unchanged, err := scope.Items().Get(t.Context(), itemUnchanged.ID)
	if err != nil {
		t.Fatalf("Get(unchanged): %v", err)
	}
	if unchanged.Version != itemUnchanged.Version {
		t.Errorf("unchanged item Version = %d, want %d (untouched)", unchanged.Version, itemUnchanged.Version)
	}
	if unchanged.UpdatedAt != itemUnchanged.UpdatedAt {
		t.Errorf("unchanged item UpdatedAt = %d, want %d (untouched)", unchanged.UpdatedAt, itemUnchanged.UpdatedAt)
	}

	if status := readImportSessionStatus(t, s, "import-1"); status != "committed" {
		t.Errorf("import session status = %q, want %q", status, "committed")
	}

	afterSeq := readGroupChangeSeqCounter(t, s, group)
	if afterSeq <= beforeSeq+1 {
		t.Errorf("group change_seq_counter = %d (before %d), want it to have advanced by more than 1 (create row + update row + new label each allocate their own seq)", afterSeq, beforeSeq)
	}
}

func TestImportCommitMidTransactionFailureRollsBackEverything(t *testing.T) {
	const groupA, groupB = "grp-commit-atomic-a", "grp-commit-atomic-b"
	commit, scope, s := importCommitScope(t, groupA)
	seedGroupRow(t, s, groupB)

	existing, err := scope.Items().Create(t.Context(), CreateItemParams{ID: "item-existing", Name: "Original Name", ShortCode: "ORIG0001", Now: 1000})
	if err != nil {
		t.Fatalf("create existing item: %v", err)
	}

	scopeB, err := s.ForGroup(MustGroupID(groupB))
	if err != nil {
		t.Fatalf("ForGroup(groupB): %v", err)
	}
	foreignLocation, err := scopeB.Locations().Create(t.Context(), CreateLocationParams{ID: "loc-foreign", Name: "Someone Else's Garage", Now: 1000})
	if err != nil {
		t.Fatalf("create foreign location: %v", err)
	}

	stageSession(t, s, groupA, "import-atomic", 2000)

	beforeVersion := existing.Version
	beforeUpdatedAt := existing.UpdatedAt
	beforeSeq := readGroupChangeSeqCounter(t, s, groupA)
	beforeItemCount := countRowsInGroup(t, s, "items", groupA)

	_, err = commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-atomic",
		Now:             3000,
		Rows: []ImportCommitRow{
			{
				Line: 2, Action: ImportCommitRowUpdate, ItemID: existing.ID,
				Changes:  []string{"name"},
				Proposed: ImportCommitProposedRow{Name: "Renamed By The Doomed Import", Quantity: existing.Quantity},
			},
			{
				Line: 3, Action: ImportCommitRowCreate,
				Proposed: ImportCommitProposedRow{Name: "Never Actually Created", LocationID: foreignLocation.ID},
			},
		},
	})
	if err == nil {
		t.Fatal("Commit succeeded, want an error from the foreign-group location_id's own FOREIGN KEY violation")
	}

	after, getErr := scope.Items().Get(t.Context(), existing.ID)
	if getErr != nil {
		t.Fatalf("Get(existing) after failed commit: %v", getErr)
	}
	if after.Name != "Original Name" {
		t.Errorf("existing item Name = %q after rolled-back commit, want %q (row 1's update must not have survived)", after.Name, "Original Name")
	}
	if after.Version != beforeVersion {
		t.Errorf("existing item Version = %d after rolled-back commit, want %d (unchanged)", after.Version, beforeVersion)
	}
	if after.UpdatedAt != beforeUpdatedAt {
		t.Errorf("existing item UpdatedAt = %d after rolled-back commit, want %d (unchanged)", after.UpdatedAt, beforeUpdatedAt)
	}

	if n := countRowsInGroup(t, s, "items", groupA); n != beforeItemCount {
		t.Errorf("items count in group = %d after rolled-back commit, want %d (unchanged)", n, beforeItemCount)
	}

	if seq := readGroupChangeSeqCounter(t, s, groupA); seq != beforeSeq {
		t.Errorf("group change_seq_counter = %d after rolled-back commit, want %d (unchanged)", seq, beforeSeq)
	}

	if status := readImportSessionStatus(t, s, "import-atomic"); status != "staged" {
		t.Errorf("import session status = %q after rolled-back commit, want %q", status, "staged")
	}
}

func TestImportCommitAlreadyCommittedSessionReturnsErrImportSessionNotStaged(t *testing.T) {
	const group = "grp-commit-twice"
	commit, scope, s := importCommitScope(t, group)
	stageSession(t, s, group, "import-twice", 1000)

	first, err := commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-twice", Now: 2000,
		Rows: []ImportCommitRow{{Line: 2, Action: ImportCommitRowCreate, Proposed: ImportCommitProposedRow{Name: "Once"}}},
	})
	if err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	if first.Created != 1 {
		t.Fatalf("first Commit result = %+v, want Created:1", first)
	}
	beforeSeq := readGroupChangeSeqCounter(t, s, group)
	beforeItemCount := countRowsInGroup(t, s, "items", group)

	_, err = commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-twice", Now: 3000,
		Rows: []ImportCommitRow{{Line: 2, Action: ImportCommitRowCreate, Proposed: ImportCommitProposedRow{Name: "Twice"}}},
	})
	if !errors.Is(err, ErrImportSessionNotStaged) {
		t.Fatalf("second Commit error = %v, want ErrImportSessionNotStaged", err)
	}

	if n := countRowsInGroup(t, s, "items", group); n != beforeItemCount {
		t.Errorf("items count = %d after second commit attempt, want %d (nothing written twice)", n, beforeItemCount)
	}
	if seq := readGroupChangeSeqCounter(t, s, group); seq != beforeSeq {
		t.Errorf("group change_seq_counter = %d after second commit attempt, want %d (unchanged)", seq, beforeSeq)
	}
	_ = scope
}

func TestImportCommitLabelCreationAndFullReplace(t *testing.T) {
	const group = "grp-commit-replace"
	commit, scope, s := importCommitScope(t, group)
	stageSession(t, s, group, "import-replace-1", 1000)

	result, err := commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-replace-1", Now: 1000,
		Rows: []ImportCommitRow{{
			Line: 2, Action: ImportCommitRowCreate,
			Proposed: ImportCommitProposedRow{
				Name:            "Tagged Item",
				Labels:          []string{"BrandNewLabel"},
				Identifications: []ImportCommitIdentification{{Kind: "serial", Value: "SN-99"}},
				CustomFields:    []ItemCustomField{{Name: "colour", FieldType: "text", TextValue: sql.NullString{String: "red", Valid: true}}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Commit (create): %v", err)
	}
	itemID := result.CreatedItems[0].ItemID

	label, err := scope.Labels().Get(t.Context(), mustSingleLabelID(t, scope, itemID))
	if err != nil {
		t.Fatalf("Labels().Get: %v", err)
	}
	if label.Name != "BrandNewLabel" || label.Color == "" {
		t.Errorf("created label = %+v, want Name=BrandNewLabel and a non-empty default colour", label)
	}

	stageSession(t, s, group, "import-replace-2", 2000)
	current, err := scope.Items().Get(t.Context(), itemID)
	if err != nil {
		t.Fatalf("Get before second commit: %v", err)
	}
	_, err = commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-replace-2", Now: 2000,
		Rows: []ImportCommitRow{{
			Line: 2, Action: ImportCommitRowUpdate, ItemID: itemID,
			Changes:  []string{"labels", "identifications", "custom_fields"},
			Proposed: ImportCommitProposedRow{Name: current.Name, Quantity: current.Quantity},
		}},
	})
	if err != nil {
		t.Fatalf("Commit (clear): %v", err)
	}

	labelsAfter, err := scope.ItemLabels().ListForItem(t.Context(), itemID)
	if err != nil {
		t.Fatalf("ListForItem after clear: %v", err)
	}
	if len(labelsAfter) != 0 {
		t.Errorf("item labels after clearing commit = %+v, want none", labelsAfter)
	}
	identsAfter, err := scope.Identifications().List(t.Context(), itemID)
	if err != nil {
		t.Fatalf("Identifications.List after clear: %v", err)
	}
	if len(identsAfter) != 0 {
		t.Errorf("item identifications after clearing commit = %+v, want none", identsAfter)
	}
	cfAfter, err := scope.ItemCustomFields().List(t.Context(), itemID)
	if err != nil {
		t.Fatalf("ItemCustomFields.List after clear: %v", err)
	}
	if len(cfAfter) != 0 {
		t.Errorf("item custom fields after clearing commit = %+v, want none", cfAfter)
	}

	if _, err := scope.Labels().Get(t.Context(), label.ID); err != nil {
		t.Errorf("Labels().Get(%s) after clearing the item's assignment: %v, want the label row to still exist", label.ID, err)
	}
}

func mustSingleLabelID(t *testing.T, scope Scope, itemID string) string {
	t.Helper()
	labels, err := scope.ItemLabels().ListForItem(t.Context(), itemID)
	if err != nil {
		t.Fatalf("ListForItem: %v", err)
	}
	if len(labels) != 1 {
		t.Fatalf("ListForItem(%s) = %+v, want exactly one label", itemID, labels)
	}
	return labels[0].ID
}

func TestImportCommitNeverTouchesAttachments(t *testing.T) {
	const group = "grp-commit-attachments"
	commit, scope, s := importCommitScope(t, group)

	item, err := scope.Items().Create(t.Context(), CreateItemParams{ID: "item-with-attachment", Name: "Has A Photo", ShortCode: "PHOTO001", Now: 1000})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	attachment, err := scope.Attachments().Create(t.Context(), CreateAttachmentParams{
		ID: "att-1", ItemID: item.ID, Category: "general", OriginalFilename: "photo.jpg",
		ContentType: "image/jpeg", SizeBytes: 123, SHA256: "deadbeef", StoragePath: group + "/att-1", Now: 1000,
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	stageSession(t, s, group, "import-attachments", 2000)
	if _, err := commit.Commit(t.Context(), ImportCommitParams{
		ImportSessionID: "import-attachments", Now: 3000,
		Rows: []ImportCommitRow{{
			Line: 2, Action: ImportCommitRowUpdate, ItemID: item.ID,
			Changes:  []string{"name"},
			Proposed: ImportCommitProposedRow{Name: "Renamed, No Attachment Cell At All", Quantity: item.Quantity},
		}},
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	after, err := scope.Attachments().Get(t.Context(), item.ID, attachment.ID)
	if err != nil {
		t.Fatalf("Attachments().Get after commit: %v", err)
	}
	if after.Version != attachment.Version || after.UpdatedAt != attachment.UpdatedAt {
		t.Errorf("attachment changed by a commit that never mentioned it: before %+v, after %+v", attachment, after)
	}
}
