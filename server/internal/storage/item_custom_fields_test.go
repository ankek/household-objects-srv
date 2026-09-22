package storage

import (
	"errors"
	"testing"
	"time"
)

func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }
func boolPtr(b bool) *bool        { return &b }

func itemCustomFieldScope(t *testing.T) (scopeA, scopeB Scope) {
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

func TestItemCustomFieldCreateStoresEachValueType(t *testing.T) {
	cases := []struct {
		name      string
		fieldType string
		params    CreateItemCustomFieldParams
		wantText  string
		wantNum   float64
		wantBool  bool
		wantDate  string
	}{
		{
			name: "text", fieldType: "text",
			params:   CreateItemCustomFieldParams{TextValue: strPtr("blue")},
			wantText: "blue",
		},
		{
			name: "number", fieldType: "number",
			params:  CreateItemCustomFieldParams{NumberValue: floatPtr(12.5)},
			wantNum: 12.5,
		},
		{
			name: "boolean", fieldType: "boolean",
			params:   CreateItemCustomFieldParams{BoolValue: boolPtr(true)},
			wantBool: true,
		},
		{
			name: "date", fieldType: "date",
			params:   CreateItemCustomFieldParams{DateValue: strPtr("2026-01-15")},
			wantDate: "2026-01-15",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scopeA, _ := itemCustomFieldScope(t)
			p := tc.params
			p.ID, p.ItemID, p.Name, p.FieldType, p.Now = "cf-"+tc.name, "itemA", "Colour", tc.fieldType, time.Now().UnixMilli()

			created, err := scopeA.ItemCustomFields().Create(t.Context(), p)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if created.Version != 1 {
				t.Errorf("Version = %d, want 1", created.Version)
			}
			if created.ChangeSeq <= 0 {
				t.Errorf("ChangeSeq = %d, want a positive allocated sequence (FR-090)", created.ChangeSeq)
			}
			if created.FieldType != tc.fieldType {
				t.Errorf("FieldType = %q, want %q", created.FieldType, tc.fieldType)
			}

			switch tc.fieldType {
			case "text":
				if !created.TextValue.Valid || created.TextValue.String != tc.wantText {
					t.Errorf("TextValue = %+v, want %q", created.TextValue, tc.wantText)
				}
				if created.NumberValue.Valid || created.BoolValue.Valid || created.DateValue.Valid {
					t.Errorf("the other three columns must stay NULL, got %+v %+v %+v", created.NumberValue, created.BoolValue, created.DateValue)
				}
			case "number":
				if !created.NumberValue.Valid || created.NumberValue.Float64 != tc.wantNum {
					t.Errorf("NumberValue = %+v, want %v", created.NumberValue, tc.wantNum)
				}
				if created.TextValue.Valid || created.BoolValue.Valid || created.DateValue.Valid {
					t.Errorf("the other three columns must stay NULL, got %+v %+v %+v", created.TextValue, created.BoolValue, created.DateValue)
				}
			case "boolean":
				if !created.BoolValue.Valid || (created.BoolValue.Int64 != 0) != tc.wantBool {
					t.Errorf("BoolValue = %+v, want %v", created.BoolValue, tc.wantBool)
				}
				if created.TextValue.Valid || created.NumberValue.Valid || created.DateValue.Valid {
					t.Errorf("the other three columns must stay NULL, got %+v %+v %+v", created.TextValue, created.NumberValue, created.DateValue)
				}
			case "date":
				if !created.DateValue.Valid || created.DateValue.String != tc.wantDate {
					t.Errorf("DateValue = %+v, want %q", created.DateValue, tc.wantDate)
				}
				if created.TextValue.Valid || created.NumberValue.Valid || created.BoolValue.Valid {
					t.Errorf("the other three columns must stay NULL, got %+v %+v %+v", created.TextValue, created.NumberValue, created.BoolValue)
				}
			}
		})
	}
}

