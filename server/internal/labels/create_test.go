package labels

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"testing"
)

type fakeLabelRepository struct {
	getFn    func(ctx context.Context, id string) (storage.Label, error)
	listFn   func(ctx context.Context) ([]storage.Label, error)
	createFn func(ctx context.Context, p storage.CreateLabelParams) (storage.Label, error)
	updateFn func(ctx context.Context, p storage.UpdateLabelParams) (storage.Label, error)
	deleteFn func(ctx context.Context, id string, now int64) error
}

func (f fakeLabelRepository) Get(ctx context.Context, id string) (storage.Label, error) {
	return f.getFn(ctx, id)
}

func (f fakeLabelRepository) List(ctx context.Context) ([]storage.Label, error) {
	return f.listFn(ctx)
}

func (f fakeLabelRepository) Create(ctx context.Context, p storage.CreateLabelParams) (storage.Label, error) {
	return f.createFn(ctx, p)
}

func (f fakeLabelRepository) Update(ctx context.Context, p storage.UpdateLabelParams) (storage.Label, error) {
	return f.updateFn(ctx, p)
}

func (f fakeLabelRepository) Delete(ctx context.Context, id string, now int64) error {
	return f.deleteFn(ctx, id, now)
}

func TestValidateColorAcceptsSixDigitHexTriplets(t *testing.T) {
	for _, c := range []string{"#888888", "#FF8800", "#ffffff", "#000000", "#AbCdEf"} {
		if err := ValidateColor(c); err != nil {
			t.Errorf("ValidateColor(%q) = %v, want nil", c, err)
		}
	}
}

func TestValidateColorRejectsEverythingElse(t *testing.T) {
	for _, c := range []string{"", "888888", "#888", "#88888G", "gray", "#8888888", " #888888", "#888888 "} {
		if err := ValidateColor(c); !errors.Is(err, ErrColorInvalid) {
			t.Errorf("ValidateColor(%q) = %v, want ErrColorInvalid", c, err)
		}
	}
}

func TestCreateLabelRejectsAnInvalidColor(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(context.Context, storage.CreateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite an invalid color")
			return storage.Label{}, nil
		},
	}
	_, err := CreateLabel(t.Context(), repo, CreateLabelRequest{Name: "Kitchen", Color: "not-a-color"})
	if !errors.Is(err, ErrColorInvalid) {
		t.Fatalf("CreateLabel with an invalid color = %v, want ErrColorInvalid", err)
	}
}

func TestCreateLabelRejectsABlankName(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(context.Context, storage.CreateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite a blank name")
			return storage.Label{}, nil
		},
	}
	_, err := CreateLabel(t.Context(), repo, CreateLabelRequest{Name: "", Color: "#888888"})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("CreateLabel with a blank name = %v, want ErrNameRequired", err)
	}
}

func TestCreateLabelChecksNameBeforeColor(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(context.Context, storage.CreateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite two invalid fields")
			return storage.Label{}, nil
		},
	}
	_, err := CreateLabel(t.Context(), repo, CreateLabelRequest{Name: "", Color: "not-a-color"})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("CreateLabel with a blank name AND an invalid color = %v, want ErrNameRequired", err)
	}
}

func TestCreateLabelMintsAnIDAndStampsNow(t *testing.T) {
	var got storage.CreateLabelParams
	repo := fakeLabelRepository{
		createFn: func(_ context.Context, p storage.CreateLabelParams) (storage.Label, error) {
			got = p
			return storage.Label{Name: p.Name, Color: p.Color, Version: 1}, nil
		},
	}

	if _, err := CreateLabel(t.Context(), repo, CreateLabelRequest{Name: "Kitchen", Color: "#FF8800"}); err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if got.ID == "" {
		t.Error("ID is empty, want a minted UUIDv7 (P-2)")
	}
	if got.Name != "Kitchen" || got.Color != "#FF8800" {
		t.Errorf("got {name:%q color:%q}, want {\"Kitchen\" \"#FF8800\"}", got.Name, got.Color)
	}
	if got.Now <= 0 {
		t.Errorf("Now = %d, want a positive Unix-millisecond timestamp", got.Now)
	}
}

