package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type fakeItemCustomFieldRepository struct {
	getFn    func(ctx context.Context, itemID, id string) (storage.ItemCustomField, error)
	listFn   func(ctx context.Context, itemID string) ([]storage.ItemCustomField, error)
	createFn func(ctx context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error)
	updateFn func(ctx context.Context, p storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error)
	deleteFn func(ctx context.Context, itemID, id string, now int64) error
}

func (f fakeItemCustomFieldRepository) Get(ctx context.Context, itemID, id string) (storage.ItemCustomField, error) {
	return f.getFn(ctx, itemID, id)
}

func (f fakeItemCustomFieldRepository) List(ctx context.Context, itemID string) ([]storage.ItemCustomField, error) {
	return f.listFn(ctx, itemID)
}

func (f fakeItemCustomFieldRepository) Create(ctx context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
	return f.createFn(ctx, p)
}

func (f fakeItemCustomFieldRepository) Update(ctx context.Context, p storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
	return f.updateFn(ctx, p)
}

func (f fakeItemCustomFieldRepository) Delete(ctx context.Context, itemID, id string, now int64) error {
	return f.deleteFn(ctx, itemID, id, now)
}

type fakeCustomFieldDefRepository struct {
	getFn    func(ctx context.Context, id string) (storage.CustomFieldDef, error)
	listFn   func(ctx context.Context) ([]storage.CustomFieldDef, error)
	createFn func(ctx context.Context, p storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error)
	updateFn func(ctx context.Context, p storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error)
	deleteFn func(ctx context.Context, id string, now int64) error
}

func (f fakeCustomFieldDefRepository) Get(ctx context.Context, id string) (storage.CustomFieldDef, error) {
	return f.getFn(ctx, id)
}

func (f fakeCustomFieldDefRepository) List(ctx context.Context) ([]storage.CustomFieldDef, error) {
	return f.listFn(ctx)
}

func (f fakeCustomFieldDefRepository) Create(ctx context.Context, p storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
	return f.createFn(ctx, p)
}

func (f fakeCustomFieldDefRepository) Update(ctx context.Context, p storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
	return f.updateFn(ctx, p)
}

func (f fakeCustomFieldDefRepository) Delete(ctx context.Context, id string, now int64) error {
	return f.deleteFn(ctx, id, now)
}

func strp(s string) *string     { return &s }
func floatp(f float64) *float64 { return &f }
func boolp(b bool) *bool        { return &b }

func TestValidateCustomFieldTypeAcceptsExactlyTheSchemasFourTokens(t *testing.T) {
	for _, ft := range []string{"text", "number", "boolean", "date"} {
		if err := ValidateCustomFieldType(ft); err != nil {
			t.Errorf("ValidateCustomFieldType(%q) = %v, want nil", ft, err)
		}
	}
	for _, ft := range []string{"", "Text", "TEXT", "string", "int", "bool", "unknown", " text"} {
		if err := ValidateCustomFieldType(ft); !errors.Is(err, ErrCustomFieldTypeInvalid) {
			t.Errorf("ValidateCustomFieldType(%q) = %v, want ErrCustomFieldTypeInvalid", ft, err)
		}
	}
}

func TestCreateItemCustomFieldRejectsZeroValueColumns(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite zero value columns populated")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "Colour", FieldType: "text",
	})
	if !errors.Is(err, ErrCustomFieldValueInvalid) {
		t.Fatalf("CreateItemCustomField with zero value columns = %v, want ErrCustomFieldValueInvalid", err)
	}
}

func TestCreateItemCustomFieldRejectsMultipleValueColumns(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite two value columns populated")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "Colour", FieldType: "text",
		TextValue: strp("blue"), NumberValue: floatp(1),
	})
	if !errors.Is(err, ErrCustomFieldValueInvalid) {
		t.Fatalf("CreateItemCustomField with two value columns = %v, want ErrCustomFieldValueInvalid", err)
	}
}

