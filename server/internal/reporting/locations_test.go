package reporting

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type fakeLocationRepository struct {
	treeFn func(ctx context.Context) ([]*storage.LocationNode, error)
}

func (f fakeLocationRepository) Get(context.Context, string) (storage.Location, error) {
	return storage.Location{}, errors.New("fakeLocationRepository: Get not implemented by this test")
}

func (f fakeLocationRepository) List(context.Context) ([]storage.Location, error) {
	return nil, errors.New("fakeLocationRepository: List not implemented by this test")
}

func (f fakeLocationRepository) Create(context.Context, storage.CreateLocationParams) (storage.Location, error) {
	return storage.Location{}, errors.New("fakeLocationRepository: Create not implemented by this test")
}

func (f fakeLocationRepository) Update(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
	return storage.Location{}, errors.New("fakeLocationRepository: Update not implemented by this test")
}

func (f fakeLocationRepository) Delete(context.Context, storage.DeleteLocationParams) error {
	return errors.New("fakeLocationRepository: Delete not implemented by this test")
}

func (f fakeLocationRepository) Descendants(context.Context, string) ([]string, error) {
	return nil, errors.New("fakeLocationRepository: Descendants not implemented by this test")
}

func (f fakeLocationRepository) Tree(ctx context.Context) ([]*storage.LocationNode, error) {
	if f.treeFn == nil {
		return nil, errors.New("fakeLocationRepository: Tree not implemented by this test")
	}
	return f.treeFn(ctx)
}

func TestItemCountByLocationRejectsNilRepository(t *testing.T) {
	if _, err := ItemCountByLocation(t.Context(), nil); err == nil {
		t.Error("ItemCountByLocation(nil repository) succeeded; want an error")
	}
}

func TestItemCountByLocationFlattensThePreOrderTree(t *testing.T) {
	child := &storage.LocationNode{
		Location:       storage.Location{ID: "shelf-1", Name: "Top Shelf"},
		ItemCount:      2,
		TotalItemCount: 2,
		Children:       []*storage.LocationNode{},
	}
	root := &storage.LocationNode{
		Location:       storage.Location{ID: "garage", Name: "Garage"},
		ItemCount:      1,
		TotalItemCount: 3,
		Children:       []*storage.LocationNode{child},
	}
	repo := fakeLocationRepository{
		treeFn: func(context.Context) ([]*storage.LocationNode, error) {
			return []*storage.LocationNode{root}, nil
		},
	}

	got, err := ItemCountByLocation(t.Context(), repo)
	if err != nil {
		t.Fatalf("ItemCountByLocation() = %v, want success", err)
	}
	want := []LocationItemCountRow{
		{LocationID: "garage", LocationName: "Garage", ItemCount: 3},
		{LocationID: "shelf-1", LocationName: "Top Shelf", ItemCount: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("ItemCountByLocation() returned %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestItemCountByLocationIncludesAnEmptyLeaf(t *testing.T) {
	empty := &storage.LocationNode{
		Location:       storage.Location{ID: "attic", Name: "Attic"},
		ItemCount:      0,
		TotalItemCount: 0,
		Children:       []*storage.LocationNode{},
	}
	repo := fakeLocationRepository{
		treeFn: func(context.Context) ([]*storage.LocationNode, error) {
			return []*storage.LocationNode{empty}, nil
		},
	}

	got, err := ItemCountByLocation(t.Context(), repo)
	if err != nil {
		t.Fatalf("ItemCountByLocation() = %v, want success", err)
	}
	if len(got) != 1 || got[0].ItemCount != 0 {
		t.Errorf("ItemCountByLocation() = %+v, want one row with ItemCount 0", got)
	}
}

func TestItemCountByLocationPropagatesRepositoryError(t *testing.T) {
	sentinel := errors.New("boom")
	repo := fakeLocationRepository{
		treeFn: func(context.Context) ([]*storage.LocationNode, error) {
			return nil, sentinel
		},
	}
	if _, err := ItemCountByLocation(t.Context(), repo); !errors.Is(err, sentinel) {
		t.Fatalf("ItemCountByLocation() error = %v, want it to wrap the repository's own error", err)
	}
}
