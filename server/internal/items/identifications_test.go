package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type fakeIdentificationRepository struct {
	getFn    func(ctx context.Context, itemID, id string) (storage.Identification, error)
	listFn   func(ctx context.Context, itemID string) ([]storage.Identification, error)
	createFn func(ctx context.Context, p storage.CreateIdentificationParams) (storage.Identification, error)
	updateFn func(ctx context.Context, p storage.UpdateIdentificationParams) (storage.Identification, error)
	deleteFn func(ctx context.Context, itemID, id string, now int64) error
}

func (f fakeIdentificationRepository) Get(ctx context.Context, itemID, id string) (storage.Identification, error) {
	return f.getFn(ctx, itemID, id)
}

func (f fakeIdentificationRepository) List(ctx context.Context, itemID string) ([]storage.Identification, error) {
	return f.listFn(ctx, itemID)
}

func (f fakeIdentificationRepository) Create(ctx context.Context, p storage.CreateIdentificationParams) (storage.Identification, error) {
	return f.createFn(ctx, p)
}

func (f fakeIdentificationRepository) Update(ctx context.Context, p storage.UpdateIdentificationParams) (storage.Identification, error) {
	return f.updateFn(ctx, p)
}

func (f fakeIdentificationRepository) Delete(ctx context.Context, itemID, id string, now int64) error {
	return f.deleteFn(ctx, itemID, id, now)
}

func TestValidateIdentificationKindAcceptsExactlyTheSchemasFiveTokens(t *testing.T) {
	for _, kind := range []string{"serial", "model", "asset_tag", "barcode", "other"} {
		if err := ValidateIdentificationKind(kind); err != nil {
			t.Errorf("ValidateIdentificationKind(%q) = %v, want nil", kind, err)
		}
	}
	for _, kind := range []string{"", "Serial", "SERIAL", "asset-tag", "upc", "ean", "unknown", " serial"} {
		if err := ValidateIdentificationKind(kind); !errors.Is(err, ErrIdentificationKindInvalid) {
			t.Errorf("ValidateIdentificationKind(%q) = %v, want ErrIdentificationKindInvalid", kind, err)
		}
	}
}

func TestCreateIdentificationRejectsAnInvalidKind(t *testing.T) {
	repo := fakeIdentificationRepository{
		createFn: func(context.Context, storage.CreateIdentificationParams) (storage.Identification, error) {
			t.Error("the repository was called despite an invalid kind")
			return storage.Identification{}, nil
		},
	}
	_, err := CreateIdentification(t.Context(), repo, CreateIdentificationRequest{ItemID: "i1", Kind: "not-a-kind", Value: "v"})
	if !errors.Is(err, ErrIdentificationKindInvalid) {
		t.Fatalf("CreateIdentification with an invalid kind = %v, want ErrIdentificationKindInvalid", err)
	}
}

func TestCreateIdentificationRejectsABlankValue(t *testing.T) {
	repo := fakeIdentificationRepository{
		createFn: func(context.Context, storage.CreateIdentificationParams) (storage.Identification, error) {
			t.Error("the repository was called despite a blank value")
			return storage.Identification{}, nil
		},
	}
	_, err := CreateIdentification(t.Context(), repo, CreateIdentificationRequest{ItemID: "i1", Kind: "serial", Value: ""})
	if !errors.Is(err, ErrIdentificationValueRequired) {
		t.Fatalf("CreateIdentification with a blank value = %v, want ErrIdentificationValueRequired", err)
	}
}

