package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

func TestUpdateRejectsEmptyName(t *testing.T) {
	repo := fakeItemRepository{
		updateFn: func(context.Context, storage.UpdateItemParams) (storage.Item, error) {
			t.Fatal("repo.Update was called; Update should have rejected the empty Name first")
			return storage.Item{}, nil
		},
	}
	_, err := Update(t.Context(), repo, UpdateRequest{ItemID: "i1", ExpectedVersion: 1})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("Update(empty Name) error = %v, want ErrNameRequired", err)
	}
}

func TestUpdateRejectsMissingVersion(t *testing.T) {
	repo := fakeItemRepository{
		updateFn: func(context.Context, storage.UpdateItemParams) (storage.Item, error) {
			t.Fatal("repo.Update was called; Update should have rejected the missing version first")
			return storage.Item{}, nil
		},
	}
	_, err := Update(t.Context(), repo, UpdateRequest{ItemID: "i1", Name: "x", ExpectedVersion: 0})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("Update(ExpectedVersion=0) error = %v, want ErrVersionRequired", err)
	}
}

func TestUpdatePassesThroughStorageResult(t *testing.T) {
	want := storage.Item{ID: "i1", Name: "New Name", Version: 2}
	var gotParams storage.UpdateItemParams
	repo := fakeItemRepository{
		updateFn: func(_ context.Context, p storage.UpdateItemParams) (storage.Item, error) {
			gotParams = p
			return want, nil
		},
	}

	got, err := Update(t.Context(), repo, UpdateRequest{
		ItemID: "i1", Name: "New Name", Description: "d", LocationID: "loc-1", Quantity: 5, ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got != want {
		t.Errorf("Update returned %+v, want %+v", got, want)
	}
	if gotParams.ItemID != "i1" || gotParams.ExpectedVersion != 1 || gotParams.Quantity != 5 {
		t.Errorf("repo.Update saw %+v", gotParams)
	}
	if gotParams.LocationID != "loc-1" {
		t.Errorf("repo.Update saw LocationID = %q, want %q", gotParams.LocationID, "loc-1")
	}
}

func TestUpdateSentinelErrorsPropagate(t *testing.T) {
	for _, want := range []error{storage.ErrVersionMismatch, storage.ErrNotFound} {
		repo := fakeItemRepository{
			updateFn: func(context.Context, storage.UpdateItemParams) (storage.Item, error) {
				return storage.Item{}, want
			},
		}
		_, err := Update(t.Context(), repo, UpdateRequest{ItemID: "i1", Name: "x", ExpectedVersion: 1})
		if !errors.Is(err, want) {
			t.Errorf("Update propagated err = %v, want errors.Is(err, %v)", err, want)
		}
	}
}
