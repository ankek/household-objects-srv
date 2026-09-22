package httpapi

import (
	"context"
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/items"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"testing"
)

type fakeIdentificationRepository struct {
	getFn    func(ctx context.Context, itemID, id string) (storage.Identification, error)
	listFn   func(ctx context.Context, itemID string) ([]storage.Identification, error)
	createFn func(ctx context.Context, p storage.CreateIdentificationParams) (storage.Identification, error)
	updateFn func(ctx context.Context, p storage.UpdateIdentificationParams) (storage.Identification, error)
	deleteFn func(ctx context.Context, itemID, id string, now int64) error
}

func (f fakeIdentificationRepository) Get(ctx context.Context, itemID, id string) (storage.Identification, error) {
	return f.getFn(ctx, itemID, id)
}

func (f fakeIdentificationRepository) List(ctx context.Context, itemID string) ([]storage.Identification, error) {
	return f.listFn(ctx, itemID)
}

func (f fakeIdentificationRepository) Create(ctx context.Context, p storage.CreateIdentificationParams) (storage.Identification, error) {
	return f.createFn(ctx, p)
}

func (f fakeIdentificationRepository) Update(ctx context.Context, p storage.UpdateIdentificationParams) (storage.Identification, error) {
	return f.updateFn(ctx, p)
}

func (f fakeIdentificationRepository) Delete(ctx context.Context, itemID, id string, now int64) error {
	return f.deleteFn(ctx, itemID, id, now)
}

type identificationFakeScope struct {
	group storage.GroupID
	repo  storage.IdentificationRepository
}

func (s identificationFakeScope) GroupID() storage.GroupID             { return s.group }
func (s identificationFakeScope) Items() storage.ItemRepository        { return nil }
func (s identificationFakeScope) Warranty() storage.WarrantyRepository { return nil }
func (s identificationFakeScope) Sale() storage.SaleRepository         { return nil }
func (s identificationFakeScope) Purchase() storage.PurchaseRepository { return nil }

func (s identificationFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }

func (s identificationFakeScope) Identifications() storage.IdentificationRepository { return s.repo }

func (s identificationFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }

func (s identificationFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }

func (s identificationFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s identificationFakeScope) Locations() storage.LocationRepository               { return nil }

func (s identificationFakeScope) Labels() storage.LabelRepository           { return nil }
func (s identificationFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s identificationFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s identificationFakeScope) Members() storage.MemberRepository { return nil }

type identificationFakeScopes struct {
	repo storage.IdentificationRepository
}

func (s identificationFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return identificationFakeScope{group: g, repo: s.repo}, nil
}

func identificationTestConfig(repo storage.IdentificationRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = identificationFakeScopes{repo: repo}
	return cfg
}

const (
	identificationCollectionPath = "/api/v1/items/itm-1/identifications"
	identificationItemPath       = "/api/v1/items/itm-1/identifications/id-1"
)

func TestIdentificationRoutesAreMounted(t *testing.T) {
	seen := map[string][2]string{}
	repo := fakeIdentificationRepository{
		listFn: func(_ context.Context, itemID string) ([]storage.Identification, error) {
			seen["GET-list"] = [2]string{itemID, ""}
			return []storage.Identification{}, nil
		},
		createFn: func(_ context.Context, p storage.CreateIdentificationParams) (storage.Identification, error) {
			seen["POST"] = [2]string{p.ItemID, ""}
			return storage.Identification{ItemID: p.ItemID, ID: "id-1", Kind: p.Kind, Value: p.Value, Version: 1}, nil
		},
		updateFn: func(_ context.Context, p storage.UpdateIdentificationParams) (storage.Identification, error) {
			seen["PUT"] = [2]string{p.ItemID, p.ID}
			return storage.Identification{ItemID: p.ItemID, ID: p.ID, Kind: p.Kind, Value: p.Value, Version: 2}, nil
		},
		deleteFn: func(_ context.Context, itemID, id string, _ int64) error {
			seen["DELETE"] = [2]string{itemID, id}
			return nil
		},
	}
	h, err := NewRouter(identificationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   any
		want   int
	}{
		{http.MethodGet, identificationCollectionPath, nil, http.StatusOK},
		{http.MethodPost, identificationCollectionPath, map[string]any{"kind": "serial", "value": "SN-1"}, http.StatusCreated},
		{http.MethodPut, identificationItemPath, map[string]any{"kind": "serial", "value": "SN-2", "version": 1}, http.StatusOK},
		{http.MethodDelete, identificationItemPath, nil, http.StatusNoContent},
	} {
		rec := doJSON(t, h, tc.method, tc.path, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: status = %d, want %d: %s", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
		}
	}
	if seen["GET-list"][0] != "itm-1" {
		t.Errorf("GET reached its handler with itemID %q, want %q -- the {itemID} wildcard is not being resolved", seen["GET-list"][0], "itm-1")
	}
	if seen["POST"][0] != "itm-1" {
		t.Errorf("POST reached its handler with itemID %q, want %q", seen["POST"][0], "itm-1")
	}
	if seen["PUT"] != [2]string{"itm-1", "id-1"} {
		t.Errorf("PUT reached its handler with {itemID:%q id:%q}, want {\"itm-1\" \"id-1\"} -- the {identificationID} wildcard is not being resolved", seen["PUT"][0], seen["PUT"][1])
	}
	if seen["DELETE"] != [2]string{"itm-1", "id-1"} {
		t.Errorf("DELETE reached its handler with {itemID:%q id:%q}, want {\"itm-1\" \"id-1\"}", seen["DELETE"][0], seen["DELETE"][1])
	}
}