func TestItemCustomFieldCreateAcceptsAnyColumnCombinationAtTheStorageLayer(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	zero, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-zero", ItemID: "itemA", Name: "Empty", FieldType: "text", Now: now,
	})
	if err != nil {
		t.Fatalf("Create with ZERO value columns populated = %v, want success -- the storage layer has no backstop for this", err)
	}
	if zero.TextValue.Valid || zero.NumberValue.Valid || zero.BoolValue.Valid || zero.DateValue.Valid {
		t.Errorf("expected all four columns NULL, got %+v", zero)
	}

	multi, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-multi", ItemID: "itemA", Name: "Conflicted", FieldType: "text",
		TextValue: strPtr("blue"), NumberValue: floatPtr(7), Now: now,
	})
	if err != nil {
		t.Fatalf("Create with TWO value columns populated = %v, want success -- the storage layer has no backstop for this", err)
	}
	if !multi.TextValue.Valid || !multi.NumberValue.Valid {
		t.Errorf("expected BOTH text_value and number_value to have landed, got %+v", multi)
	}

	mismatched, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-mismatch", ItemID: "itemA", Name: "Mislabelled", FieldType: "number",
		TextValue: strPtr("not a number"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create with field_type=number but text_value set = %v, want success -- the storage layer has no backstop for this", err)
	}
	if mismatched.FieldType != "number" || !mismatched.TextValue.Valid || mismatched.NumberValue.Valid {
		t.Errorf("expected a CORRUPT stored row {field_type:number text_value:valid number_value:NULL}, got %+v", mismatched)
	}
}

func TestItemCustomFieldCreateAllowsMultipleRowsOfTheSameName(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-2", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("red"), Now: now,
	}); err != nil {
		t.Fatalf("second Create (same name, different value) = %v, want success -- FR-016 is 0..N", err)
	}

	rows, err := scopeA.ItemCustomFields().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List = %d rows, want 2", len(rows))
	}
}

func TestItemCustomFieldCreateRejectsAnItemFromAnotherGroup(t *testing.T) {
	scopeA, scopeB := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	_, err := scopeB.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-x", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("pwned"), Now: now,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Create on groupA's item = %v, want ErrNotFound (P-3)", err)
	}

	if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-a", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	}); err != nil {
		t.Fatalf("groupA Create on its OWN item = %v, want success; without this the refusal above proves nothing about scope", err)
	}
}

func TestItemCustomFieldCreateWithFieldDefIDStoresIt(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	def, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "Colour", FieldType: "text", Now: now,
	})
	if err != nil {
		t.Fatalf("Create custom field def: %v", err)
	}

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", FieldDefID: def.ID, Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create with field_def_id: %v", err)
	}
	if !created.FieldDefID.Valid || created.FieldDefID.String != def.ID {
		t.Errorf("FieldDefID = %+v, want {%q true}", created.FieldDefID, def.ID)
	}

	adHoc, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-2", ItemID: "itemA", Name: "Ad hoc", FieldType: "text", TextValue: strPtr("v"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create without field_def_id: %v", err)
	}
	if adHoc.FieldDefID.Valid {
		t.Errorf("FieldDefID = %+v, want {\"\" false} for an ad hoc row", adHoc.FieldDefID)
	}
}

func TestItemCustomFieldCreateRejectsFieldDefFromAnotherGroupAtTheForeignKey(t *testing.T) {
	scopeA, scopeB := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	def, err := scopeA.CustomFieldDefs().Create(t.Context(), CreateCustomFieldDefParams{
		ID: "cfd-1", Name: "Colour", FieldType: "text", Now: now,
	})
	if err != nil {
		t.Fatalf("Create custom field def in groupA: %v", err)
	}

	_, err = scopeB.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-x", ItemID: "itemB", FieldDefID: def.ID, Name: "Colour", FieldType: "text", TextValue: strPtr("pwned"), Now: now,
	})
	if err == nil {
		t.Fatal("groupB Create naming groupA's field_def_id succeeded, want the composite FOREIGN KEY to refuse it")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("got ErrNotFound, want a raw constraint failure -- this repository does not translate the FK error (package items' A103.2 pre-read is what normally prevents reaching it): %v", err)
	}
}