func TestCreateLabelPropagatesANameConflictFromStorage(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(context.Context, storage.CreateLabelParams) (storage.Label, error) {
			return storage.Label{}, storage.ErrLabelNameConflict
		},
	}
	_, err := CreateLabel(t.Context(), repo, CreateLabelRequest{Name: "Kitchen", Color: "#888888"})
	if !errors.Is(err, storage.ErrLabelNameConflict) {
		t.Fatalf("CreateLabel propagating a storage conflict = %v, want storage.ErrLabelNameConflict", err)
	}
}

func TestUpdateLabelRequiresAVersion(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(context.Context, storage.UpdateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite a missing version")
			return storage.Label{}, nil
		},
	}
	_, err := UpdateLabel(t.Context(), repo, UpdateLabelRequest{LabelID: "lbl-1", Name: "n", Color: "#888888"})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("UpdateLabel with no version = %v, want ErrVersionRequired", err)
	}
}

func TestUpdateLabelRejectsAnInvalidColor(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(context.Context, storage.UpdateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite an invalid color")
			return storage.Label{}, nil
		},
	}
	_, err := UpdateLabel(t.Context(), repo, UpdateLabelRequest{
		LabelID: "lbl-1", Name: "n", Color: "not-a-color", ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrColorInvalid) {
		t.Fatalf("UpdateLabel with an invalid color = %v, want ErrColorInvalid", err)
	}
}

func TestUpdateLabelRejectsABlankName(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(context.Context, storage.UpdateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite a blank name")
			return storage.Label{}, nil
		},
	}
	_, err := UpdateLabel(t.Context(), repo, UpdateLabelRequest{
		LabelID: "lbl-1", Name: "", Color: "#888888", ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("UpdateLabel with a blank name = %v, want ErrNameRequired", err)
	}
}

func TestUpdateLabelPassesLabelIDThrough(t *testing.T) {
	var got storage.UpdateLabelParams
	repo := fakeLabelRepository{
		updateFn: func(_ context.Context, p storage.UpdateLabelParams) (storage.Label, error) {
			got = p
			return storage.Label{ID: p.ID, Name: p.Name, Color: p.Color, Version: 2}, nil
		},
	}
	if _, err := UpdateLabel(t.Context(), repo, UpdateLabelRequest{
		LabelID: "lbl-1", Name: "n", Color: "#00FF00", ExpectedVersion: 1,
	}); err != nil {
		t.Fatalf("UpdateLabel: %v", err)
	}
	if got.ID != "lbl-1" {
		t.Errorf("ID = %q, want %q", got.ID, "lbl-1")
	}
	if got.Color != "#00FF00" {
		t.Errorf("Color = %q, want #00FF00", got.Color)
	}
}

func TestUpdateLabelPropagatesANameConflictFromStorage(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(context.Context, storage.UpdateLabelParams) (storage.Label, error) {
			return storage.Label{}, storage.ErrLabelNameConflict
		},
	}
	_, err := UpdateLabel(t.Context(), repo, UpdateLabelRequest{
		LabelID: "lbl-1", Name: "Kitchen", Color: "#888888", ExpectedVersion: 1,
	})
	if !errors.Is(err, storage.ErrLabelNameConflict) {
		t.Fatalf("UpdateLabel propagating a storage conflict = %v, want storage.ErrLabelNameConflict", err)
	}
}

func TestCreateLabelRejectsANilRepository(t *testing.T) {
	if _, err := CreateLabel(t.Context(), nil, CreateLabelRequest{Name: "n", Color: "#888888"}); err == nil {
		t.Fatal("CreateLabel with a nil repository succeeded, want an error")
	}
}

func TestUpdateLabelRejectsANilRepository(t *testing.T) {
	if _, err := UpdateLabel(t.Context(), nil, UpdateLabelRequest{LabelID: "lbl-1", Name: "n", Color: "#888888", ExpectedVersion: 1}); err == nil {
		t.Fatal("UpdateLabel with a nil repository succeeded, want an error")
	}
}