func TestCreateItemCustomFieldRejectsAMismatchedValueColumn(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite a mismatched value column")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "Weight", FieldType: "number",
		TextValue: strp("not a number"),
	})
	if !errors.Is(err, ErrCustomFieldValueInvalid) {
		t.Fatalf("CreateItemCustomField with field_type=number and only text_value set = %v, want ErrCustomFieldValueInvalid", err)
	}
}

func TestCreateItemCustomFieldAcceptsEachFieldTypeWithItsOwnColumn(t *testing.T) {
	cases := []struct {
		name      string
		fieldType string
		req       CreateItemCustomFieldRequest
	}{
		{"text", "text", CreateItemCustomFieldRequest{TextValue: strp("blue")}},
		{"number", "number", CreateItemCustomFieldRequest{NumberValue: floatp(5)}},
		{"boolean", "boolean", CreateItemCustomFieldRequest{BoolValue: boolp(true)}},
		{"date", "date", CreateItemCustomFieldRequest{DateValue: strp("2026-01-15")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			repo := fakeItemCustomFieldRepository{
				createFn: func(_ context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
					called = true
					return storage.ItemCustomField{Name: p.Name, FieldType: p.FieldType, Version: 1}, nil
				},
			}
			req := tc.req
			req.ItemID, req.Name, req.FieldType = "itm-1", "Field", tc.fieldType
			if _, err := CreateItemCustomField(t.Context(), repo, nil, req); err != nil {
				t.Fatalf("CreateItemCustomField(%s): %v", tc.fieldType, err)
			}
			if !called {
				t.Error("the repository was never called")
			}
		})
	}
}

func TestCreateItemCustomFieldRejectsABlankName(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite a blank name")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "", FieldType: "text", TextValue: strp("v"),
	})
	if !errors.Is(err, ErrCustomFieldNameRequired) {
		t.Fatalf("CreateItemCustomField with a blank name = %v, want ErrCustomFieldNameRequired", err)
	}
}

func TestCreateItemCustomFieldRejectsAnInvalidFieldType(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite an invalid field_type")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "n", FieldType: "not-a-type",
	})
	if !errors.Is(err, ErrCustomFieldTypeInvalid) {
		t.Fatalf("CreateItemCustomField with an invalid field_type = %v, want ErrCustomFieldTypeInvalid", err)
	}
}

func TestCreateItemCustomFieldChecksNameBeforeFieldTypeBeforeValueColumns(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite three invalid fields")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "", FieldType: "not-a-type",
	})
	if !errors.Is(err, ErrCustomFieldNameRequired) {
		t.Fatalf("CreateItemCustomField with a blank name AND an invalid field_type AND zero value columns = %v, want ErrCustomFieldNameRequired", err)
	}

	_, err = CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "n", FieldType: "not-a-type",
	})
	if !errors.Is(err, ErrCustomFieldTypeInvalid) {
		t.Fatalf("CreateItemCustomField with an invalid field_type AND zero value columns = %v, want ErrCustomFieldTypeInvalid", err)
	}
}

func TestCreateItemCustomFieldAdHocNeverCallsTheDefRepository(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(_ context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			return storage.ItemCustomField{Name: p.Name, FieldType: p.FieldType, Version: 1}, nil
		},
	}
	if _, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "Colour", FieldType: "text", TextValue: strp("blue"),
	}); err != nil {
		t.Fatalf("CreateItemCustomField with no field_def_id and a nil defRepo = %v, want success", err)
	}
}

func TestCreateItemCustomFieldReadsTheDefinitionWhenFieldDefIDIsSet(t *testing.T) {
	var gotID string
	defRepo := fakeCustomFieldDefRepository{
		getFn: func(_ context.Context, id string) (storage.CustomFieldDef, error) {
			gotID = id
			return storage.CustomFieldDef{ID: id, Name: "Colour", FieldType: "text", Version: 1}, nil
		},
	}
	repo := fakeItemCustomFieldRepository{
		createFn: func(_ context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			return storage.ItemCustomField{Name: p.Name, FieldType: p.FieldType, Version: 1}, nil
		},
	}
	if _, err := CreateItemCustomField(t.Context(), repo, defRepo, CreateItemCustomFieldRequest{
		ItemID: "itm-1", FieldDefID: "cfd-1", Name: "Colour", FieldType: "text", TextValue: strp("blue"),
	}); err != nil {
		t.Fatalf("CreateItemCustomField: %v", err)
	}
	if gotID != "cfd-1" {
		t.Errorf("defRepo.Get was called with id %q, want %q", gotID, "cfd-1")
	}
}

