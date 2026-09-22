package items

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"testing"
)

type fakePurchaseRepository struct {
	getFn    func(ctx context.Context, itemID string) (storage.Purchase, error)
	createFn func(ctx context.Context, p storage.CreatePurchaseParams) (storage.Purchase, error)
	updateFn func(ctx context.Context, p storage.UpdatePurchaseParams) (storage.Purchase, error)
	deleteFn func(ctx context.Context, itemID string, now int64) error
}

func (f fakePurchaseRepository) Get(ctx context.Context, itemID string) (storage.Purchase, error) {
	return f.getFn(ctx, itemID)
}

func (f fakePurchaseRepository) Create(ctx context.Context, p storage.CreatePurchaseParams) (storage.Purchase, error) {
	return f.createFn(ctx, p)
}

func (f fakePurchaseRepository) Update(ctx context.Context, p storage.UpdatePurchaseParams) (storage.Purchase, error) {
	return f.updateFn(ctx, p)
}

func (f fakePurchaseRepository) Delete(ctx context.Context, itemID string, now int64) error {
	return f.deleteFn(ctx, itemID, now)
}

func TestCreatePurchaseRequiresNothingButAnItem(t *testing.T) {
	var got storage.CreatePurchaseParams
	repo := fakePurchaseRepository{
		createFn: func(_ context.Context, p storage.CreatePurchaseParams) (storage.Purchase, error) {
			got = p
			return storage.Purchase{ItemID: p.ItemID, Version: 1}, nil
		},
	}

	if _, err := CreatePurchase(t.Context(), repo, CreatePurchaseRequest{ItemID: "i1"}); err != nil {
		t.Fatalf("CreatePurchase(item only) = %v, want success (FR-011/FR-014: no field of the block is mandatory)", err)
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

func TestCreatePurchasePassesEveryFR014FieldThrough(t *testing.T) {
	var got storage.CreatePurchaseParams
	repo := fakePurchaseRepository{
		createFn: func(_ context.Context, p storage.CreatePurchaseParams) (storage.Purchase, error) {
			got = p
			return storage.Purchase{}, nil
		},
	}

	req := CreatePurchaseRequest{
		ItemID: "i1", Vendor: "Acme Hardware", PurchasedOn: "2026-01-15", PurchasePriceMinor: 4599,
		OrderReference: "ORD-12345", Notes: "picked up in store",
	}
	if _, err := CreatePurchase(t.Context(), repo, req); err != nil {
		t.Fatalf("CreatePurchase: %v", err)
	}
	if got.Vendor != req.Vendor || got.Notes != req.Notes {
		t.Errorf("text fields = {%q %q}, want {%q %q}", got.Vendor, got.Notes, req.Vendor, req.Notes)
	}
	if got.PurchasedOn != req.PurchasedOn || got.PurchasePriceMinor != req.PurchasePriceMinor {
		t.Errorf("date/price = {%q %d}, want {%q %d}", got.PurchasedOn, got.PurchasePriceMinor, req.PurchasedOn, req.PurchasePriceMinor)
	}
	if got.OrderReference != req.OrderReference {
		t.Errorf("OrderReference = %q, want %q", got.OrderReference, req.OrderReference)
	}
}

func TestPurchaseDateValidation(t *testing.T) {
	cases := []struct {
		name        string
		purchasedOn string
		wantErr     bool
	}{
		{name: "empty means not recorded", wantErr: false},
		{name: "well-formed ISO day", purchasedOn: "2026-01-15", wantErr: false},
		{name: "leap day in a leap year", purchasedOn: "2028-02-29", wantErr: false},
		{name: "day out of range for the month", purchasedOn: "2026-02-30", wantErr: true},
		{name: "month out of range", purchasedOn: "2026-13-01", wantErr: true},
		{name: "day-first european format", purchasedOn: "15/01/2026", wantErr: true},
		{name: "a timestamp rather than a day", purchasedOn: "2026-01-15T00:00:00Z", wantErr: true},
		{name: "unpadded month", purchasedOn: "2026-1-15", wantErr: true},
		{name: "unpadded day", purchasedOn: "2026-01-5", wantErr: true},
		{name: "free text", purchasedOn: "next tuesday", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			createRepo := fakePurchaseRepository{
				createFn: func(context.Context, storage.CreatePurchaseParams) (storage.Purchase, error) {
					reached = true
					return storage.Purchase{}, nil
				},
			}
			_, err := CreatePurchase(t.Context(), createRepo, CreatePurchaseRequest{ItemID: "i1", PurchasedOn: tc.purchasedOn})
			switch {
			case tc.wantErr && !errors.Is(err, ErrPurchaseDateInvalid):
				t.Fatalf("CreatePurchase(purchasedOn=%q) = %v, want ErrPurchaseDateInvalid", tc.purchasedOn, err)
			case tc.wantErr && reached:
				t.Fatal("the repository was called despite the invalid date; validation must run before storage")
			case !tc.wantErr && err != nil:
				t.Fatalf("CreatePurchase(purchasedOn=%q) = %v, want success", tc.purchasedOn, err)
			}

			updateRepo := fakePurchaseRepository{
				updateFn: func(context.Context, storage.UpdatePurchaseParams) (storage.Purchase, error) {
					return storage.Purchase{}, nil
				},
			}
			_, err = UpdatePurchase(t.Context(), updateRepo, UpdatePurchaseRequest{ItemID: "i1", PurchasedOn: tc.purchasedOn, ExpectedVersion: 1})
			if tc.wantErr != errors.Is(err, ErrPurchaseDateInvalid) {
				t.Fatalf("UpdatePurchase(purchasedOn=%q) = %v, wantErr = %t -- create and update must judge a date identically", tc.purchasedOn, err, tc.wantErr)
			}
		})
	}
}

func TestPurchasePriceMustNotBeNegative(t *testing.T) {
	reached := false
	createRepo := fakePurchaseRepository{
		createFn: func(context.Context, storage.CreatePurchaseParams) (storage.Purchase, error) {
			reached = true
			return storage.Purchase{}, nil
		},
	}
	if _, err := CreatePurchase(t.Context(), createRepo, CreatePurchaseRequest{ItemID: "i1", PurchasePriceMinor: -1}); !errors.Is(err, ErrPurchasePriceNegative) {
		t.Fatalf("CreatePurchase(PurchasePriceMinor=-1) = %v, want ErrPurchasePriceNegative", err)
	}
	if reached {
		t.Fatal("the repository was called despite a negative purchase price")
	}

	updateRepo := fakePurchaseRepository{
		updateFn: func(context.Context, storage.UpdatePurchaseParams) (storage.Purchase, error) {
			t.Fatal("repo.Update was called despite a negative purchase price")
			return storage.Purchase{}, nil
		},
	}
	if _, err := UpdatePurchase(t.Context(), updateRepo, UpdatePurchaseRequest{ItemID: "i1", PurchasePriceMinor: -1, ExpectedVersion: 1}); !errors.Is(err, ErrPurchasePriceNegative) {
		t.Fatalf("UpdatePurchase(PurchasePriceMinor=-1) = %v, want ErrPurchasePriceNegative", err)
	}
}

func TestUpdatePurchaseRejectsMissingVersion(t *testing.T) {
	repo := fakePurchaseRepository{
		updateFn: func(context.Context, storage.UpdatePurchaseParams) (storage.Purchase, error) {
			t.Fatal("repo.Update was called; UpdatePurchase should have rejected the missing version first")
			return storage.Purchase{}, nil
		},
	}
	_, err := UpdatePurchase(t.Context(), repo, UpdatePurchaseRequest{ItemID: "i1", ExpectedVersion: 0})
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("UpdatePurchase(ExpectedVersion=0) = %v, want ErrVersionRequired", err)
	}
}

