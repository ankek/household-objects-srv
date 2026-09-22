package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"testing"
)

type fakeSaleRepository struct {
	getFn    func(ctx context.Context, itemID string) (storage.Sale, error)
	createFn func(ctx context.Context, p storage.CreateSaleParams) (storage.Sale, error)
	updateFn func(ctx context.Context, p storage.UpdateSaleParams) (storage.Sale, error)
	deleteFn func(ctx context.Context, itemID string, now int64) error
}

func (f fakeSaleRepository) Get(ctx context.Context, itemID string) (storage.Sale, error) {
	return f.getFn(ctx, itemID)
}

func (f fakeSaleRepository) Create(ctx context.Context, p storage.CreateSaleParams) (storage.Sale, error) {
	return f.createFn(ctx, p)
}

func (f fakeSaleRepository) Update(ctx context.Context, p storage.UpdateSaleParams) (storage.Sale, error) {
	return f.updateFn(ctx, p)
}

func (f fakeSaleRepository) Delete(ctx context.Context, itemID string, now int64) error {
	return f.deleteFn(ctx, itemID, now)
}

func TestCreateSaleRequiresNothingButAnItem(t *testing.T) {
	var got storage.CreateSaleParams
	repo := fakeSaleRepository{
		createFn: func(_ context.Context, p storage.CreateSaleParams) (storage.Sale, error) {
			got = p
			return storage.Sale{ItemID: p.ItemID, Version: 1}, nil
		},
	}

	if _, err := CreateSale(t.Context(), repo, CreateSaleRequest{ItemID: "i1"}); err != nil {
		t.Fatalf("CreateSale(item only) = %v, want success (FR-011/FR-013: no field of the block is mandatory)", err)
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

func TestCreateSalePassesEveryFR013FieldThrough(t *testing.T) {
	var got storage.CreateSaleParams
	repo := fakeSaleRepository{
		createFn: func(_ context.Context, p storage.CreateSaleParams) (storage.Sale, error) {
			got = p
			return storage.Sale{}, nil
		},
	}

	req := CreateSaleRequest{
		ItemID: "i1", BuyerName: "Bea", SoldOn: "2026-01-15", SalePriceMinor: 4599, Notes: "sold at the yard sale",
	}
	if _, err := CreateSale(t.Context(), repo, req); err != nil {
		t.Fatalf("CreateSale: %v", err)
	}
	if got.BuyerName != req.BuyerName || got.Notes != req.Notes {
		t.Errorf("text fields = {%q %q}, want {%q %q}", got.BuyerName, got.Notes, req.BuyerName, req.Notes)
	}
	if got.SoldOn != req.SoldOn || got.SalePriceMinor != req.SalePriceMinor {
		t.Errorf("date/price = {%q %d}, want {%q %d}", got.SoldOn, got.SalePriceMinor, req.SoldOn, req.SalePriceMinor)
	}
}

func TestSaleDateValidation(t *testing.T) {
	cases := []struct {
		name    string
		soldOn  string
		wantErr bool
	}{
		{name: "empty means not recorded", wantErr: false},
		{name: "well-formed ISO day", soldOn: "2026-01-15", wantErr: false},
		{name: "leap day in a leap year", soldOn: "2028-02-29", wantErr: false},
		{name: "day out of range for the month", soldOn: "2026-02-30", wantErr: true},
		{name: "month out of range", soldOn: "2026-13-01", wantErr: true},
		{name: "day-first european format", soldOn: "15/01/2026", wantErr: true},
		{name: "a timestamp rather than a day", soldOn: "2026-01-15T00:00:00Z", wantErr: true},
		{name: "unpadded month", soldOn: "2026-1-15", wantErr: true},
		{name: "unpadded day", soldOn: "2026-01-5", wantErr: true},
		{name: "free text", soldOn: "next tuesday", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			createRepo := fakeSaleRepository{
				createFn: func(context.Context, storage.CreateSaleParams) (storage.Sale, error) {
					reached = true
					return storage.Sale{}, nil
				},
			}
			_, err := CreateSale(t.Context(), createRepo, CreateSaleRequest{ItemID: "i1", SoldOn: tc.soldOn})
			switch {
			case tc.wantErr && !errors.Is(err, ErrSaleDateInvalid):
				t.Fatalf("CreateSale(soldOn=%q) = %v, want ErrSaleDateInvalid", tc.soldOn, err)
			case tc.wantErr && reached:
				t.Fatal("the repository was called despite the invalid date; validation must run before storage")
			case !tc.wantErr && err != nil:
				t.Fatalf("CreateSale(soldOn=%q) = %v, want success", tc.soldOn, err)
			}

			updateRepo := fakeSaleRepository{
				updateFn: func(context.Context, storage.UpdateSaleParams) (storage.Sale, error) {
					return storage.Sale{}, nil
				},
			}
			_, err = UpdateSale(t.Context(), updateRepo, UpdateSaleRequest{ItemID: "i1", SoldOn: tc.soldOn, ExpectedVersion: 1})
			if tc.wantErr != errors.Is(err, ErrSaleDateInvalid) {
				t.Fatalf("UpdateSale(soldOn=%q) = %v, wantErr = %t -- create and update must judge a date identically", tc.soldOn, err, tc.wantErr)
			}
		})
	}
}

func TestSalePriceMustNotBeNegative(t *testing.T) {
	reached := false
	createRepo := fakeSaleRepository{
		createFn: func(context.Context, storage.CreateSaleParams) (storage.Sale, error) {
			reached = true
			return storage.Sale{}, nil
		},
	}
	if _, err := CreateSale(t.Context(), createRepo, CreateSaleRequest{ItemID: "i1", SalePriceMinor: -1}); !errors.Is(err, ErrSalePriceNegative) {
		t.Fatalf("CreateSale(SalePriceMinor=-1) = %v, want ErrSalePriceNegative", err)
	}
	if reached {
		t.Fatal("the repository was called despite a negative sale price")
	}

	updateRepo := fakeSaleRepository{
		updateFn: func(context.Context, storage.UpdateSaleParams) (storage.Sale, error) {
			t.Fatal("repo.Update was called despite a negative sale price")
			return storage.Sale{}, nil
		},
	}
	if _, err := UpdateSale(t.Context(), updateRepo, UpdateSaleRequest{ItemID: "i1", SalePriceMinor: -1, ExpectedVersion: 1}); !errors.Is(err, ErrSalePriceNegative) {
		t.Fatalf("UpdateSale(SalePriceMinor=-1) = %v, want ErrSalePriceNegative", err)
	}
}

func TestUpdateSaleRejectsMissingVersion(t *testing.T) {
	repo := fakeSaleRepository{
		updateFn: func(context.Context, storage.UpdateSaleParams) (storage.Sale, error) {
			t.Fatal("repo.Update was called; UpdateSale should have rejected the missing version first")
			return storage.Sale{}, nil
		},
	}
	_, err := UpdateSale(t.Context(), repo, UpdateSaleRequest{ItemID: "i1", ExpectedVersion: 0})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("UpdateSale(ExpectedVersion=0) = %v, want ErrVersionRequired", err)
	}
}