func TestCreateItemCustomFieldRejectsAnUnknownFieldDefID(t *testing.T) {
	defRepo := fakeCustomFieldDefRepository{
		getFn: func(context.Context, string) (storage.CustomFieldDef, error) {
			return storage.CustomFieldDef{}, storage.ErrNotFound
		},
	}
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite an unresolvable field_def_id")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, defRepo, CreateItemCustomFieldRequest{
		ItemID: "itm-1", FieldDefID: "cfd-gone", Name: "Colour", FieldType: "text", TextValue: strp("blue"),
	})
	if !errors.Is(err, ErrCustomFieldDefNotFound) {
		t.Fatalf("CreateItemCustomField with an unresolvable field_def_id = %v, want ErrCustomFieldDefNotFound", err)
	}
	if errors.Is(err, storage.ErrNotFound) {
		t.Errorf("err wraps storage.ErrNotFound directly (%v); it must be items' own ErrCustomFieldDefNotFound, a DIFFERENT sentinel httpapi maps to 400, not 404", err)
	}
}

func TestCreateItemCustomFieldRejectsAFieldTypeContradictingTheDefinition(t *testing.T) {
	defRepo := fakeCustomFieldDefRepository{
		getFn: func(_ context.Context, id string) (storage.CustomFieldDef, error) {
			return storage.CustomFieldDef{ID: id, Name: "Purchase date", FieldType: "date", Version: 1}, nil
		},
	}
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite a field_type contradicting the definition")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, defRepo, CreateItemCustomFieldRequest{
		ItemID: "itm-1", FieldDefID: "cfd-1", Name: "Purchase date", FieldType: "number", NumberValue: floatp(42),
	})
	if !errors.Is(err, ErrCustomFieldTypeMismatch) {
		t.Fatalf("CreateItemCustomField typed number against a date definition = %v, want ErrCustomFieldTypeMismatch", err)
	}
}

func TestCreateItemCustomFieldWithFieldDefIDButNilDefRepoFails(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite a nil def repository")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", FieldDefID: "cfd-1", Name: "n", FieldType: "text", TextValue: strp("v"),
	})
	if err == nil {
		t.Fatal("CreateItemCustomField with field_def_id set and a nil def repository succeeded, want an error")
	}
}

func TestCreateItemCustomFieldMintsAnIDAndStampsNow(t *testing.T) {
	var got storage.CreateItemCustomFieldParams
	repo := fakeItemCustomFieldRepository{
		createFn: func(_ context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			got = p
			return storage.ItemCustomField{Name: p.Name, FieldType: p.FieldType, Version: 1}, nil
		},
	}
	if _, err := CreateItemCustomField(t.Context(), repo, nil, CreateItemCustomFieldRequest{
		ItemID: "itm-1", Name: "Colour", FieldType: "text", TextValue: strp("blue"),
	}); err != nil {
		t.Fatalf("CreateItemCustomField: %v", err)
	}
	if got.ID == "" {
		t.Error("ID is empty, want a minted UUIDv7 (P-2)")
	}
	if got.ItemID != "itm-1" || got.Name != "Colour" || got.FieldType != "text" {
		t.Errorf("got {item_id:%q name:%q field_type:%q}, want {\"itm-1\" \"Colour\" \"text\"}", got.ItemID, got.Name, got.FieldType)
	}
	if got.Now <= 0 {
		t.Errorf("Now = %d, want a positive Unix-millisecond timestamp", got.Now)
	}
}