func TestUpdatePurchasePassesThroughStorageResult(t *testing.T) {
	want := storage.Purchase{ItemID: "i1", Vendor: "Acme Hardware", Version: 2}
	var got storage.UpdatePurchaseParams
	repo := fakePurchaseRepository{
		updateFn: func(_ context.Context, p storage.UpdatePurchaseParams) (storage.Purchase, error) {
			got = p
			return want, nil
		},
	}

	out, err := UpdatePurchase(t.Context(), repo, UpdatePurchaseRequest{
		ItemID: "i1", Vendor: "Acme Hardware", Notes: "n", PurchasePriceMinor: 100, ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdatePurchase: %v", err)
	}
	if out != want {
		t.Errorf("returned %+v, want %+v", out, want)
	}
	if got.ExpectedVersion != 1 || got.Vendor != "Acme Hardware" || got.PurchasePriceMinor != 100 || got.Now <= 0 {
		t.Errorf("params = %+v, want the request's fields plus a positive Now", got)
	}

	sentinel := errors.New("boom")
	failing := fakePurchaseRepository{
		updateFn: func(context.Context, storage.UpdatePurchaseParams) (storage.Purchase, error) {
			return storage.Purchase{}, sentinel
		},
	}
	if _, err := UpdatePurchase(t.Context(), failing, UpdatePurchaseRequest{ItemID: "i1", ExpectedVersion: 1}); !errors.Is(err, sentinel) {
		t.Errorf("UpdatePurchase error = %v, want the repository's own error passed through", err)
	}
}

func TestPurchaseFunctionsRefuseANilRepository(t *testing.T) {
	if _, err := CreatePurchase(t.Context(), nil, CreatePurchaseRequest{ItemID: "i1"}); err == nil {
		t.Error("CreatePurchase(nil repo) succeeded")
	}
	if _, err := UpdatePurchase(t.Context(), nil, UpdatePurchaseRequest{ItemID: "i1", ExpectedVersion: 1}); err == nil {
		t.Error("UpdatePurchase(nil repo) succeeded")
	}
}