func TestUpdateSalePassesThroughStorageResult(t *testing.T) {
	want := storage.Sale{ItemID: "i1", BuyerName: "Bea", Version: 2}
	var got storage.UpdateSaleParams
	repo := fakeSaleRepository{
		updateFn: func(_ context.Context, p storage.UpdateSaleParams) (storage.Sale, error) {
			got = p
			return want, nil
		},
	}

	out, err := UpdateSale(t.Context(), repo, UpdateSaleRequest{
		ItemID: "i1", BuyerName: "Bea", Notes: "n", SalePriceMinor: 100, ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdateSale: %v", err)
	}
	if out != want {
		t.Errorf("returned %+v, want %+v", out, want)
	}
	if got.ExpectedVersion != 1 || got.BuyerName != "Bea" || got.SalePriceMinor != 100 || got.Now <= 0 {
		t.Errorf("params = %+v, want the request's fields plus a positive Now", got)
	}

	sentinel := errors.New("boom")
	failing := fakeSaleRepository{
		updateFn: func(context.Context, storage.UpdateSaleParams) (storage.Sale, error) {
			return storage.Sale{}, sentinel
		},
	}
	if _, err := UpdateSale(t.Context(), failing, UpdateSaleRequest{ItemID: "i1", ExpectedVersion: 1}); !errors.Is(err, sentinel) {
		t.Errorf("UpdateSale error = %v, want the repository's own error passed through", err)
	}
}

func TestSaleFunctionsRefuseANilRepository(t *testing.T) {
	if _, err := CreateSale(t.Context(), nil, CreateSaleRequest{ItemID: "i1"}); err == nil {
		t.Error("CreateSale(nil repo) succeeded")
	}
	if _, err := UpdateSale(t.Context(), nil, UpdateSaleRequest{ItemID: "i1", ExpectedVersion: 1}); err == nil {
		t.Error("UpdateSale(nil repo) succeeded")
	}
}
