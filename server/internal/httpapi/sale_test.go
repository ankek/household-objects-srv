package httpapi

import (
	"context"
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/items"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
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

type saleFakeScope struct {
	group storage.GroupID
	repo  storage.SaleRepository
}

func (s saleFakeScope) GroupID() storage.GroupID                      { return s.group }
func (s saleFakeScope) Items() storage.ItemRepository                 { return nil }
func (s saleFakeScope) Warranty() storage.WarrantyRepository          { return nil }
func (s saleFakeScope) Sale() storage.SaleRepository                  { return s.repo }
func (s saleFakeScope) Purchase() storage.PurchaseRepository          { return nil }
func (s saleFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }

func (s saleFakeScope) Identifications() storage.IdentificationRepository { return nil }

func (s saleFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }

func (s saleFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }

func (s saleFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s saleFakeScope) Locations() storage.LocationRepository               { return nil }

func (s saleFakeScope) Labels() storage.LabelRepository           { return nil }
func (s saleFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s saleFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s saleFakeScope) Members() storage.MemberRepository { return nil }

type saleFakeScopes struct{ repo storage.SaleRepository }

func (s saleFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return saleFakeScope{group: g, repo: s.repo}, nil
}

func saleTestConfig(repo storage.SaleRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = saleFakeScopes{repo: repo}
	return cfg
}

const salePath = "/api/v1/items/itm-1/sale"

func TestSaleRoutesAreMounted(t *testing.T) {
	seen := map[string]string{}
	repo := fakeSaleRepository{
		getFn: func(_ context.Context, itemID string) (storage.Sale, error) {
			seen["GET"] = itemID
			return storage.Sale{ItemID: itemID, Version: 1}, nil
		},
		createFn: func(_ context.Context, p storage.CreateSaleParams) (storage.Sale, error) {
			seen["POST"] = p.ItemID
			return storage.Sale{ItemID: p.ItemID, Version: 1}, nil
		},
		updateFn: func(_ context.Context, p storage.UpdateSaleParams) (storage.Sale, error) {
			seen["PUT"] = p.ItemID
			return storage.Sale{ItemID: p.ItemID, Version: 2}, nil
		},
		deleteFn: func(_ context.Context, itemID string, _ int64) error {
			seen["DELETE"] = itemID
			return nil
		},
	}
	h, err := NewRouter(saleTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct {
		method string
		body   any
		want   int
	}{
		{http.MethodGet, nil, http.StatusOK},
		{http.MethodPost, map[string]any{"buyer_name": "bea"}, http.StatusCreated},
		{http.MethodPut, map[string]any{"buyer_name": "bea", "version": 1}, http.StatusOK},
		{http.MethodDelete, nil, http.StatusNoContent},
	} {
		rec := doJSON(t, h, tc.method, salePath, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: status = %d, want %d: %s", tc.method, salePath, rec.Code, tc.want, rec.Body.String())
		}
		if seen[tc.method] != "itm-1" {
			t.Errorf("%s %s reached its handler with itemID %q, want %q -- the {itemID} wildcard is not being resolved", tc.method, salePath, seen[tc.method], "itm-1")
		}
	}
}

func TestSaleCreateSucceedsWithAnEmptyBody(t *testing.T) {
	var got storage.CreateSaleParams
	repo := fakeSaleRepository{
		createFn: func(_ context.Context, p storage.CreateSaleParams) (storage.Sale, error) {
			got = p
			return storage.Sale{ItemID: p.ItemID, Version: 1}, nil
		},
	}
	h, err := NewRouter(saleTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPost, salePath, map[string]any{})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if got.ItemID != "itm-1" {
		t.Errorf("ItemID = %q, want %q -- the parent item comes from the URL, never the body", got.ItemID, "itm-1")
	}
}

func TestSaleResponseCarriesEveryFR013Field(t *testing.T) {
	row := storage.Sale{
		ItemID: "itm-1", BuyerName: "Bea",
		SoldOn:         sql.NullString{String: "2026-01-15", Valid: true},
		SalePriceMinor: 4599, Notes: "sold at the yard sale", CreatedAt: 111, UpdatedAt: 222, Version: 3,
	}
	repo := fakeSaleRepository{
		getFn: func(context.Context, string) (storage.Sale, error) { return row, nil },
	}
	h, err := NewRouter(saleTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, salePath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	for k, want := range map[string]any{
		"item_id": "itm-1", "buyer_name": "Bea", "sold_on": "2026-01-15",
		"sale_price_minor": float64(4599), "notes": "sold at the yard sale",
		"created_at": float64(111), "updated_at": float64(222), "version": float64(3),
	} {
		if got[k] != want {
			t.Errorf("body[%q] = %#v, want %#v", k, got[k], want)
		}
	}
	if _, present := got["id"]; present {
		t.Error("the response carries an `id`; the block is addressed by its parent item and no route accepts a sale id -- see sale.go's file doc")
	}

	row.SalePriceMinor = 0
	rec = doJSON(t, h, http.MethodGet, salePath, nil)
	got = decodeBody(t, rec)
	if v, present := got["sale_price_minor"]; !present || v != float64(0) {
		t.Errorf("body[\"sale_price_minor\"] = %#v (present=%t), want 0 and present -- an int with omitempty could not distinguish a real zero price from unset", v, present)
	}
	row.SoldOn = sql.NullString{}
	rec = doJSON(t, h, http.MethodGet, salePath, nil)
	got = decodeBody(t, rec)
	if _, present := got["sold_on"]; present {
		t.Errorf("body[\"sold_on\"] is present for a NULL column, want absent")
	}
}

func TestSaleHandlersTranslateStorageErrors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		body       any
		repo       fakeSaleRepository
		wantStatus int
	}{
		{
			name: "get: not found is 404", method: http.MethodGet,
			repo: fakeSaleRepository{getFn: func(context.Context, string) (storage.Sale, error) {
				return storage.Sale{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "get: an unexpected fault is 500", method: http.MethodGet,
			repo: fakeSaleRepository{getFn: func(context.Context, string) (storage.Sale, error) {
				return storage.Sale{}, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "create: an unknown or foreign item is 404", method: http.MethodPost, body: map[string]any{},
			repo: fakeSaleRepository{createFn: func(context.Context, storage.CreateSaleParams) (storage.Sale, error) {
				return storage.Sale{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "create: a second block is 409", method: http.MethodPost, body: map[string]any{},
			repo: fakeSaleRepository{createFn: func(context.Context, storage.CreateSaleParams) (storage.Sale, error) {
				return storage.Sale{}, storage.ErrSaleExists
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "create: a malformed date is 400", method: http.MethodPost, body: map[string]any{"sold_on": "15/01/2026"},
			repo: fakeSaleRepository{createFn: func(context.Context, storage.CreateSaleParams) (storage.Sale, error) {
				t.Error("the repository was called despite an invalid date")
				return storage.Sale{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "create: a negative sale price is 400", method: http.MethodPost, body: map[string]any{"sale_price_minor": -1},
			repo: fakeSaleRepository{createFn: func(context.Context, storage.CreateSaleParams) (storage.Sale, error) {
				t.Error("the repository was called despite a negative sale price")
				return storage.Sale{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: not found is 404", method: http.MethodPut, body: map[string]any{"version": 1},
			repo: fakeSaleRepository{updateFn: func(context.Context, storage.UpdateSaleParams) (storage.Sale, error) {
				return storage.Sale{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "update: a stale version is 409", method: http.MethodPut, body: map[string]any{"version": 1},
			repo: fakeSaleRepository{updateFn: func(context.Context, storage.UpdateSaleParams) (storage.Sale, error) {
				return storage.Sale{}, storage.ErrVersionMismatch
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "update: a missing version is 400", method: http.MethodPut, body: map[string]any{"buyer_name": "a"},
			repo: fakeSaleRepository{updateFn: func(context.Context, storage.UpdateSaleParams) (storage.Sale, error) {
				t.Error("the repository was called despite a missing version")
				return storage.Sale{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "delete: not found is 404", method: http.MethodDelete,
			repo: fakeSaleRepository{deleteFn: func(context.Context, string, int64) error {
				return storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete: an unexpected fault is 500", method: http.MethodDelete,
			repo: fakeSaleRepository{deleteFn: func(context.Context, string, int64) error {
				return sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(saleTestConfig(tc.repo))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			rec := doJSON(t, h, tc.method, salePath, tc.body)
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

func TestSaleUpdateIsAWholeBlockReplace(t *testing.T) {
	var got storage.UpdateSaleParams
	repo := fakeSaleRepository{
		updateFn: func(_ context.Context, p storage.UpdateSaleParams) (storage.Sale, error) {
			got = p
			return storage.Sale{ItemID: p.ItemID, Version: 2}, nil
		},
	}
	h, err := NewRouter(saleTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPut, salePath, map[string]any{"buyer_name": "Carl", "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got.BuyerName != "Carl" {
		t.Errorf("BuyerName = %q, want %q", got.BuyerName, "Carl")
	}
	if got.Notes != "" || got.SoldOn != "" || got.SalePriceMinor != 0 {
		t.Errorf("omitted fields arrived as %+v, want every one of them zero -- PUT is a whole-block replace", got)
	}
	if got.ExpectedVersion != 1 {
		t.Errorf("ExpectedVersion = %d, want 1", got.ExpectedVersion)
	}
}

func TestSaleWriteHandlersRejectMalformedJSON(t *testing.T) {
	repo := fakeSaleRepository{
		createFn: func(context.Context, storage.CreateSaleParams) (storage.Sale, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Sale{}, nil
		},
		updateFn: func(context.Context, storage.UpdateSaleParams) (storage.Sale, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Sale{}, nil
		},
	}
	h, err := NewRouter(saleTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		rec := doRawBody(t, h, method, salePath, []byte("{not json"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s with a malformed body: status = %d, want 400: %s", method, rec.Code, rec.Body.String())
		}
	}
}

var _ = []error{items.ErrVersionRequired, items.ErrSaleDateInvalid, items.ErrSalePriceNegative}