func TestItemCustomFieldListRejectsAnotherGroupsItem(t *testing.T) {
	scopeA, scopeB := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scopeB.ItemCustomFields().List(t.Context(), "itemA"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB List of groupA's item = %v, want ErrNotFound", err)
	}
	rows, err := scopeA.ItemCustomFields().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("groupA List of its OWN item = %v, want success", err)
	}
	if len(rows) != 1 || rows[0].Name != "Colour" {
		t.Errorf("groupA's own list = %+v, want exactly one row {name:\"Colour\"}", rows)
	}
}

func TestItemCustomFieldListOnAnItemWithNoRowsIsAnEmptySlice(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)

	rows, err := scopeA.ItemCustomFields().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List on an item with no custom fields = %v, want success", err)
	}
	if len(rows) != 0 {
		t.Errorf("List = %+v, want an empty slice", rows)
	}

	if _, err := scopeA.ItemCustomFields().List(t.Context(), "no-such-item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("List on an unknown item = %v, want ErrNotFound", err)
	}
}

func TestItemCustomFieldListExcludesOtherItemsRowsInTheSameGroup(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "itemA's own field", FieldType: "text", TextValue: strPtr("v"), Now: now,
	}); err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}
	if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-2", ItemID: "itemA2", Name: "itemA2's field", FieldType: "text", TextValue: strPtr("v"), Now: now,
	}); err != nil {
		t.Fatalf("Create on itemA2: %v", err)
	}

	rows, err := scopeA.ItemCustomFields().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List(itemA): %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "itemA's own field" {
		t.Fatalf("List(itemA) = %+v, want exactly one row {name:\"itemA's own field\"} -- itemA2's row leaked into itemA's list", rows)
	}
}

func TestItemCustomFieldGetRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := scopeB.ItemCustomFields().Get(t.Context(), "itemA", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Get of groupA's row = %v, want ErrNotFound", err)
	}
	own, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get of its OWN row = %v, want success", err)
	}
	if own.Name != "Colour" {
		t.Errorf("name = %q, want %q", own.Name, "Colour")
	}
}

func TestItemCustomFieldGetRejectsAnotherItemsRowInTheSameGroup(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}

	if _, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA2", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA Get of itemA's row by NAMING itemA2 = %v, want ErrNotFound", err)
	}
	if _, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA", created.ID); err != nil {
		t.Fatalf("Get naming the correct item = %v, want success", err)
	}
}

func TestItemCustomFieldUpdateAppliesAndBumpsVersion(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := scopeA.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{
		ItemID: "itemA", ID: created.ID, Name: "Colour", FieldType: "number", NumberValue: floatPtr(5),
		ExpectedVersion: created.Version, Now: now + 1,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2", updated.Version)
	}
	if updated.FieldType != "number" || !updated.NumberValue.Valid || updated.NumberValue.Float64 != 5 {
		t.Errorf("got {field_type:%q number_value:%+v}, want {\"number\" {5 true}}", updated.FieldType, updated.NumberValue)
	}
	if updated.TextValue.Valid {
		t.Errorf("TextValue = %+v, want NULL after switching field_type away from text", updated.TextValue)
	}
	if updated.ChangeSeq <= created.ChangeSeq {
		t.Errorf("ChangeSeq = %d, want greater than the create's %d (FR-090)", updated.ChangeSeq, created.ChangeSeq)
	}
}

