package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"testing"
)

type fakeWarrantyRepository struct {
	getFn    func(ctx context.Context, itemID string) (storage.Warranty, error)
	createFn func(ctx context.Context, p storage.CreateWarrantyParams) (storage.Warranty, error)
	updateFn func(ctx context.Context, p storage.UpdateWarrantyParams) (storage.Warranty, error)
	deleteFn func(ctx context.Context, itemID string, now int64) error
}

func (f fakeWarrantyRepository) Get(ctx context.Context, itemID string) (storage.Warranty, error) {
	return f.getFn(ctx, itemID)
}

func (f fakeWarrantyRepository) Create(ctx context.Context, p storage.CreateWarrantyParams) (storage.Warranty, error) {
	return f.createFn(ctx, p)
}

func (f fakeWarrantyRepository) Update(ctx context.Context, p storage.UpdateWarrantyParams) (storage.Warranty, error) {
	return f.updateFn(ctx, p)
}

func (f fakeWarrantyRepository) Delete(ctx context.Context, itemID string, now int64) error {
	return f.deleteFn(ctx, itemID, now)
}

func TestCreateWarrantyRequiresNothingButAnItem(t *testing.T) {
	var got storage.CreateWarrantyParams
	repo := fakeWarrantyRepository{
		createFn: func(_ context.Context, p storage.CreateWarrantyParams) (storage.Warranty, error) {
			got = p
			return storage.Warranty{ItemID: p.ItemID, Version: 1}, nil
		},
	}

	if _, err := CreateWarranty(t.Context(), repo, CreateWarrantyRequest{ItemID: "i1"}); err != nil {
		t.Fatalf("CreateWarranty(item only) = %v, want success (FR-011/FR-012: no field of the block is mandatory)", err)
	}
	if got.ItemID != "i1" {
		t.Errorf("ItemID = %q, want %q", got.ItemID, "i1")
	}
	if got.Now <= 0 {
		t.Errorf("Now = %d, want a positive Unix-millisecond stamp minted by this package", got.Now)
	}
	parsed, err := uuid.Parse(got.ID)
	if err != nil {
		t.Fatalf("ID %q is not a UUID: %v", got.ID, err)
	}
	if parsed.Version() != 7 {
		t.Errorf("ID %q is a UUIDv%d, want v7 (P-2)", got.ID, parsed.Version())
	}
}

func TestCreateWarrantyPassesEveryFR012FieldThrough(t *testing.T) {
	var got storage.CreateWarrantyParams
	repo := fakeWarrantyRepository{
		createFn: func(_ context.Context, p storage.CreateWarrantyParams) (storage.Warranty, error) {
			got = p
			return storage.Warranty{}, nil
		},
	}

	req := CreateWarrantyRequest{
		ItemID: "i1", Holder: "Alice", Provider: "Acme", StartsOn: "2026-01-15",
		ExpiresOn: "2029-01-14", IsLifetime: true, Notes: "in the drawer",
	}
	if _, err := CreateWarranty(t.Context(), repo, req); err != nil {
		t.Fatalf("CreateWarranty: %v", err)
	}
	if got.Holder != req.Holder || got.Provider != req.Provider || got.Notes != req.Notes {
		t.Errorf("text fields = {%q %q %q}, want {%q %q %q}", got.Holder, got.Provider, got.Notes, req.Holder, req.Provider, req.Notes)
	}
	if got.StartsOn != req.StartsOn || got.ExpiresOn != req.ExpiresOn || !got.IsLifetime {
		t.Errorf("dates/flag = {%q %q %t}, want {%q %q true}", got.StartsOn, got.ExpiresOn, got.IsLifetime, req.StartsOn, req.ExpiresOn)
	}
}