func TestIdentificationListRendersAnEmptyArrayNotNull(t *testing.T) {
	repo := fakeIdentificationRepository{
		listFn: func(context.Context, string) ([]storage.Identification, error) { return nil, nil },
	}
	h, err := NewRouter(identificationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodGet, identificationCollectionPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"identifications":[]}`+"\n" && got != `{"identifications":[]}` {
		t.Errorf("body = %q, want an empty array, never null", got)
	}
}

func TestIdentificationCreateRejectsAnInvalidKindAsBadRequest(t *testing.T) {
	repo := fakeIdentificationRepository{
		createFn: func(context.Context, storage.CreateIdentificationParams) (storage.Identification, error) {
			t.Error("the repository was called despite an invalid kind")
			return storage.Identification{}, nil
		},
	}
	h, err := NewRouter(identificationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, identificationCollectionPath, map[string]any{"kind": "not-a-kind", "value": "v"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestIdentificationHandlersTranslateStorageErrors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		body       any
		repo       fakeIdentificationRepository
		wantStatus int
	}{
		{
			name: "list: not found is 404", method: http.MethodGet, path: identificationCollectionPath,
			repo: fakeIdentificationRepository{listFn: func(context.Context, string) ([]storage.Identification, error) {
				return nil, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "list: an unexpected fault is 500", method: http.MethodGet, path: identificationCollectionPath,
			repo: fakeIdentificationRepository{listFn: func(context.Context, string) ([]storage.Identification, error) {
				return nil, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "create: an unknown or foreign item is 404", method: http.MethodPost, path: identificationCollectionPath,
			body: map[string]any{"kind": "serial", "value": "v"},
			repo: fakeIdentificationRepository{createFn: func(context.Context, storage.CreateIdentificationParams) (storage.Identification, error) {
				return storage.Identification{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "create: a blank value is 400", method: http.MethodPost, path: identificationCollectionPath,
			body: map[string]any{"kind": "serial", "value": ""},
			repo: fakeIdentificationRepository{createFn: func(context.Context, storage.CreateIdentificationParams) (storage.Identification, error) {
				t.Error("the repository was called despite a blank value")
				return storage.Identification{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: not found is 404", method: http.MethodPut, path: identificationItemPath,
			body: map[string]any{"kind": "serial", "value": "v", "version": 1},
			repo: fakeIdentificationRepository{updateFn: func(context.Context, storage.UpdateIdentificationParams) (storage.Identification, error) {
				return storage.Identification{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "update: a stale version is 409", method: http.MethodPut, path: identificationItemPath,
			body: map[string]any{"kind": "serial", "value": "v", "version": 1},
			repo: fakeIdentificationRepository{updateFn: func(context.Context, storage.UpdateIdentificationParams) (storage.Identification, error) {
				return storage.Identification{}, storage.ErrVersionMismatch
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "update: a missing version is 400", method: http.MethodPut, path: identificationItemPath,
			body: map[string]any{"kind": "serial", "value": "v"},
			repo: fakeIdentificationRepository{updateFn: func(context.Context, storage.UpdateIdentificationParams) (storage.Identification, error) {
				t.Error("the repository was called despite a missing version")
				return storage.Identification{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "delete: not found is 404", method: http.MethodDelete, path: identificationItemPath,
			repo: fakeIdentificationRepository{deleteFn: func(context.Context, string, string, int64) error {
				return storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete: an unexpected fault is 500", method: http.MethodDelete, path: identificationItemPath,
			repo: fakeIdentificationRepository{deleteFn: func(context.Context, string, string, int64) error {
				return sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(identificationTestConfig(tc.repo))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			rec := doJSON(t, h, tc.method, tc.path, tc.body)
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

func TestIdentificationUpdatePassesBothIDsThrough(t *testing.T) {
	var got storage.UpdateIdentificationParams
	repo := fakeIdentificationRepository{
		updateFn: func(_ context.Context, p storage.UpdateIdentificationParams) (storage.Identification, error) {
			got = p
			return storage.Identification{ItemID: p.ItemID, ID: p.ID, Kind: p.Kind, Value: p.Value, Version: 2}, nil
		},
	}
	h, err := NewRouter(identificationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, identificationItemPath, map[string]any{"kind": "model", "value": "M-1", "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got.ItemID != "itm-1" || got.ID != "id-1" {
		t.Errorf("got {item_id:%q id:%q}, want {\"itm-1\" \"id-1\"}", got.ItemID, got.ID)
	}
}

func TestIdentificationWriteHandlersRejectMalformedJSON(t *testing.T) {
	repo := fakeIdentificationRepository{
		createFn: func(context.Context, storage.CreateIdentificationParams) (storage.Identification, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Identification{}, nil
		},
		updateFn: func(context.Context, storage.UpdateIdentificationParams) (storage.Identification, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Identification{}, nil
		},
	}
	h, err := NewRouter(identificationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, identificationCollectionPath},
		{http.MethodPut, identificationItemPath},
	} {
		rec := doRawBody(t, h, tc.method, tc.path, []byte("{not json"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s with a malformed body: status = %d, want 400: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

var _ = []error{items.ErrVersionRequired, items.ErrIdentificationKindInvalid, items.ErrIdentificationValueRequired}
