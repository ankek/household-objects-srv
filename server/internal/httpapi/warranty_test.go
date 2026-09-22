package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/items"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
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

type warrantyFakeScope struct {
	group storage.GroupID
	repo  storage.WarrantyRepository
}

func (s warrantyFakeScope) GroupID() storage.GroupID             { return s.group }
func (s warrantyFakeScope) Items() storage.ItemRepository        { return nil }
func (s warrantyFakeScope) Warranty() storage.WarrantyRepository { return s.repo }

func (s warrantyFakeScope) Sale() storage.SaleRepository { return nil }

func (s warrantyFakeScope) Purchase() storage.PurchaseRepository          { return nil }
func (s warrantyFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }

func (s warrantyFakeScope) Identifications() storage.IdentificationRepository { return nil }

func (s warrantyFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }

func (s warrantyFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }

func (s warrantyFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s warrantyFakeScope) Locations() storage.LocationRepository               { return nil }

func (s warrantyFakeScope) Labels() storage.LabelRepository           { return nil }
func (s warrantyFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s warrantyFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s warrantyFakeScope) Members() storage.MemberRepository { return nil }

type warrantyFakeScopes struct{ repo storage.WarrantyRepository }

func (s warrantyFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return warrantyFakeScope{group: g, repo: s.repo}, nil
}

func warrantyTestConfig(repo storage.WarrantyRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = warrantyFakeScopes{repo: repo}
	return cfg
}

const warrantyPath = "/api/v1/items/itm-1/warranty"

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	return out
}

