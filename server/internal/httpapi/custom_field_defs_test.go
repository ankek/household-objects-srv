package httpapi

import (
	"context"
	"database/sql"
	"github.com/ankek/Household-Objects-Dev/server/internal/customfields"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeCustomFieldDefRepository struct {
	getFn    func(ctx context.Context, id string) (storage.CustomFieldDef, error)
	listFn   func(ctx context.Context) ([]storage.CustomFieldDef, error)
	createFn func(ctx context.Context, p storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error)
	updateFn func(ctx context.Context, p storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error)
	deleteFn func(ctx context.Context, id string, now int64) error
}

func (f fakeCustomFieldDefRepository) Get(ctx context.Context, id string) (storage.CustomFieldDef, error) {
	return f.getFn(ctx, id)
}

func (f fakeCustomFieldDefRepository) List(ctx context.Context) ([]storage.CustomFieldDef, error) {
	return f.listFn(ctx)
}

func (f fakeCustomFieldDefRepository) Create(ctx context.Context, p storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
	return f.createFn(ctx, p)
}

func (f fakeCustomFieldDefRepository) Update(ctx context.Context, p storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
	return f.updateFn(ctx, p)
}

func (f fakeCustomFieldDefRepository) Delete(ctx context.Context, id string, now int64) error {
	return f.deleteFn(ctx, id, now)
}

type customFieldDefFakeScope struct {
	group storage.GroupID
	repo  storage.CustomFieldDefRepository
}

func (s customFieldDefFakeScope) GroupID() storage.GroupID                      { return s.group }
func (s customFieldDefFakeScope) Items() storage.ItemRepository                 { return nil }
func (s customFieldDefFakeScope) Warranty() storage.WarrantyRepository          { return nil }
func (s customFieldDefFakeScope) Sale() storage.SaleRepository                  { return nil }
func (s customFieldDefFakeScope) Purchase() storage.PurchaseRepository          { return nil }
func (s customFieldDefFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }
func (s customFieldDefFakeScope) Identifications() storage.IdentificationRepository {
	return nil
}

func (s customFieldDefFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return s.repo }

func (s customFieldDefFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }

func (s customFieldDefFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s customFieldDefFakeScope) Locations() storage.LocationRepository               { return nil }

func (s customFieldDefFakeScope) Labels() storage.LabelRepository         { return nil }
func (s customFieldDefFakeScope) ItemLabels() storage.ItemLabelRepository { return nil }

func (s customFieldDefFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s customFieldDefFakeScope) Members() storage.MemberRepository { return nil }

type customFieldDefFakeScopes struct {
	repo storage.CustomFieldDefRepository
}

func (s customFieldDefFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return customFieldDefFakeScope{group: g, repo: s.repo}, nil
}

func customFieldDefTestConfig(repo storage.CustomFieldDefRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = customFieldDefFakeScopes{repo: repo}
	return cfg
}

const (
	customFieldDefCollectionPath = "/api/v1/custom-field-defs"
	customFieldDefItemPath       = "/api/v1/custom-field-defs/cfd-1"
)

func TestCustomFieldDefRoutesAreMounted(t *testing.T) {
	seen := map[string]string{}
	repo := fakeCustomFieldDefRepository{
		listFn: func(context.Context) ([]storage.CustomFieldDef, error) {
			seen["GET-list"] = ""
			return []storage.CustomFieldDef{}, nil
		},
		createFn: func(_ context.Context, p storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			seen["POST"] = ""
			return storage.CustomFieldDef{ID: "cfd-1", Name: p.Name, FieldType: p.FieldType, Version: 1}, nil
		},
		updateFn: func(_ context.Context, p storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			seen["PUT"] = p.ID
			return storage.CustomFieldDef{ID: p.ID, Name: p.Name, FieldType: p.FieldType, Version: 2}, nil
		},
		deleteFn: func(_ context.Context, id string, _ int64) error {
			seen["DELETE"] = id
			return nil
		},
	}
	h, err := NewRouter(customFieldDefTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   any
		want   int
	}{
		{http.MethodGet, customFieldDefCollectionPath, nil, http.StatusOK},
		{http.MethodPost, customFieldDefCollectionPath, map[string]any{"name": "Warranty length", "field_type": "text"}, http.StatusCreated},
		{http.MethodPut, customFieldDefItemPath, map[string]any{"name": "New name", "field_type": "number", "version": 1}, http.StatusOK},
		{http.MethodDelete, customFieldDefItemPath, nil, http.StatusNoContent},
	} {
		rec := doJSON(t, h, tc.method, tc.path, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: status = %d, want %d: %s", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
		}
	}
	if _, ok := seen["GET-list"]; !ok {
		t.Error("GET did not reach its handler")
	}
	if _, ok := seen["POST"]; !ok {
		t.Error("POST did not reach its handler")
	}
	if seen["PUT"] != "cfd-1" {
		t.Errorf("PUT reached its handler with id %q, want %q -- the {fieldDefID} wildcard is not being resolved", seen["PUT"], "cfd-1")
	}
	if seen["DELETE"] != "cfd-1" {
		t.Errorf("DELETE reached its handler with id %q, want %q", seen["DELETE"], "cfd-1")
	}
}

func TestCustomFieldDefListRendersAnEmptyArrayNotNull(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		listFn: func(context.Context) ([]storage.CustomFieldDef, error) { return nil, nil },
	}
	h, err := NewRouter(customFieldDefTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodGet, customFieldDefCollectionPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"custom_field_defs":[]}`+"\n" && got != `{"custom_field_defs":[]}` {
		t.Errorf("body = %q, want an empty array, never null", got)
	}
}

func TestCustomFieldDefCreateRejectsAnInvalidFieldTypeAsBadRequest(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		createFn: func(context.Context, storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called despite an invalid field_type")
			return storage.CustomFieldDef{}, nil
		},
	}
	h, err := NewRouter(customFieldDefTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, customFieldDefCollectionPath, map[string]any{"name": "n", "field_type": "not-a-type"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestCustomFieldDefHandlersTranslateStorageErrors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		body       any
		repo       fakeCustomFieldDefRepository
		wantStatus int
	}{
		{
			name: "list: an unexpected fault is 500", method: http.MethodGet, path: customFieldDefCollectionPath,
			repo: fakeCustomFieldDefRepository{listFn: func(context.Context) ([]storage.CustomFieldDef, error) {
				return nil, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "create: a blank name is 400", method: http.MethodPost, path: customFieldDefCollectionPath,
			body: map[string]any{"name": "", "field_type": "text"},
			repo: fakeCustomFieldDefRepository{createFn: func(context.Context, storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
				t.Error("the repository was called despite a blank name")
				return storage.CustomFieldDef{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "create: an unexpected fault is 500", method: http.MethodPost, path: customFieldDefCollectionPath,
			body: map[string]any{"name": "n", "field_type": "text"},
			repo: fakeCustomFieldDefRepository{createFn: func(context.Context, storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
				return storage.CustomFieldDef{}, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "update: not found is 404", method: http.MethodPut, path: customFieldDefItemPath,
			body: map[string]any{"name": "n", "field_type": "text", "version": 1},
			repo: fakeCustomFieldDefRepository{updateFn: func(context.Context, storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
				return storage.CustomFieldDef{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "update: a stale version is 409", method: http.MethodPut, path: customFieldDefItemPath,
			body: map[string]any{"name": "n", "field_type": "text", "version": 1},
			repo: fakeCustomFieldDefRepository{updateFn: func(context.Context, storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
				return storage.CustomFieldDef{}, storage.ErrVersionMismatch
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "update: a missing version is 400", method: http.MethodPut, path: customFieldDefItemPath,
			body: map[string]any{"name": "n", "field_type": "text"},
			repo: fakeCustomFieldDefRepository{updateFn: func(context.Context, storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
				t.Error("the repository was called despite a missing version")
				return storage.CustomFieldDef{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "delete: not found is 404", method: http.MethodDelete, path: customFieldDefItemPath,
			repo: fakeCustomFieldDefRepository{deleteFn: func(context.Context, string, int64) error {
				return storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete: an unexpected fault is 500", method: http.MethodDelete, path: customFieldDefItemPath,
			repo: fakeCustomFieldDefRepository{deleteFn: func(context.Context, string, int64) error {
				return sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(customFieldDefTestConfig(tc.repo))
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

func TestCustomFieldDefUpdatePassesFieldDefIDThrough(t *testing.T) {
	var got storage.UpdateCustomFieldDefParams
	repo := fakeCustomFieldDefRepository{
		updateFn: func(_ context.Context, p storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			got = p
			return storage.CustomFieldDef{ID: p.ID, Name: p.Name, FieldType: p.FieldType, Version: 2}, nil
		},
	}
	h, err := NewRouter(customFieldDefTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, customFieldDefItemPath, map[string]any{"name": "n", "field_type": "number", "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got.ID != "cfd-1" {
		t.Errorf("got ID %q, want %q", got.ID, "cfd-1")
	}
}

func TestCustomFieldDefWriteHandlersRejectMalformedJSON(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		createFn: func(context.Context, storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.CustomFieldDef{}, nil
		},
		updateFn: func(context.Context, storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.CustomFieldDef{}, nil
		},
	}
	h, err := NewRouter(customFieldDefTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, customFieldDefCollectionPath},
		{http.MethodPut, customFieldDefItemPath},
	} {
		rec := doRawBody(t, h, tc.method, tc.path, []byte("{not json"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s with a malformed body: status = %d, want 400: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

var _ = []error{customfields.ErrVersionRequired, customfields.ErrNameRequired, customfields.ErrFieldTypeInvalid}

func TestCustomFieldDefListIsReadableByAMember(t *testing.T) {
	repo := fakeCustomFieldDefRepository{
		listFn: func(context.Context) ([]storage.CustomFieldDef, error) {
			return []storage.CustomFieldDef{{ID: "cfd-1", Name: "Warranty length", FieldType: "text", Version: 1}}, nil
		},
	}
	cfg := customFieldDefTestConfig(repo)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, customFieldDefCollectionPath, nil), "member")
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("GET %s as a member: status = 403, want 200 -- A101.1 makes the READ side "+
			"member-readable (FR-017 definitions drive every item screen's widgets); a 403 here "+
			"means the route was mounted inside the RequireOwner sub-group with the three writes",
			customFieldDefCollectionPath)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as a member: status = %d, want %d; body = %s",
			customFieldDefCollectionPath, rec.Code, http.StatusOK, rec.Body.String())
	}
}
