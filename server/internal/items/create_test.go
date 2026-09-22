package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type fakeItemRepository struct {
	createFn func(ctx context.Context, p storage.CreateItemParams) (storage.Item, error)
	updateFn func(ctx context.Context, p storage.UpdateItemParams) (storage.Item, error)
}

func (f fakeItemRepository) Get(context.Context, string) (storage.Item, error) {
	return storage.Item{}, errors.New("fakeItemRepository: Get not implemented by this test")
}

func (f fakeItemRepository) GetByShortCode(context.Context, string) (storage.Item, error) {
	return storage.Item{}, errors.New("fakeItemRepository: GetByShortCode not implemented by this test")
}

func (f fakeItemRepository) GetByIDs(context.Context, []string) ([]storage.Item, error) {
	return nil, errors.New("fakeItemRepository: GetByIDs not implemented by this test")
}

func (f fakeItemRepository) ListFiltered(context.Context, storage.ItemFilter, storage.Page) ([]storage.Item, error) {
	return nil, errors.New("items: fakeItemRepository.ListFiltered must not be called")
}

func (f fakeItemRepository) List(context.Context, storage.Page) ([]storage.Item, error) {
	return nil, errors.New("fakeItemRepository: List not implemented by this test")
}

func (f fakeItemRepository) Create(ctx context.Context, p storage.CreateItemParams) (storage.Item, error) {
	return f.createFn(ctx, p)
}

func (f fakeItemRepository) Update(ctx context.Context, p storage.UpdateItemParams) (storage.Item, error) {
	return f.updateFn(ctx, p)
}

func (f fakeItemRepository) Delete(context.Context, string, int64) error {
	return errors.New("fakeItemRepository: Delete not implemented by this test")
}

func TestCreateRejectsEmptyName(t *testing.T) {
	repo := fakeItemRepository{
		createFn: func(context.Context, storage.CreateItemParams) (storage.Item, error) {
			t.Fatal("repo.Create was called; Create should have rejected the empty Name first")
			return storage.Item{}, nil
		},
	}
	_, err := Create(t.Context(), repo, CreateRequest{})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("Create(empty Name) error = %v, want ErrNameRequired", err)
	}
}

func TestCreateSucceedsWithNameOnly(t *testing.T) {
	var gotParams storage.CreateItemParams
	want := storage.Item{ID: "generated", Name: "A Lamp", ShortCode: "ABCDEFGH", Version: 1}
	repo := fakeItemRepository{
		createFn: func(_ context.Context, p storage.CreateItemParams) (storage.Item, error) {
			gotParams = p
			return want, nil
		},
	}

	got, err := Create(t.Context(), repo, CreateRequest{Name: "A Lamp"})
	if err != nil {
		t.Fatalf("Create(name only) = %v, want success", err)
	}
	if got != want {
		t.Errorf("Create returned %+v, want the repository's own result %+v unmodified", got, want)
	}
	if gotParams.Name != "A Lamp" {
		t.Errorf("repo.Create saw Name = %q, want %q", gotParams.Name, "A Lamp")
	}
	if gotParams.Description != "" {
		t.Errorf("repo.Create saw Description = %q, want \"\" (no reinterpretation of the zero value)", gotParams.Description)
	}
	if gotParams.LocationID != "" {
		t.Errorf("repo.Create saw LocationID = %q, want \"\" for an empty request field", gotParams.LocationID)
	}
	if gotParams.ID == "" {
		t.Error("repo.Create saw an empty ID; Create must mint one")
	}
	if gotParams.ShortCode == "" {
		t.Error("repo.Create saw an empty ShortCode; Create must generate one")
	}
	if len(gotParams.ShortCode) != shortCodeLength {
		t.Errorf("generated ShortCode %q has length %d, want %d", gotParams.ShortCode, len(gotParams.ShortCode), shortCodeLength)
	}
}

func TestCreateRetriesOnShortCodeCollision(t *testing.T) {
	seenCodes := map[string]bool{}
	attempts := 0
	repo := fakeItemRepository{
		createFn: func(_ context.Context, p storage.CreateItemParams) (storage.Item, error) {
			attempts++
			if seenCodes[p.ShortCode] {
				t.Errorf("attempt %d reused short code %q instead of drawing a fresh one", attempts, p.ShortCode)
			}
			seenCodes[p.ShortCode] = true
			if attempts < 3 {
				return storage.Item{}, storage.ErrShortCodeTaken
			}
			return storage.Item{ID: p.ID, Name: p.Name, ShortCode: p.ShortCode}, nil
		},
	}

	item, err := Create(t.Context(), repo, CreateRequest{Name: "Retried Item"})
	if err != nil {
		t.Fatalf("Create after two collisions = %v, want eventual success", err)
	}
	if attempts != 3 {
		t.Errorf("repo.Create was called %d times, want exactly 3 (two collisions then a success)", attempts)
	}
	if item.Name != "Retried Item" {
		t.Errorf("returned item = %+v", item)
	}
}

func TestCreateGivesUpAfterMaxShortCodeAttempts(t *testing.T) {
	attempts := 0
	repo := fakeItemRepository{
		createFn: func(context.Context, storage.CreateItemParams) (storage.Item, error) {
			attempts++
			return storage.Item{}, storage.ErrShortCodeTaken
		},
	}

	_, err := Create(t.Context(), repo, CreateRequest{Name: "Never Succeeds"})
	if err == nil {
		t.Fatal("Create against a repository that always collides succeeded; the retry loop must be bounded")
	}
	if attempts != maxShortCodeAttempts {
		t.Errorf("repo.Create was called %d times, want exactly maxShortCodeAttempts=%d", attempts, maxShortCodeAttempts)
	}
}

func TestCreateRejectsNilRepository(t *testing.T) {
	if _, err := Create(t.Context(), nil, CreateRequest{Name: "x"}); err == nil {
		t.Error("Create(nil repository) succeeded; want an error")
	}
}