func doRawBody(t *testing.T, h http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWarrantyRoutesAreMounted(t *testing.T) {
	seen := map[string]string{}
	repo := fakeWarrantyRepository{
		getFn: func(_ context.Context, itemID string) (storage.Warranty, error) {
			seen["GET"] = itemID
			return storage.Warranty{ItemID: itemID, Version: 1}, nil
		},
		createFn: func(_ context.Context, p storage.CreateWarrantyParams) (storage.Warranty, error) {
			seen["POST"] = p.ItemID
			return storage.Warranty{ItemID: p.ItemID, Version: 1}, nil
		},
		updateFn: func(_ context.Context, p storage.UpdateWarrantyParams) (storage.Warranty, error) {
			seen["PUT"] = p.ItemID
			return storage.Warranty{ItemID: p.ItemID, Version: 2}, nil
		},
		deleteFn: func(_ context.Context, itemID string, _ int64) error {
			seen["DELETE"] = itemID
			return nil
		},
	}
	h, err := NewRouter(warrantyTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct {
		method string
		body   any
		want   int
	}{
		{http.MethodGet, nil, http.StatusOK},
		{http.MethodPost, map[string]any{"holder": "alice"}, http.StatusCreated},
		{http.MethodPut, map[string]any{"holder": "alice", "version": 1}, http.StatusOK},
		{http.MethodDelete, nil, http.StatusNoContent},
	} {
		rec := doJSON(t, h, tc.method, warrantyPath, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: status = %d, want %d: %s", tc.method, warrantyPath, rec.Code, tc.want, rec.Body.String())
		}
		if seen[tc.method] != "itm-1" {
			t.Errorf("%s %s reached its handler with itemID %q, want %q -- the {itemID} wildcard is not being resolved", tc.method, warrantyPath, seen[tc.method], "itm-1")
		}
	}
}

func TestWarrantyCreateSucceedsWithAnEmptyBody(t *testing.T) {
	var got storage.CreateWarrantyParams
	repo := fakeWarrantyRepository{
		createFn: func(_ context.Context, p storage.CreateWarrantyParams) (storage.Warranty, error) {
			got = p
			return storage.Warranty{ItemID: p.ItemID, Version: 1}, nil
		},
	}
	h, err := NewRouter(warrantyTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPost, warrantyPath, map[string]any{})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if got.ItemID != "itm-1" {
		t.Errorf("ItemID = %q, want %q -- the parent item comes from the URL, never the body", got.ItemID, "itm-1")
	}
}

func TestWarrantyResponseCarriesEveryFR012Field(t *testing.T) {
	row := storage.Warranty{
		ItemID: "itm-1", Holder: "Alice", Provider: "Acme",
		StartsOn:   sql.NullString{String: "2026-01-15", Valid: true},
		ExpiresOn:  sql.NullString{String: "2029-01-14", Valid: true},
		IsLifetime: 1, Notes: "in the drawer", CreatedAt: 111, UpdatedAt: 222, Version: 3,
	}
	repo := fakeWarrantyRepository{
		getFn: func(context.Context, string) (storage.Warranty, error) { return row, nil },
	}
	h, err := NewRouter(warrantyTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, warrantyPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	for k, want := range map[string]any{
		"item_id": "itm-1", "holder": "Alice", "provider": "Acme",
		"starts_on": "2026-01-15", "expires_on": "2029-01-14",
		"is_lifetime": true, "notes": "in the drawer",
		"created_at": float64(111), "updated_at": float64(222), "version": float64(3),
	} {
		if got[k] != want {
			t.Errorf("body[%q] = %#v, want %#v", k, got[k], want)
		}
	}
	if _, present := got["id"]; present {
		t.Error("the response carries an `id`; the block is addressed by its parent item and no route accepts a warranty id -- see warranty.go's file doc")
	}

	row.IsLifetime = 0
	rec = doJSON(t, h, http.MethodGet, warrantyPath, nil)
	got = decodeBody(t, rec)
	if v, present := got["is_lifetime"]; !present || v != false {
		t.Errorf("body[\"is_lifetime\"] = %#v (present=%t), want false and present -- a bool with omitempty is a tri-state on the wire", v, present)
	}
	row.StartsOn, row.ExpiresOn = sql.NullString{}, sql.NullString{}
	rec = doJSON(t, h, http.MethodGet, warrantyPath, nil)
	got = decodeBody(t, rec)
	if _, present := got["starts_on"]; present {
		t.Errorf("body[\"starts_on\"] is present for a NULL column, want absent")
	}
}

func TestWarrantyHandlersTranslateStorageErrors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		body       any
		repo       fakeWarrantyRepository
		wantStatus int
	}{
		{
			name: "get: not found is 404", method: http.MethodGet,
			repo: fakeWarrantyRepository{getFn: func(context.Context, string) (storage.Warranty, error) {
				return storage.Warranty{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "get: an unexpected fault is 500", method: http.MethodGet,
			repo: fakeWarrantyRepository{getFn: func(context.Context, string) (storage.Warranty, error) {
				return storage.Warranty{}, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "create: an unknown or foreign item is 404", method: http.MethodPost, body: map[string]any{},
			repo: fakeWarrantyRepository{createFn: func(context.Context, storage.CreateWarrantyParams) (storage.Warranty, error) {
				return storage.Warranty{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "create: a second block is 409", method: http.MethodPost, body: map[string]any{},
			repo: fakeWarrantyRepository{createFn: func(context.Context, storage.CreateWarrantyParams) (storage.Warranty, error) {
				return storage.Warranty{}, storage.ErrWarrantyExists
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "create: a malformed date is 400", method: http.MethodPost, body: map[string]any{"starts_on": "15/01/2026"},
			repo: fakeWarrantyRepository{createFn: func(context.Context, storage.CreateWarrantyParams) (storage.Warranty, error) {
				t.Error("the repository was called despite an invalid date")
				return storage.Warranty{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: not found is 404", method: http.MethodPut, body: map[string]any{"version": 1},
			repo: fakeWarrantyRepository{updateFn: func(context.Context, storage.UpdateWarrantyParams) (storage.Warranty, error) {
				return storage.Warranty{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "update: a stale version is 409", method: http.MethodPut, body: map[string]any{"version": 1},
			repo: fakeWarrantyRepository{updateFn: func(context.Context, storage.UpdateWarrantyParams) (storage.Warranty, error) {
				return storage.Warranty{}, storage.ErrVersionMismatch
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "update: a missing version is 400", method: http.MethodPut, body: map[string]any{"holder": "a"},
			repo: fakeWarrantyRepository{updateFn: func(context.Context, storage.UpdateWarrantyParams) (storage.Warranty, error) {
				t.Error("the repository was called despite a missing version")
				return storage.Warranty{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "delete: not found is 404", method: http.MethodDelete,
			repo: fakeWarrantyRepository{deleteFn: func(context.Context, string, int64) error {
				return storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete: an unexpected fault is 500", method: http.MethodDelete,
			repo: fakeWarrantyRepository{deleteFn: func(context.Context, string, int64) error {
				return sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(warrantyTestConfig(tc.repo))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			rec := doJSON(t, h, tc.method, warrantyPath, tc.body)
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

func TestWarrantyUpdateIsAWholeBlockReplace(t *testing.T) {
	var got storage.UpdateWarrantyParams
	repo := fakeWarrantyRepository{
		updateFn: func(_ context.Context, p storage.UpdateWarrantyParams) (storage.Warranty, error) {
			got = p
			return storage.Warranty{ItemID: p.ItemID, Version: 2}, nil
		},
	}
	h, err := NewRouter(warrantyTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPut, warrantyPath, map[string]any{"holder": "Bob", "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got.Holder != "Bob" {
		t.Errorf("Holder = %q, want %q", got.Holder, "Bob")
	}
	if got.Provider != "" || got.Notes != "" || got.StartsOn != "" || got.ExpiresOn != "" || got.IsLifetime {
		t.Errorf("omitted fields arrived as %+v, want every one of them zero -- PUT is a whole-block replace", got)
	}
	if got.ExpectedVersion != 1 {
		t.Errorf("ExpectedVersion = %d, want 1", got.ExpectedVersion)
	}
}

func TestWarrantyWriteHandlersRejectMalformedJSON(t *testing.T) {
	repo := fakeWarrantyRepository{
		createFn: func(context.Context, storage.CreateWarrantyParams) (storage.Warranty, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Warranty{}, nil
		},
		updateFn: func(context.Context, storage.UpdateWarrantyParams) (storage.Warranty, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Warranty{}, nil
		},
	}
	h, err := NewRouter(warrantyTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		rec := doRawBody(t, h, method, warrantyPath, []byte("{not json"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s with a malformed body: status = %d, want 400: %s", method, rec.Code, rec.Body.String())
		}
	}
}

var _ = []error{items.ErrVersionRequired, items.ErrWarrantyDateInvalid}