func TestItemCustomFieldUpdateRejectsAStaleVersion(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := scopeA.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{
		ItemID: "itemA", ID: created.ID, Name: "Colour", FieldType: "text", TextValue: strPtr("red"), ExpectedVersion: 1, Now: now + 1,
	}); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	_, err = scopeA.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{
		ItemID: "itemA", ID: created.ID, Name: "Colour", FieldType: "text", TextValue: strPtr("green"), ExpectedVersion: 1, Now: now + 2,
	})
	if !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("Update with a stale version = %v, want ErrVersionMismatch", err)
	}
	got, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.TextValue.Valid || got.TextValue.String != "red" || got.Version != 2 {
		t.Errorf("after the rejected update the row is {text_value:%+v version:%d}, want {\"red\" 2} -- the conflicting write landed anyway", got.TextValue, got.Version)
	}
}

func TestItemCustomFieldUpdateRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = scopeB.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{
		ItemID: "itemA", ID: created.ID, Name: "Colour", FieldType: "text", TextValue: strPtr("pwned"), ExpectedVersion: 1, Now: now + 1,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Update of groupA's row = %v, want ErrNotFound (never ErrVersionMismatch)", err)
	}

	got, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get after groupB's attempt: %v", err)
	}
	if !got.TextValue.Valid || got.TextValue.String != "blue" || got.Version != 1 {
		t.Errorf("groupA's row is now {text_value:%+v version:%d}, want {\"blue\" 1} -- groupB's write landed despite the error", got.TextValue, got.Version)
	}
}

func TestItemCustomFieldUpdateRejectsAnotherItemsRowInTheSameGroup(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}

	_, err = scopeA.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{
		ItemID: "itemA2", ID: created.ID, Name: "Colour", FieldType: "text", TextValue: strPtr("pwned by itemA2"), ExpectedVersion: 1, Now: now + 1,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA Update naming itemA2 for itemA's row = %v, want ErrNotFound (never ErrVersionMismatch)", err)
	}

	got, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get (naming the correct item) after the cross-item attempt: %v", err)
	}
	if !got.TextValue.Valid || got.TextValue.String != "blue" || got.Version != 1 {
		t.Errorf("itemA's row is now {text_value:%+v version:%d}, want {\"blue\" 1} -- the cross-item write landed despite the error", got.TextValue, got.Version)
	}
}

func TestItemCustomFieldDeleteTombstonesAndIsNotFoundAfterwards(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := scopeA.ItemCustomFields().Delete(t.Context(), "itemA", created.ID, now+1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := scopeA.ItemCustomFields().Delete(t.Context(), "itemA", created.ID, now+2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
	if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-2", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now + 3,
	}); err != nil {
		t.Fatalf("Create after Delete with the same name/value = %v, want success (0..N, no uniqueness rule)", err)
	}
}

func TestItemCustomFieldDeleteRejectsAnotherGroupsRow(t *testing.T) {
	scopeA, scopeB := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := scopeB.ItemCustomFields().Delete(t.Context(), "itemA", created.ID, now+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupB Delete of groupA's row = %v, want ErrNotFound", err)
	}
	got, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get after groupB's delete attempt = %v, want the row still there", err)
	}
	if !got.TextValue.Valid || got.TextValue.String != "blue" || got.DeletedAt.Valid {
		t.Errorf("groupA's row is now {text_value:%+v deleted_at:%+v}, want {\"blue\" NULL} -- groupB's delete landed despite the error", got.TextValue, got.DeletedAt)
	}
}

func TestItemCustomFieldDeleteRejectsAnotherItemsRowInTheSameGroup(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	created, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "Colour", FieldType: "text", TextValue: strPtr("blue"), Now: now,
	})
	if err != nil {
		t.Fatalf("Create on itemA: %v", err)
	}

	if err := scopeA.ItemCustomFields().Delete(t.Context(), "itemA2", created.ID, now+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("groupA Delete naming itemA2 for itemA's row = %v, want ErrNotFound", err)
	}
	got, err := scopeA.ItemCustomFields().Get(t.Context(), "itemA", created.ID)
	if err != nil {
		t.Fatalf("groupA Get (naming the correct item) after the cross-item delete attempt: %v", err)
	}
	if !got.TextValue.Valid || got.TextValue.String != "blue" || got.DeletedAt.Valid {
		t.Errorf("itemA's row is now {text_value:%+v deleted_at:%+v}, want {\"blue\" NULL} -- the cross-item delete landed despite the error", got.TextValue, got.DeletedAt)
	}
}

