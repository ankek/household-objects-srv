package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeStockAdjustmentRepository struct {
	listFn   func(ctx context.Context, itemID string) ([]storage.StockAdjustment, error)
	createFn func(ctx context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error)
}

func (f fakeStockAdjustmentRepository) List(ctx context.Context, itemID string) ([]storage.StockAdjustment, error) {
	return f.listFn(ctx, itemID)
}

func (f fakeStockAdjustmentRepository) Create(ctx context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
	return f.createFn(ctx, p)
}

type stockAdjustmentFakeScope struct {
	group storage.GroupID
	repo  storage.StockAdjustmentRepository
}

func (s stockAdjustmentFakeScope) GroupID() storage.GroupID                          { return s.group }
func (s stockAdjustmentFakeScope) Items() storage.ItemRepository                     { return nil }
func (s stockAdjustmentFakeScope) Warranty() storage.WarrantyRepository              { return nil }
func (s stockAdjustmentFakeScope) Sale() storage.SaleRepository                      { return nil }
func (s stockAdjustmentFakeScope) Purchase() storage.PurchaseRepository              { return nil }
func (s stockAdjustmentFakeScope) Visibility() storage.GroupVisibilityRepository     { return nil }
func (s stockAdjustmentFakeScope) Identifications() storage.IdentificationRepository { return nil }
func (s stockAdjustmentFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }
func (s stockAdjustmentFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository {
	return nil
}
func (s stockAdjustmentFakeScope) StockAdjustments() storage.StockAdjustmentRepository {
	return s.repo
}
func (s stockAdjustmentFakeScope) Locations() storage.LocationRepository { return nil }

func (s stockAdjustmentFakeScope) Labels() storage.LabelRepository           { return nil }
func (s stockAdjustmentFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s stockAdjustmentFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s stockAdjustmentFakeScope) Members() storage.MemberRepository { return nil }

type stockAdjustmentFakeScopes struct {
	repo storage.StockAdjustmentRepository
}

func (s stockAdjustmentFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return stockAdjustmentFakeScope{group: g, repo: s.repo}, nil
}

func stockAdjustmentTestConfig(repo storage.StockAdjustmentRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = stockAdjustmentFakeScopes{repo: repo}
	return cfg
}

const stockAdjustmentCollectionPath = "/api/v1/items/itm-1/stock-adjustments"

func TestStockAdjustmentRoutesAreMounted(t *testing.T) {
	seen := map[string]string{}
	repo := fakeStockAdjustmentRepository{
		listFn: func(_ context.Context, itemID string) ([]storage.StockAdjustment, error) {
			seen["GET-list"] = itemID
			return []storage.StockAdjustment{}, nil
		},
		createFn: func(_ context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			seen["POST"] = p.ItemID
			return storage.StockAdjustment{ID: "sa-1", ItemID: p.ItemID, Delta: p.Delta, Reason: p.Reason, Note: p.Note, ResultingQuantity: p.Delta, Version: 1}, nil
		},
	}
	h, err := NewRouter(stockAdjustmentTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct {
		method string
		body   any
		want   int
	}{
		{http.MethodGet, nil, http.StatusOK},
		{http.MethodPost, map[string]any{"delta": 3, "reason": "restock", "note": "found more in the garage"}, http.StatusCreated},
	} {
		rec := doJSON(t, h, tc.method, stockAdjustmentCollectionPath, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: status = %d, want %d: %s", tc.method, stockAdjustmentCollectionPath, rec.Code, tc.want, rec.Body.String())
		}
	}
	if seen["GET-list"] != "itm-1" {
		t.Errorf("GET reached its handler with itemID %q, want %q -- the {itemID} wildcard is not being resolved", seen["GET-list"], "itm-1")
	}
	if seen["POST"] != "itm-1" {
		t.Errorf("POST reached its handler with itemID %q, want %q", seen["POST"], "itm-1")
	}
}

func TestStockAdjustmentListRendersAnEmptyArrayNotNull(t *testing.T) {
	repo := fakeStockAdjustmentRepository{
		listFn: func(context.Context, string) ([]storage.StockAdjustment, error) { return nil, nil },
	}
	h, err := NewRouter(stockAdjustmentTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodGet, stockAdjustmentCollectionPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"stock_adjustments":[]}`+"\n" && got != `{"stock_adjustments":[]}` {
		t.Errorf("body = %q, want an empty array, never null", got)
	}
}

func TestStockAdjustmentCreatePassesDeltaReasonNoteThrough(t *testing.T) {
	var got storage.CreateStockAdjustmentParams
	repo := fakeStockAdjustmentRepository{
		createFn: func(_ context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			got = p
			return storage.StockAdjustment{ID: "sa-1", ItemID: p.ItemID, Delta: p.Delta, Reason: p.Reason, Note: p.Note, ResultingQuantity: 7, Version: 1}, nil
		},
	}
	h, err := NewRouter(stockAdjustmentTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, stockAdjustmentCollectionPath, map[string]any{
		"delta": -2, "reason": "damaged", "note": "one unit broke in transit",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if got.ItemID != "itm-1" || got.Delta != -2 || got.Reason != "damaged" || got.Note != "one unit broke in transit" {
		t.Errorf("got %+v, want {ItemID:itm-1 Delta:-2 Reason:damaged Note:\"one unit broke in transit\"}", got)
	}

	var body stockAdjustmentBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "sa-1" || body.ResultingQuantity != 7 {
		t.Errorf("response body = %+v, want {id:sa-1 resulting_quantity:7 ...}", body)
	}
}

func TestStockAdjustmentCreateAcceptsAZeroDelta(t *testing.T) {
	repo := fakeStockAdjustmentRepository{
		createFn: func(_ context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			return storage.StockAdjustment{ID: "sa-1", ItemID: p.ItemID, Delta: 0, Reason: p.Reason, ResultingQuantity: 5, Version: 1}, nil
		},
	}
	h, err := NewRouter(stockAdjustmentTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, stockAdjustmentCollectionPath, map[string]any{
		"delta": 0, "reason": "recounted, no change",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 -- a zero delta carries no non-zero requirement under FR-018: %s", rec.Code, rec.Body.String())
	}
}

func TestStockAdjustmentHandlersTranslateStorageErrors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		body       any
		repo       fakeStockAdjustmentRepository
		wantStatus int
	}{
		{
			name: "list: not found is 404", method: http.MethodGet,
			repo: fakeStockAdjustmentRepository{listFn: func(context.Context, string) ([]storage.StockAdjustment, error) {
				return nil, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "list: an unexpected fault is 500", method: http.MethodGet,
			repo: fakeStockAdjustmentRepository{listFn: func(context.Context, string) ([]storage.StockAdjustment, error) {
				return nil, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "create: an unknown or foreign item is 404", method: http.MethodPost,
			body: map[string]any{"delta": 1},
			repo: fakeStockAdjustmentRepository{createFn: func(context.Context, storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
				return storage.StockAdjustment{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "create: an unexpected fault is 500", method: http.MethodPost,
			body: map[string]any{"delta": 1},
			repo: fakeStockAdjustmentRepository{createFn: func(context.Context, storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
				return storage.StockAdjustment{}, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(stockAdjustmentTestConfig(tc.repo))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			rec := doJSON(t, h, tc.method, stockAdjustmentCollectionPath, tc.body)
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

func TestStockAdjustmentWriteHandlerRejectsMalformedJSON(t *testing.T) {
	repo := fakeStockAdjustmentRepository{
		createFn: func(context.Context, storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.StockAdjustment{}, nil
		},
	}
	h, err := NewRouter(stockAdjustmentTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, stockAdjustmentCollectionPath, strings.NewReader("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST with a malformed body: status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestStockAdjustmentCreateIsWritableByAMember(t *testing.T) {
	repo := fakeStockAdjustmentRepository{
		createFn: func(_ context.Context, p storage.CreateStockAdjustmentParams) (storage.StockAdjustment, error) {
			return storage.StockAdjustment{ID: "sa-1", ItemID: p.ItemID, Delta: p.Delta, ResultingQuantity: p.Delta, Version: 1}, nil
		},
	}
	cfg := stockAdjustmentTestConfig(repo)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{"delta": 1, "reason": "restock"}); err != nil {
		t.Fatalf("encode request body: %v", err)
	}
	req := asTestUser(httptest.NewRequest(http.MethodPost, stockAdjustmentCollectionPath, &buf), "member")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("POST %s as a member: status = 403, want 201 -- stock adjustments are member-writable "+
			"item data (the SAME posture identifications/item-custom-fields have): a 403 here means this "+
			"route was mounted inside middleware.RequireOwner by mistake", stockAdjustmentCollectionPath)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s as a member: status = %d, want 201; body = %s", stockAdjustmentCollectionPath, rec.Code, rec.Body.String())
	}
}
