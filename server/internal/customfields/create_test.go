package customfields

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

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

func TestValidateFieldTypeAcceptsExactlyTheSchemasFourTokens(t *testing.T) {
	for _, ft := range []string{"text", "number", "boolean", "date"} {
		if err := ValidateFieldType(ft); err != nil {
			t.Errorf("ValidateFieldType(%q) = %v, want nil", ft, err)
		}
	}
	for _, ft := range []string{"", "Text", "TEXT", "string", "int", "bool", "unknown", " text"} {
		if err := ValidateFieldType(ft); !errors.Is(err, ErrFieldTypeInvalid) {
			t.Errorf("ValidateFieldType(%q) = %v, want ErrFieldTypeInvalid", ft, err)
		}
	}
}

func TestCreateCustomFieldDefRejectsAnInvalidFieldType(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		createFn: func(context.Context, storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called despite an invalid field_type")
			return storage.CustomFieldDef{}, nil
		},
	}
	_, err := CreateCustomFieldDef(t.Context(), repo, CreateCustomFieldDefRequest{Name: "n", FieldType: "not-a-type"})
	if !errors.Is(err, ErrFieldTypeInvalid) {
		t.Fatalf("CreateCustomFieldDef with an invalid field_type = %v, want ErrFieldTypeInvalid", err)
	}
}

func TestCreateCustomFieldDefRejectsABlankName(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		createFn: func(context.Context, storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called despite a blank name")
			return storage.CustomFieldDef{}, nil
		},
	}
	_, err := CreateCustomFieldDef(t.Context(), repo, CreateCustomFieldDefRequest{Name: "", FieldType: "text"})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("CreateCustomFieldDef with a blank name = %v, want ErrNameRequired", err)
	}
}

func TestCreateCustomFieldDefChecksNameBeforeFieldType(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		createFn: func(context.Context, storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called despite two invalid fields")
			return storage.CustomFieldDef{}, nil
		},
	}
	_, err := CreateCustomFieldDef(t.Context(), repo, CreateCustomFieldDefRequest{Name: "", FieldType: "not-a-type"})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("CreateCustomFieldDef with a blank name AND an invalid field_type = %v, want ErrNameRequired", err)
	}
}

func TestCreateCustomFieldDefMintsAnIDAndStampsNow(t *testing.T) {
	var got storage.CreateCustomFieldDefParams
	repo := fakeCustomFieldDefRepository{
		createFn: func(_ context.Context, p storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			got = p
			return storage.CustomFieldDef{Name: p.Name, FieldType: p.FieldType, DisplayOrder: p.DisplayOrder, Version: 1}, nil
		},
	}

	if _, err := CreateCustomFieldDef(t.Context(), repo, CreateCustomFieldDefRequest{Name: "Warranty length", FieldType: "text"}); err != nil {
		t.Fatalf("CreateCustomFieldDef: %v", err)
	}
	if got.ID == "" {
		t.Error("ID is empty, want a minted UUIDv7 (P-2)")
	}
	if got.Name != "Warranty length" || got.FieldType != "text" {
		t.Errorf("got {name:%q field_type:%q}, want {\"Warranty length\" \"text\"}", got.Name, got.FieldType)
	}
	if got.DisplayOrder != 0 {
		t.Errorf("DisplayOrder = %d, want 0 (the caller's own zero value, not a sentinel)", got.DisplayOrder)
	}
	if got.Now <= 0 {
		t.Errorf("Now = %d, want a positive Unix-millisecond timestamp", got.Now)
	}
}

func TestCreateCustomFieldDefPassesDisplayOrderThrough(t *testing.T) {
	var got storage.CreateCustomFieldDefParams
	repo := fakeCustomFieldDefRepository{
		createFn: func(_ context.Context, p storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			got = p
			return storage.CustomFieldDef{Name: p.Name, FieldType: p.FieldType, DisplayOrder: p.DisplayOrder, Version: 1}, nil
		},
	}
	if _, err := CreateCustomFieldDef(t.Context(), repo, CreateCustomFieldDefRequest{Name: "n", FieldType: "number", DisplayOrder: 7}); err != nil {
		t.Fatalf("CreateCustomFieldDef: %v", err)
	}
	if got.DisplayOrder != 7 {
		t.Errorf("DisplayOrder = %d, want 7", got.DisplayOrder)
	}
}

func TestUpdateCustomFieldDefRequiresAVersion(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		updateFn: func(context.Context, storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called despite a missing version")
			return storage.CustomFieldDef{}, nil
		},
	}
	_, err := UpdateCustomFieldDef(t.Context(), repo, UpdateCustomFieldDefRequest{
		FieldDefID: "cfd-1", Name: "n", FieldType: "text",
	})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("UpdateCustomFieldDef with no version = %v, want ErrVersionRequired", err)
	}
}

func TestUpdateCustomFieldDefRejectsAnInvalidFieldType(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		updateFn: func(context.Context, storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called despite an invalid field_type")
			return storage.CustomFieldDef{}, nil
		},
	}
	_, err := UpdateCustomFieldDef(t.Context(), repo, UpdateCustomFieldDefRequest{
		FieldDefID: "cfd-1", Name: "n", FieldType: "not-a-type", ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrFieldTypeInvalid) {
		t.Fatalf("UpdateCustomFieldDef with an invalid field_type = %v, want ErrFieldTypeInvalid", err)
	}
}

func TestUpdateCustomFieldDefRejectsABlankName(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		updateFn: func(context.Context, storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called despite a blank name")
			return storage.CustomFieldDef{}, nil
		},
	}
	_, err := UpdateCustomFieldDef(t.Context(), repo, UpdateCustomFieldDefRequest{
		FieldDefID: "cfd-1", Name: "", FieldType: "text", ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("UpdateCustomFieldDef with a blank name = %v, want ErrNameRequired", err)
	}
}

func TestUpdateCustomFieldDefPassesFieldDefIDThrough(t *testing.T) {
	var got storage.UpdateCustomFieldDefParams
	repo := fakeCustomFieldDefRepository{
		updateFn: func(_ context.Context, p storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			got = p
			return storage.CustomFieldDef{ID: p.ID, Name: p.Name, FieldType: p.FieldType, Version: 2}, nil
		},
	}
	if _, err := UpdateCustomFieldDef(t.Context(), repo, UpdateCustomFieldDefRequest{
		FieldDefID: "cfd-1", Name: "n", FieldType: "text", DisplayOrder: 3, ExpectedVersion: 1,
	}); err != nil {
		t.Fatalf("UpdateCustomFieldDef: %v", err)
	}
	if got.ID != "cfd-1" {
		t.Errorf("ID = %q, want %q", got.ID, "cfd-1")
	}
	if got.DisplayOrder != 3 {
		t.Errorf("DisplayOrder = %d, want 3", got.DisplayOrder)
	}
}

func TestCreateCustomFieldDefRejectsANilRepository(t *testing.T) {
	if _, err := CreateCustomFieldDef(t.Context(), nil, CreateCustomFieldDefRequest{Name: "n", FieldType: "text"}); err == nil {
		t.Fatal("CreateCustomFieldDef with a nil repository succeeded, want an error")
	}
}

func TestUpdateCustomFieldDefRejectsANilRepository(t *testing.T) {
	if _, err := UpdateCustomFieldDef(t.Context(), nil, UpdateCustomFieldDefRequest{FieldDefID: "cfd-1", Name: "n", FieldType: "text", ExpectedVersion: 1}); err == nil {
		t.Fatal("UpdateCustomFieldDef with a nil repository succeeded, want an error")
	}
}