func TestCreateItemCustomFieldRejectsANilRepository(t *testing.T) {
	if _, err := CreateItemCustomField(t.Context(), nil, nil, CreateItemCustomFieldRequest{ItemID: "itm-1", Name: "n", FieldType: "text", TextValue: strp("v")}); err == nil {
		t.Fatal("CreateItemCustomField with a nil repository succeeded, want an error")
	}
}

func TestUpdateItemCustomFieldRequiresAVersion(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		updateFn: func(context.Context, storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite a missing version")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := UpdateItemCustomField(t.Context(), repo, nil, UpdateItemCustomFieldRequest{
		ItemID: "itm-1", CustomFieldID: "cf-1", Name: "n", FieldType: "text", TextValue: strp("v"),
	})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("UpdateItemCustomField with no version = %v, want ErrVersionRequired", err)
	}
}

func TestUpdateItemCustomFieldRejectsZeroValueColumns(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		updateFn: func(context.Context, storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite zero value columns populated")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := UpdateItemCustomField(t.Context(), repo, nil, UpdateItemCustomFieldRequest{
		ItemID: "itm-1", CustomFieldID: "cf-1", Name: "n", FieldType: "text", ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrCustomFieldValueInvalid) {
		t.Fatalf("UpdateItemCustomField with zero value columns = %v, want ErrCustomFieldValueInvalid", err)
	}
}

func TestUpdateItemCustomFieldRejectsAFieldTypeContradictingTheDefinition(t *testing.T) {
	defRepo := fakeCustomFieldDefRepository{
		getFn: func(_ context.Context, id string) (storage.CustomFieldDef, error) {
			return storage.CustomFieldDef{ID: id, Name: "Purchase date", FieldType: "date", Version: 1}, nil
		},
	}
	repo := fakeItemCustomFieldRepository{
		updateFn: func(context.Context, storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite a field_type contradicting the definition")
			return storage.ItemCustomField{}, nil
		},
	}
	_, err := UpdateItemCustomField(t.Context(), repo, defRepo, UpdateItemCustomFieldRequest{
		ItemID: "itm-1", CustomFieldID: "cf-1", FieldDefID: "cfd-1", Name: "Purchase date", FieldType: "number", NumberValue: floatp(1), ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrCustomFieldTypeMismatch) {
		t.Fatalf("UpdateItemCustomField typed number against a date definition = %v, want ErrCustomFieldTypeMismatch", err)
	}
}

func TestUpdateItemCustomFieldPassesIDsThrough(t *testing.T) {
	var got storage.UpdateItemCustomFieldParams
	repo := fakeItemCustomFieldRepository{
		updateFn: func(_ context.Context, p storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
			got = p
			return storage.ItemCustomField{ID: p.ID, ItemID: p.ItemID, Name: p.Name, FieldType: p.FieldType, Version: 2}, nil
		},
	}
	if _, err := UpdateItemCustomField(t.Context(), repo, nil, UpdateItemCustomFieldRequest{
		ItemID: "itm-1", CustomFieldID: "cf-1", Name: "Colour", FieldType: "text", TextValue: strp("red"), ExpectedVersion: 1,
	}); err != nil {
		t.Fatalf("UpdateItemCustomField: %v", err)
	}
	if got.ItemID != "itm-1" || got.ID != "cf-1" {
		t.Errorf("got {item_id:%q id:%q}, want {\"itm-1\" \"cf-1\"}", got.ItemID, got.ID)
	}
}

func TestUpdateItemCustomFieldRejectsANilRepository(t *testing.T) {
	if _, err := UpdateItemCustomField(t.Context(), nil, nil, UpdateItemCustomFieldRequest{
		ItemID: "itm-1", CustomFieldID: "cf-1", Name: "n", FieldType: "text", TextValue: strp("v"), ExpectedVersion: 1,
	}); err == nil {
		t.Fatal("UpdateItemCustomField with a nil repository succeeded, want an error")
	}
}