func TestItemCustomFieldDeleteRemovesExactlyOneRow(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	for _, id := range []string{"cf-1", "cf-2", "cf-3"} {
		if _, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
			ID: id, ItemID: "itemA", Name: id, FieldType: "text", TextValue: strPtr(id), Now: now,
		}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	if err := scopeA.ItemCustomFields().Delete(t.Context(), "itemA", "cf-1", now); err != nil {
		t.Fatalf("Delete cf-1: %v", err)
	}

	rows, err := scopeA.ItemCustomFields().List(t.Context(), "itemA")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("deleting cf-1 left %d of 3 custom fields, want 2 -- the delete matched more than the row it named", len(rows))
	}
	for _, r := range rows {
		if r.ID == "cf-1" {
			t.Errorf("cf-1 survived its own delete")
		}
	}
}

func TestItemCustomFieldRepositoryRejectsIncompleteParams(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)
	now := time.Now().UnixMilli()

	cases := []struct {
		name string
		call func() error
	}{
		{"create without an id", func() error {
			_, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{ItemID: "itemA", Name: "n", FieldType: "text", Now: now})
			return err
		}},
		{"create without an item", func() error {
			_, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{ID: "cf-1", Name: "n", FieldType: "text", Now: now})
			return err
		}},
		{"create without a name", func() error {
			_, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{ID: "cf-1", ItemID: "itemA", FieldType: "text", Now: now})
			return err
		}},
		{"create without a field_type", func() error {
			_, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{ID: "cf-1", ItemID: "itemA", Name: "n", Now: now})
			return err
		}},
		{"create without a timestamp", func() error {
			_, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{ID: "cf-1", ItemID: "itemA", Name: "n", FieldType: "text"})
			return err
		}},
		{"update without an expected version", func() error {
			_, err := scopeA.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{ItemID: "itemA", ID: "cf-1", Name: "n", FieldType: "text", Now: now})
			return err
		}},
		{"update without a name", func() error {
			_, err := scopeA.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{ItemID: "itemA", ID: "cf-1", FieldType: "text", ExpectedVersion: 1, Now: now})
			return err
		}},
		{"update without a field_type", func() error {
			_, err := scopeA.ItemCustomFields().Update(t.Context(), UpdateItemCustomFieldParams{ItemID: "itemA", ID: "cf-1", Name: "n", ExpectedVersion: 1, Now: now})
			return err
		}},
		{"delete without an item", func() error {
			return scopeA.ItemCustomFields().Delete(t.Context(), "", "cf-1", now)
		}},
		{"delete without an id", func() error {
			return scopeA.ItemCustomFields().Delete(t.Context(), "itemA", "", now)
		}},
		{"delete without a timestamp", func() error {
			return scopeA.ItemCustomFields().Delete(t.Context(), "itemA", "cf-1", 0)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("succeeded; an incomplete params struct must be refused before it reaches SQL")
			}
		})
	}
}

func TestItemCustomFieldCreateRejectsAnUnknownFieldTypeAtTheCheckConstraint(t *testing.T) {
	scopeA, _ := itemCustomFieldScope(t)

	_, err := scopeA.ItemCustomFields().Create(t.Context(), CreateItemCustomFieldParams{
		ID: "cf-1", ItemID: "itemA", Name: "n", FieldType: "not-a-real-type", TextValue: strPtr("v"), Now: time.Now().UnixMilli(),
	})
	if err == nil {
		t.Fatal("Create with an unrecognised field_type succeeded; migration 0001's CHECK (field_type IN (...)) should have refused it")
	}
}