func TestWarrantyDateValidation(t *testing.T) {
	cases := []struct {
		name      string
		startsOn  string
		expiresOn string
		wantErr   bool
	}{
		{name: "both empty means not recorded", wantErr: false},
		{name: "well-formed ISO days", startsOn: "2026-01-15", expiresOn: "2029-12-31", wantErr: false},
		{name: "leap day in a leap year", startsOn: "2028-02-29", wantErr: false},
		{name: "day out of range for the month", expiresOn: "2026-02-30", wantErr: true},
		{name: "month out of range", expiresOn: "2026-13-01", wantErr: true},
		{name: "day-first european format", startsOn: "15/01/2026", wantErr: true},
		{name: "a timestamp rather than a day", expiresOn: "2026-01-15T00:00:00Z", wantErr: true},
		{name: "unpadded month", startsOn: "2026-1-15", wantErr: true},
		{name: "unpadded day", expiresOn: "2026-01-5", wantErr: true},
		{name: "free text", startsOn: "next tuesday", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			createRepo := fakeWarrantyRepository{
				createFn: func(context.Context, storage.CreateWarrantyParams) (storage.Warranty, error) {
					reached = true
					return storage.Warranty{}, nil
				},
			}
			_, err := CreateWarranty(t.Context(), createRepo, CreateWarrantyRequest{
				ItemID: "i1", StartsOn: tc.startsOn, ExpiresOn: tc.expiresOn,
			})
			switch {
			case tc.wantErr && !errors.Is(err, ErrWarrantyDateInvalid):
				t.Fatalf("CreateWarranty(starts=%q expires=%q) = %v, want ErrWarrantyDateInvalid", tc.startsOn, tc.expiresOn, err)
			case tc.wantErr && reached:
				t.Fatal("the repository was called despite the invalid date; validation must run before storage")
			case !tc.wantErr && err != nil:
				t.Fatalf("CreateWarranty(starts=%q expires=%q) = %v, want success", tc.startsOn, tc.expiresOn, err)
			}

			updateRepo := fakeWarrantyRepository{
				updateFn: func(context.Context, storage.UpdateWarrantyParams) (storage.Warranty, error) {
					return storage.Warranty{}, nil
				},
			}
			_, err = UpdateWarranty(t.Context(), updateRepo, UpdateWarrantyRequest{
				ItemID: "i1", StartsOn: tc.startsOn, ExpiresOn: tc.expiresOn, ExpectedVersion: 1,
			})
			if tc.wantErr != errors.Is(err, ErrWarrantyDateInvalid) {
				t.Fatalf("UpdateWarranty(starts=%q expires=%q) = %v, wantErr = %t -- create and update must judge a date identically", tc.startsOn, tc.expiresOn, err, tc.wantErr)
			}
		})
	}
}

func TestCreateWarrantyAcceptsLifetimeWithAnExpiry(t *testing.T) {
	repo := fakeWarrantyRepository{
		createFn: func(context.Context, storage.CreateWarrantyParams) (storage.Warranty, error) {
			return storage.Warranty{}, nil
		},
	}
	if _, err := CreateWarranty(t.Context(), repo, CreateWarrantyRequest{
		ItemID: "i1", IsLifetime: true, ExpiresOn: "2030-01-01",
	}); err != nil {
		t.Fatalf("CreateWarranty(lifetime + expiry) = %v, want success (A93)", err)
	}
}

func TestUpdateWarrantyRejectsMissingVersion(t *testing.T) {
	repo := fakeWarrantyRepository{
		updateFn: func(context.Context, storage.UpdateWarrantyParams) (storage.Warranty, error) {
			t.Fatal("repo.Update was called; UpdateWarranty should have rejected the missing version first")
			return storage.Warranty{}, nil
		},
	}
	_, err := UpdateWarranty(t.Context(), repo, UpdateWarrantyRequest{ItemID: "i1", ExpectedVersion: 0})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("UpdateWarranty(ExpectedVersion=0) = %v, want ErrVersionRequired", err)
	}
}

func TestUpdateWarrantyPassesThroughStorageResult(t *testing.T) {
	want := storage.Warranty{ItemID: "i1", Holder: "Bob", Version: 2}
	var got storage.UpdateWarrantyParams
	repo := fakeWarrantyRepository{
		updateFn: func(_ context.Context, p storage.UpdateWarrantyParams) (storage.Warranty, error) {
			got = p
			return want, nil
		},
	}

	out, err := UpdateWarranty(t.Context(), repo, UpdateWarrantyRequest{
		ItemID: "i1", Holder: "Bob", Provider: "Acme", Notes: "n", IsLifetime: true, ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdateWarranty: %v", err)
	}
	if out != want {
		t.Errorf("returned %+v, want %+v", out, want)
	}
	if got.ExpectedVersion != 1 || got.Holder != "Bob" || !got.IsLifetime || got.Now <= 0 {
		t.Errorf("params = %+v, want the request's fields plus a positive Now", got)
	}

	sentinel := errors.New("boom")
	failing := fakeWarrantyRepository{
		updateFn: func(context.Context, storage.UpdateWarrantyParams) (storage.Warranty, error) {
			return storage.Warranty{}, sentinel
		},
	}
	if _, err := UpdateWarranty(t.Context(), failing, UpdateWarrantyRequest{ItemID: "i1", ExpectedVersion: 1}); !errors.Is(err, sentinel) {
		t.Errorf("UpdateWarranty error = %v, want the repository's own error passed through", err)
	}
}

func TestWarrantyFunctionsRefuseANilRepository(t *testing.T) {
	if _, err := CreateWarranty(t.Context(), nil, CreateWarrantyRequest{ItemID: "i1"}); err == nil {
		t.Error("CreateWarranty(nil repo) succeeded")
	}
	if _, err := UpdateWarranty(t.Context(), nil, UpdateWarrantyRequest{ItemID: "i1", ExpectedVersion: 1}); err == nil {
		t.Error("UpdateWarranty(nil repo) succeeded")
	}
}