func TestCreateIdentificationMintsAnIDAndStampsNow(t *testing.T) {
	var got storage.CreateIdentificationParams
	repo := fakeIdentificationRepository{
		createFn: func(_ context.Context, p storage.CreateIdentificationParams) (storage.Identification, error) {
			got = p
			return storage.Identification{ItemID: p.ItemID, Kind: p.Kind, Value: p.Value, Version: 1}, nil
		},
	}

	if _, err := CreateIdentification(t.Context(), repo, CreateIdentificationRequest{ItemID: "i1", Kind: "serial", Value: "SN-1"}); err != nil {
		t.Fatalf("CreateIdentification: %v", err)
	}
	if got.ID == "" {
		t.Error("ID is empty, want a minted UUIDv7 (P-2)")
	}
	if got.ItemID != "i1" || got.Kind != "serial" || got.Value != "SN-1" {
		t.Errorf("got {item_id:%q kind:%q value:%q}, want {\"i1\" \"serial\" \"SN-1\"}", got.ItemID, got.Kind, got.Value)
	}
	if got.Now <= 0 {
		t.Errorf("Now = %d, want a positive Unix-millisecond timestamp", got.Now)
	}
}

func TestUpdateIdentificationRequiresAVersion(t *testing.T) {
	repo := fakeIdentificationRepository{
		updateFn: func(context.Context, storage.UpdateIdentificationParams) (storage.Identification, error) {
			t.Error("the repository was called despite a missing version")
			return storage.Identification{}, nil
		},
	}
	_, err := UpdateIdentification(t.Context(), repo, UpdateIdentificationRequest{
		ItemID: "i1", IdentificationID: "id-1", Kind: "serial", Value: "v",
	})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("UpdateIdentification with no version = %v, want ErrVersionRequired", err)
	}
}

func TestUpdateIdentificationRejectsAnInvalidKind(t *testing.T) {
	repo := fakeIdentificationRepository{
		updateFn: func(context.Context, storage.UpdateIdentificationParams) (storage.Identification, error) {
			t.Error("the repository was called despite an invalid kind")
			return storage.Identification{}, nil
		},
	}
	_, err := UpdateIdentification(t.Context(), repo, UpdateIdentificationRequest{
		ItemID: "i1", IdentificationID: "id-1", Kind: "not-a-kind", Value: "v", ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrIdentificationKindInvalid) {
		t.Fatalf("UpdateIdentification with an invalid kind = %v, want ErrIdentificationKindInvalid", err)
	}
}

func TestUpdateIdentificationRejectsABlankValue(t *testing.T) {
	repo := fakeIdentificationRepository{
		updateFn: func(context.Context, storage.UpdateIdentificationParams) (storage.Identification, error) {
			t.Error("the repository was called despite a blank value")
			return storage.Identification{}, nil
		},
	}
	_, err := UpdateIdentification(t.Context(), repo, UpdateIdentificationRequest{
		ItemID: "i1", IdentificationID: "id-1", Kind: "serial", Value: "", ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrIdentificationValueRequired) {
		t.Fatalf("UpdateIdentification with a blank value = %v, want ErrIdentificationValueRequired", err)
	}
}

func TestUpdateIdentificationPassesTheRowIDThrough(t *testing.T) {
	var got storage.UpdateIdentificationParams
	repo := fakeIdentificationRepository{
		updateFn: func(_ context.Context, p storage.UpdateIdentificationParams) (storage.Identification, error) {
			got = p
			return storage.Identification{ItemID: p.ItemID, ID: p.ID, Kind: p.Kind, Value: p.Value, Version: 2}, nil
		},
	}
	if _, err := UpdateIdentification(t.Context(), repo, UpdateIdentificationRequest{
		ItemID: "i1", IdentificationID: "id-1", Kind: "model", Value: "M-2", ExpectedVersion: 1,
	}); err != nil {
		t.Fatalf("UpdateIdentification: %v", err)
	}
	if got.ItemID != "i1" || got.ID != "id-1" {
		t.Errorf("got {item_id:%q id:%q}, want {\"i1\" \"id-1\"}", got.ItemID, got.ID)
	}
	if got.ExpectedVersion != 1 {
		t.Errorf("ExpectedVersion = %d, want 1", got.ExpectedVersion)
	}
}
