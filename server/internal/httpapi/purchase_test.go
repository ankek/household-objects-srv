package httpapi

import (
	"context"
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/items"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
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

type purchaseFakeScope struct {
	group storage.GroupID
	repo  storage.PurchaseRepository
}

func (s purchaseFakeScope) GroupID() storage.GroupID                      { return s.group }
func (s purchaseFakeScope) Items() storage.ItemRepository                 { return nil }
func (s purchaseFakeScope) Warranty() storage.WarrantyRepository          { return nil }
func (s purchaseFakeScope) Sale() storage.SaleRepository                  { return nil }
func (s purchaseFakeScope) Purchase() storage.PurchaseRepository          { return s.repo }
func (s purchaseFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }

func (s purchaseFakeScope) Identifications() storage.IdentificationRepository { return nil }

func (s purchaseFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }

func (s purchaseFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }

func (s purchaseFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s purchaseFakeScope) Locations() storage.LocationRepository               { return nil }

func (s purchaseFakeScope) Labels() storage.LabelRepository           { return nil }
func (s purchaseFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s purchaseFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s purchaseFakeScope) Members() storage.MemberRepository { return nil }

type purchaseFakeScopes struct{ repo storage.PurchaseRepository }

func (s purchaseFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return purchaseFakeScope{group: g, repo: s.repo}, nil
}

func purchaseTestConfig(repo storage.PurchaseRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = purchaseFakeScopes{repo: repo}
	return cfg
}

const purchasePath = "/api/v1/items/itm-1/purchase"

func TestPurchaseRoutesAreMounted(t *testing.T) {
	seen := map[string]string{}
	repo := fakePurchaseRepository{
		getFn: func(_ context.Context, itemID string) (storage.Purchase, error) {
			seen["GET"] = itemID
			return storage.Purchase{ItemID: itemID, Version: 1}, nil
		},
		createFn: func(_ context.Context, p storage.CreatePurchaseParams) (storage.Purchase, error) {
			seen["POST"] = p.ItemID
			return storage.Purchase{ItemID: p.ItemID, Version: 1}, nil
		},
		updateFn: func(_ context.Context, p storage.UpdatePurchaseParams) (storage.Purchase, error) {
			seen["PUT"] = p.ItemID
			return storage.Purchase{ItemID: p.ItemID, Version: 2}, nil
		},
		deleteFn: func(_ context.Context, itemID string, _ int64) error {
			seen["DELETE"] = itemID
			return nil
		},
	}
	h, err := NewRouter(purchaseTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct {
		method string
		body   any
		want   int
	}{
		{http.MethodGet, nil, http.StatusOK},
		{http.MethodPost, map[string]any{"vendor": "acme"}, http.StatusCreated},
		{http.MethodPut, map[string]any{"vendor": "acme", "version": 1}, http.StatusOK},
		{http.MethodDelete, nil, http.StatusNoContent},
	} {
		rec := doJSON(t, h, tc.method, purchasePath, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: status = %d, want %d: %s", tc.method, purchasePath, rec.Code, tc.want, rec.Body.String())
		}
		if seen[tc.method] != "itm-1" {
			t.Errorf("%s %s reached its handler with itemID %q, want %q -- the {itemID} wildcard is not being resolved", tc.method, purchasePath, seen[tc.method], "itm-1")
		}
	}
}

func TestPurchaseCreateSucceedsWithAnEmptyBody(t *testing.T) {
	var got storage.CreatePurchaseParams
	repo := fakePurchaseRepository{
		createFn: func(_ context.Context, p storage.CreatePurchaseParams) (storage.Purchase, error) {
			got = p
			return storage.Purchase{ItemID: p.ItemID, Version: 1}, nil
		},
	}
	h, err := NewRouter(purchaseTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPost, purchasePath, map[string]any{})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if got.ItemID != "itm-1" {
		t.Errorf("ItemID = %q, want %q -- the parent item comes from the URL, never the body", got.ItemID, "itm-1")
	}
}

func TestPurchaseResponseCarriesEveryFR014Field(t *testing.T) {
	row := storage.Purchase{
		ItemID: "itm-1", Vendor: "Acme Hardware",
		PurchasedOn:        sql.NullString{String: "2026-01-15", Valid: true},
		PurchasePriceMinor: 4599, OrderReference: "ORD-12345", Notes: "picked up in store",
		CreatedAt: 111, UpdatedAt: 222, Version: 3,
	}
	repo := fakePurchaseRepository{
		getFn: func(context.Context, string) (storage.Purchase, error) { return row, nil },
	}
	h, err := NewRouter(purchaseTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, purchasePath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	for k, want := range map[string]any{
		"item_id": "itm-1", "vendor": "Acme Hardware", "purchased_on": "2026-01-15",
		"purchase_price_minor": float64(4599), "order_reference": "ORD-12345", "notes": "picked up in store",
		"created_at": float64(111), "updated_at": float64(222), "version": float64(3),
	} {
		if got[k] != want {
			t.Errorf("body[%q] = %#v, want %#v", k, got[k], want)
		}
	}
	if _, present := got["id"]; present {
		t.Error("the response carries an `id`; the block is addressed by its parent item and no route accepts a purchase id -- see purchase.go's file doc")
	}

	row.PurchasePriceMinor = 0
	rec = doJSON(t, h, http.MethodGet, purchasePath, nil)
	got = decodeBody(t, rec)
	if v, present := got["purchase_price_minor"]; !present || v != float64(0) {
		t.Errorf("body[\"purchase_price_minor\"] = %#v (present=%t), want 0 and present -- an int with omitempty could not distinguish a real zero price from unset", v, present)
	}
	row.PurchasedOn = sql.NullString{}
	rec = doJSON(t, h, http.MethodGet, purchasePath, nil)
	got = decodeBody(t, rec)
	if _, present := got["purchased_on"]; present {
		t.Errorf("body[\"purchased_on\"] is present for a NULL column, want absent")
	}
}

func TestPurchaseHandlersTranslateStorageErrors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		body       any
		repo       fakePurchaseRepository
		wantStatus int
	}{
		{
			name: "get: not found is 404", method: http.MethodGet,
			repo: fakePurchaseRepository{getFn: func(context.Context, string) (storage.Purchase, error) {
				return storage.Purchase{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "get: an unexpected fault is 500", method: http.MethodGet,
			repo: fakePurchaseRepository{getFn: func(context.Context, string) (storage.Purchase, error) {
				return storage.Purchase{}, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "create: an unknown or foreign item is 404", method: http.MethodPost, body: map[string]any{},
			repo: fakePurchaseRepository{createFn: func(context.Context, storage.CreatePurchaseParams) (storage.Purchase, error) {
				return storage.Purchase{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "create: a second block is 409", method: http.MethodPost, body: map[string]any{},
			repo: fakePurchaseRepository{createFn: func(context.Context, storage.CreatePurchaseParams) (storage.Purchase, error) {
				return storage.Purchase{}, storage.ErrPurchaseExists
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "create: a malformed date is 400", method: http.MethodPost, body: map[string]any{"purchased_on": "15/01/2026"},
			repo: fakePurchaseRepository{createFn: func(context.Context, storage.CreatePurchaseParams) (storage.Purchase, error) {
				t.Error("the repository was called despite an invalid date")
				return storage.Purchase{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "create: a negative purchase price is 400", method: http.MethodPost, body: map[string]any{"purchase_price_minor": -1},
			repo: fakePurchaseRepository{createFn: func(context.Context, storage.CreatePurchaseParams) (storage.Purchase, error) {
				t.Error("the repository was called despite a negative purchase price")
				return storage.Purchase{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: not found is 404", method: http.MethodPut, body: map[string]any{"version": 1},
			repo: fakePurchaseRepository{updateFn: func(context.Context, storage.UpdatePurchaseParams) (storage.Purchase, error) {
				return storage.Purchase{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "update: a stale version is 409", method: http.MethodPut, body: map[string]any{"version": 1},
			repo: fakePurchaseRepository{updateFn: func(context.Context, storage.UpdatePurchaseParams) (storage.Purchase, error) {
				return storage.Purchase{}, storage.ErrVersionMismatch
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "update: a missing version is 400", method: http.MethodPut, body: map[string]any{"vendor": "a"},
			repo: fakePurchaseRepository{updateFn: func(context.Context, storage.UpdatePurchaseParams) (storage.Purchase, error) {
				t.Error("the repository was called despite a missing version")
				return storage.Purchase{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "delete: not found is 404", method: http.MethodDelete,
			repo: fakePurchaseRepository{deleteFn: func(context.Context, string, int64) error {
				return storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete: an unexpected fault is 500", method: http.MethodDelete,
			repo: fakePurchaseRepository{deleteFn: func(context.Context, string, int64) error {
				return sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(purchaseTestConfig(tc.repo))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			rec := doJSON(t, h, tc.method, purchasePath, tc.body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusNotFound {
				p := decodeProblem(t, rec)
				if p.Detail != "" {
					t.Errorf("404 carries detail %q; a not-found must say nothing request-specific (NFR-010)", p.Detail)
				}
			}
		})
	}
}

func TestPurchaseUpdateIsAWholeBlockReplace(t *testing.T) {
	var got storage.UpdatePurchaseParams
	repo := fakePurchaseRepository{
		updateFn: func(_ context.Context, p storage.UpdatePurchaseParams) (storage.Purchase, error) {
			got = p
			return storage.Purchase{ItemID: p.ItemID, Version: 2}, nil
		},
	}
	h, err := NewRouter(purchaseTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPut, purchasePath, map[string]any{"vendor": "Carl's Vendor", "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got.Vendor != "Carl's Vendor" {
		t.Errorf("Vendor = %q, want %q", got.Vendor, "Carl's Vendor")
	}
	if got.Notes != "" || got.PurchasedOn != "" || got.PurchasePriceMinor != 0 || got.OrderReference != "" {
		t.Errorf("omitted fields arrived as %+v, want every one of them zero -- PUT is a whole-block replace", got)
	}
	if got.ExpectedVersion != 1 {
		t.Errorf("ExpectedVersion = %d, want 1", got.ExpectedVersion)
	}
}

func TestPurchaseWriteHandlersRejectMalformedJSON(t *testing.T) {
	repo := fakePurchaseRepository{
		createFn: func(context.Context, storage.CreatePurchaseParams) (storage.Purchase, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Purchase{}, nil
		},
		updateFn: func(context.Context, storage.UpdatePurchaseParams) (storage.Purchase, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Purchase{}, nil
		},
	}
	h, err := NewRouter(purchaseTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		rec := doRawBody(t, h, method, purchasePath, []byte("{not json"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s with a malformed body: status = %d, want 400: %s", method, rec.Code, rec.Body.String())
		}
	}
}

var _ = []error{items.ErrVersionRequired, items.ErrPurchaseDateInvalid, items.ErrPurchasePriceNegative}
