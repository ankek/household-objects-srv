package locations

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type fakeLocationRepository struct {
	getFn         func(ctx context.Context, id string) (storage.Location, error)
	listFn        func(ctx context.Context) ([]storage.Location, error)
	createFn      func(ctx context.Context, p storage.CreateLocationParams) (storage.Location, error)
	updateFn      func(ctx context.Context, p storage.UpdateLocationParams) (storage.Location, error)
	deleteFn      func(ctx context.Context, p storage.DeleteLocationParams) error
	descendantsFn func(ctx context.Context, locationID string) ([]string, error)
	treeFn        func(ctx context.Context) ([]*storage.LocationNode, error)
}

func (f fakeLocationRepository) Get(ctx context.Context, id string) (storage.Location, error) {
	return f.getFn(ctx, id)
}
func (f fakeLocationRepository) List(ctx context.Context) ([]storage.Location, error) {
	return f.listFn(ctx)
}
func (f fakeLocationRepository) Create(ctx context.Context, p storage.CreateLocationParams) (storage.Location, error) {
	return f.createFn(ctx, p)
}
func (f fakeLocationRepository) Update(ctx context.Context, p storage.UpdateLocationParams) (storage.Location, error) {
	return f.updateFn(ctx, p)
}
func (f fakeLocationRepository) Delete(ctx context.Context, p storage.DeleteLocationParams) error {
	return f.deleteFn(ctx, p)
}

func (f fakeLocationRepository) Descendants(ctx context.Context, locationID string) ([]string, error) {
	if f.descendantsFn != nil {
		return f.descendantsFn(ctx, locationID)
	}
	return nil, nil
}

func (f fakeLocationRepository) Tree(ctx context.Context) ([]*storage.LocationNode, error) {
	if f.treeFn != nil {
		return f.treeFn(ctx)
	}
	return nil, nil
}

func TestCreateRejectsBlankName(t *testing.T) {
	repo := fakeLocationRepository{
		createFn: func(ctx context.Context, p storage.CreateLocationParams) (storage.Location, error) {
			t.Fatal("Create must not reach the repository with a blank Name")
			return storage.Location{}, nil
		},
	}
	if _, err := Create(t.Context(), repo, CreateRequest{Name: ""}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("Create with blank Name: err = %v, want ErrNameRequired", err)
	}
}

func TestCreatePassesParentIDThrough(t *testing.T) {
	var gotParentID string
	repo := fakeLocationRepository{
		createFn: func(ctx context.Context, p storage.CreateLocationParams) (storage.Location, error) {
			gotParentID = p.ParentID
			if p.ID == "" {
				t.Error("Create did not mint an ID")
			}
			if p.Now <= 0 {
				t.Error("Create did not stamp Now")
			}
			return storage.Location{ID: p.ID, Name: p.Name}, nil
		},
	}
	if _, err := Create(t.Context(), repo, CreateRequest{Name: "Garage", ParentID: "loc-parent"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotParentID != "loc-parent" {
		t.Fatalf("repo saw ParentID %q, want loc-parent", gotParentID)
	}
}

func TestCreateSurfacesRepositoryErrorsUnchanged(t *testing.T) {
	repo := fakeLocationRepository{
		createFn: func(ctx context.Context, p storage.CreateLocationParams) (storage.Location, error) {
			return storage.Location{}, storage.ErrLocationParentNotFound
		},
	}
	_, err := Create(t.Context(), repo, CreateRequest{Name: "Garage", ParentID: "does-not-exist"})
	if !errors.Is(err, storage.ErrLocationParentNotFound) {
		t.Fatalf("Create: err = %v, want storage.ErrLocationParentNotFound to pass through unchanged", err)
	}
}

func TestUpdateRejectsBlankName(t *testing.T) {
	repo := fakeLocationRepository{
		updateFn: func(ctx context.Context, p storage.UpdateLocationParams) (storage.Location, error) {
			t.Fatal("Update must not reach the repository with a blank Name")
			return storage.Location{}, nil
		},
	}
	_, err := Update(t.Context(), repo, UpdateRequest{LocationID: "loc-1", Name: "", ExpectedVersion: 1})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("Update with blank Name: err = %v, want ErrNameRequired", err)
	}
}

func TestUpdateRejectsMissingVersion(t *testing.T) {
	repo := fakeLocationRepository{
		updateFn: func(ctx context.Context, p storage.UpdateLocationParams) (storage.Location, error) {
			t.Fatal("Update must not reach the repository with ExpectedVersion <= 0")
			return storage.Location{}, nil
		},
	}
	_, err := Update(t.Context(), repo, UpdateRequest{LocationID: "loc-1", Name: "Garage", ExpectedVersion: 0})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("Update with ExpectedVersion=0: err = %v, want ErrVersionRequired", err)
	}
}

func TestUpdateSurfacesCycleErrorUnchanged(t *testing.T) {
	repo := fakeLocationRepository{
		updateFn: func(ctx context.Context, p storage.UpdateLocationParams) (storage.Location, error) {
			return storage.Location{}, storage.ErrLocationCycle
		},
	}
	_, err := Update(t.Context(), repo, UpdateRequest{LocationID: "loc-a", Name: "A", ParentID: "loc-c", ExpectedVersion: 1})
	if !errors.Is(err, storage.ErrLocationCycle) {
		t.Fatalf("Update: err = %v, want storage.ErrLocationCycle to pass through unchanged", err)
	}
}
