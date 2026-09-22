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
	"strings"
	"testing"
)

type fakeItemCustomFieldRepository struct {
	getFn    func(ctx context.Context, itemID, id string) (storage.ItemCustomField, error)
	listFn   func(ctx context.Context, itemID string) ([]storage.ItemCustomField, error)
	createFn func(ctx context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error)
	updateFn func(ctx context.Context, p storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error)
	deleteFn func(ctx context.Context, itemID, id string, now int64) error
}

func (f fakeItemCustomFieldRepository) Get(ctx context.Context, itemID, id string) (storage.ItemCustomField, error) {
	return f.getFn(ctx, itemID, id)
}

func (f fakeItemCustomFieldRepository) List(ctx context.Context, itemID string) ([]storage.ItemCustomField, error) {
	return f.listFn(ctx, itemID)
}

func (f fakeItemCustomFieldRepository) Create(ctx context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
	return f.createFn(ctx, p)
}

func (f fakeItemCustomFieldRepository) Update(ctx context.Context, p storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
	return f.updateFn(ctx, p)
}

func (f fakeItemCustomFieldRepository) Delete(ctx context.Context, itemID, id string, now int64) error {
	return f.deleteFn(ctx, itemID, id, now)
}

type fakeCustomFieldDefRepositoryForItemCustomFields struct {
	getFn func(ctx context.Context, id string) (storage.CustomFieldDef, error)
}

func (f fakeCustomFieldDefRepositoryForItemCustomFields) Get(ctx context.Context, id string) (storage.CustomFieldDef, error) {
	return f.getFn(ctx, id)
}
func (f fakeCustomFieldDefRepositoryForItemCustomFields) List(context.Context) ([]storage.CustomFieldDef, error) {
	return nil, nil
}
func (f fakeCustomFieldDefRepositoryForItemCustomFields) Create(context.Context, storage.CreateCustomFieldDefParams) (storage.CustomFieldDef, error) {
	return storage.CustomFieldDef{}, nil
}
func (f fakeCustomFieldDefRepositoryForItemCustomFields) Update(context.Context, storage.UpdateCustomFieldDefParams) (storage.CustomFieldDef, error) {
	return storage.CustomFieldDef{}, nil
}
func (f fakeCustomFieldDefRepositoryForItemCustomFields) Delete(context.Context, string, int64) error {
	return nil
}

type itemCustomFieldFakeScope struct {
	group   storage.GroupID
	repo    storage.ItemCustomFieldRepository
	defRepo storage.CustomFieldDefRepository
}

func (s itemCustomFieldFakeScope) GroupID() storage.GroupID                      { return s.group }
func (s itemCustomFieldFakeScope) Items() storage.ItemRepository                 { return nil }
func (s itemCustomFieldFakeScope) Warranty() storage.WarrantyRepository          { return nil }
func (s itemCustomFieldFakeScope) Sale() storage.SaleRepository                  { return nil }
func (s itemCustomFieldFakeScope) Purchase() storage.PurchaseRepository          { return nil }
func (s itemCustomFieldFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }
func (s itemCustomFieldFakeScope) Identifications() storage.IdentificationRepository {
	return nil
}
func (s itemCustomFieldFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository {
	return s.defRepo
}
func (s itemCustomFieldFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository {
	return s.repo
}

func (s itemCustomFieldFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s itemCustomFieldFakeScope) Locations() storage.LocationRepository               { return nil }

func (s itemCustomFieldFakeScope) Labels() storage.LabelRepository           { return nil }
func (s itemCustomFieldFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s itemCustomFieldFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s itemCustomFieldFakeScope) Members() storage.MemberRepository { return nil }

type itemCustomFieldFakeScopes struct {
	repo    storage.ItemCustomFieldRepository
	defRepo storage.CustomFieldDefRepository
}

func (s itemCustomFieldFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return itemCustomFieldFakeScope{group: g, repo: s.repo, defRepo: s.defRepo}, nil
}

func itemCustomFieldTestConfig(repo storage.ItemCustomFieldRepository, defRepo storage.CustomFieldDefRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = itemCustomFieldFakeScopes{repo: repo, defRepo: defRepo}
	return cfg
}

const (
	itemCustomFieldCollectionPath = "/api/v1/items/itm-1/custom-fields"
	itemCustomFieldItemPath       = "/api/v1/items/itm-1/custom-fields/cf-1"
)

func TestItemCustomFieldRoutesAreMounted(t *testing.T) {
	seen := map[string][2]string{}
	repo := fakeItemCustomFieldRepository{
		listFn: func(_ context.Context, itemID string) ([]storage.ItemCustomField, error) {
			seen["GET-list"] = [2]string{itemID, ""}
			return []storage.ItemCustomField{}, nil
		},
		createFn: func(_ context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			seen["POST"] = [2]string{p.ItemID, ""}
			return storage.ItemCustomField{ItemID: p.ItemID, ID: "cf-1", Name: p.Name, FieldType: p.FieldType, Version: 1}, nil
		},
		updateFn: func(_ context.Context, p storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
			seen["PUT"] = [2]string{p.ItemID, p.ID}
			return storage.ItemCustomField{ItemID: p.ItemID, ID: p.ID, Name: p.Name, FieldType: p.FieldType, Version: 2}, nil
		},
		deleteFn: func(_ context.Context, itemID, id string, _ int64) error {
			seen["DELETE"] = [2]string{itemID, id}
			return nil
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   any
		want   int
	}{
		{http.MethodGet, itemCustomFieldCollectionPath, nil, http.StatusOK},
		{http.MethodPost, itemCustomFieldCollectionPath, map[string]any{"name": "Colour", "field_type": "text", "text_value": "blue"}, http.StatusCreated},
		{http.MethodPut, itemCustomFieldItemPath, map[string]any{"name": "Colour", "field_type": "text", "text_value": "red", "version": 1}, http.StatusOK},
		{http.MethodDelete, itemCustomFieldItemPath, nil, http.StatusNoContent},
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
	if seen["PUT"] != [2]string{"itm-1", "cf-1"} {
		t.Errorf("PUT reached its handler with {itemID:%q id:%q}, want {\"itm-1\" \"cf-1\"} -- the {customFieldID} wildcard is not being resolved", seen["PUT"][0], seen["PUT"][1])
	}
	if seen["DELETE"] != [2]string{"itm-1", "cf-1"} {
		t.Errorf("DELETE reached its handler with {itemID:%q id:%q}, want {\"itm-1\" \"cf-1\"}", seen["DELETE"][0], seen["DELETE"][1])
	}
}

func TestItemCustomFieldListRendersAnEmptyArrayNotNull(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		listFn: func(context.Context, string) ([]storage.ItemCustomField, error) { return nil, nil },
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodGet, itemCustomFieldCollectionPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"custom_fields":[]}`+"\n" && got != `{"custom_fields":[]}` {
		t.Errorf("body = %q, want an empty array, never null", got)
	}
}

func TestItemCustomFieldCreateRejectsZeroValueColumnsAsBadRequest(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite zero value columns populated")
			return storage.ItemCustomField{}, nil
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, itemCustomFieldCollectionPath, map[string]any{"name": "Colour", "field_type": "text"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestItemCustomFieldCreateRejectsMultipleValueColumnsAsBadRequest(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite two value columns populated")
			return storage.ItemCustomField{}, nil
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, itemCustomFieldCollectionPath, map[string]any{
		"name": "Colour", "field_type": "text", "text_value": "blue", "number_value": 5,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestItemCustomFieldCreateRejectsAMismatchedValueColumnAsBadRequest(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite a mismatched value column")
			return storage.ItemCustomField{}, nil
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, itemCustomFieldCollectionPath, map[string]any{
		"name": "Weight", "field_type": "number", "text_value": "not a number",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestItemCustomFieldCreateWithFieldDefIDReadsTheDefinitionAndRejectsAMismatch(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite a field_type contradicting the definition")
			return storage.ItemCustomField{}, nil
		},
	}
	defRepo := fakeCustomFieldDefRepositoryForItemCustomFields{
		getFn: func(_ context.Context, id string) (storage.CustomFieldDef, error) {
			return storage.CustomFieldDef{ID: id, Name: "Purchase date", FieldType: "date", Version: 1}, nil
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, defRepo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, itemCustomFieldCollectionPath, map[string]any{
		"field_def_id": "cfd-1", "name": "Purchase date", "field_type": "number", "number_value": 42,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestItemCustomFieldCreateWithAnUnknownFieldDefIDIsBadRequest(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called despite an unresolvable field_def_id")
			return storage.ItemCustomField{}, nil
		},
	}
	defRepo := fakeCustomFieldDefRepositoryForItemCustomFields{
		getFn: func(context.Context, string) (storage.CustomFieldDef, error) {
			return storage.CustomFieldDef{}, storage.ErrNotFound
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, defRepo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, itemCustomFieldCollectionPath, map[string]any{
		"field_def_id": "cfd-gone", "name": "Colour", "field_type": "text", "text_value": "blue",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (never 404 -- field_def_id is a body field, not the path's resource id): %s", rec.Code, rec.Body.String())
	}
}

func TestItemCustomFieldHandlersTranslateStorageErrors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		body       any
		repo       fakeItemCustomFieldRepository
		wantStatus int
	}{
		{
			name: "list: not found is 404", method: http.MethodGet, path: itemCustomFieldCollectionPath,
			repo: fakeItemCustomFieldRepository{listFn: func(context.Context, string) ([]storage.ItemCustomField, error) {
				return nil, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "list: an unexpected fault is 500", method: http.MethodGet, path: itemCustomFieldCollectionPath,
			repo: fakeItemCustomFieldRepository{listFn: func(context.Context, string) ([]storage.ItemCustomField, error) {
				return nil, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "create: an unknown or foreign item is 404", method: http.MethodPost, path: itemCustomFieldCollectionPath,
			body: map[string]any{"name": "Colour", "field_type": "text", "text_value": "blue"},
			repo: fakeItemCustomFieldRepository{createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
				return storage.ItemCustomField{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "create: a blank name is 400", method: http.MethodPost, path: itemCustomFieldCollectionPath,
			body: map[string]any{"name": "", "field_type": "text", "text_value": "blue"},
			repo: fakeItemCustomFieldRepository{createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
				t.Error("the repository was called despite a blank name")
				return storage.ItemCustomField{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "create: an invalid field_type is 400", method: http.MethodPost, path: itemCustomFieldCollectionPath,
			body: map[string]any{"name": "n", "field_type": "not-a-type", "text_value": "v"},
			repo: fakeItemCustomFieldRepository{createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
				t.Error("the repository was called despite an invalid field_type")
				return storage.ItemCustomField{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: not found is 404", method: http.MethodPut, path: itemCustomFieldItemPath,
			body: map[string]any{"name": "Colour", "field_type": "text", "text_value": "blue", "version": 1},
			repo: fakeItemCustomFieldRepository{updateFn: func(context.Context, storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
				return storage.ItemCustomField{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "update: a stale version is 409", method: http.MethodPut, path: itemCustomFieldItemPath,
			body: map[string]any{"name": "Colour", "field_type": "text", "text_value": "blue", "version": 1},
			repo: fakeItemCustomFieldRepository{updateFn: func(context.Context, storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
				return storage.ItemCustomField{}, storage.ErrVersionMismatch
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "update: a missing version is 400", method: http.MethodPut, path: itemCustomFieldItemPath,
			body: map[string]any{"name": "Colour", "field_type": "text", "text_value": "blue"},
			repo: fakeItemCustomFieldRepository{updateFn: func(context.Context, storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
				t.Error("the repository was called despite a missing version")
				return storage.ItemCustomField{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "delete: not found is 404", method: http.MethodDelete, path: itemCustomFieldItemPath,
			repo: fakeItemCustomFieldRepository{deleteFn: func(context.Context, string, string, int64) error {
				return storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete: an unexpected fault is 500", method: http.MethodDelete, path: itemCustomFieldItemPath,
			repo: fakeItemCustomFieldRepository{deleteFn: func(context.Context, string, string, int64) error {
				return sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(itemCustomFieldTestConfig(tc.repo, nil))
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

func TestItemCustomFieldUpdatePassesBothIDsThrough(t *testing.T) {
	var got storage.UpdateItemCustomFieldParams
	repo := fakeItemCustomFieldRepository{
		updateFn: func(_ context.Context, p storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
			got = p
			return storage.ItemCustomField{ItemID: p.ItemID, ID: p.ID, Name: p.Name, FieldType: p.FieldType, Version: 2}, nil
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, itemCustomFieldItemPath, map[string]any{"name": "Colour", "field_type": "text", "text_value": "red", "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got.ItemID != "itm-1" || got.ID != "cf-1" {
		t.Errorf("got {item_id:%q id:%q}, want {\"itm-1\" \"cf-1\"}", got.ItemID, got.ID)
	}
}

func TestItemCustomFieldWriteHandlersRejectMalformedJSON(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(context.Context, storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.ItemCustomField{}, nil
		},
		updateFn: func(context.Context, storage.UpdateItemCustomFieldParams) (storage.ItemCustomField, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.ItemCustomField{}, nil
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, itemCustomFieldCollectionPath},
		{http.MethodPut, itemCustomFieldItemPath},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{not valid json"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s %s with a malformed body: status = %d, want 400: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestItemCustomFieldWireRoundTripsBoolAndNumberZeroValues(t *testing.T) {
	falseVal := false
	zero := 0.0
	repo := fakeItemCustomFieldRepository{
		createFn: func(_ context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			return storage.ItemCustomField{
				ItemID: p.ItemID, ID: "cf-1", Name: p.Name, FieldType: p.FieldType,
				BoolValue: sql.NullInt64{Int64: 0, Valid: p.BoolValue != nil},
				Version:   1,
			}, nil
		},
	}
	h, err := NewRouter(itemCustomFieldTestConfig(repo, nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, itemCustomFieldCollectionPath, map[string]any{
		"name": "Broken", "field_type": "boolean", "bool_value": falseVal,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var body itemCustomFieldBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.BoolValue == nil {
		t.Fatal("bool_value round-tripped as absent, want a PRESENT false")
	}
	if *body.BoolValue != false {
		t.Errorf("bool_value = %v, want false", *body.BoolValue)
	}
	_ = zero
}

func TestItemCustomFieldCreateIsWritableByAMember(t *testing.T) {
	repo := fakeItemCustomFieldRepository{
		createFn: func(_ context.Context, p storage.CreateItemCustomFieldParams) (storage.ItemCustomField, error) {
			return storage.ItemCustomField{ItemID: p.ItemID, ID: "cf-1", Name: p.Name, FieldType: p.FieldType, Version: 1}, nil
		},
	}
	cfg := itemCustomFieldTestConfig(repo, nil)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{
		"name": "Colour", "field_type": "text", "text_value": "blue",
	}); err != nil {
		t.Fatalf("encode request body: %v", err)
	}
	req := asTestUser(httptest.NewRequest(http.MethodPost, itemCustomFieldCollectionPath, &buf), "member")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("POST %s as a member: status = 403, want 201 -- item custom-field values are member-writable "+
			"item data (the SAME posture identifications has), not an owner-gated group setting the way "+
			"/custom-field-defs' DEFINITIONS are: a 403 here means this route was mounted inside "+
			"middleware.RequireOwner by mistake", itemCustomFieldCollectionPath)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s as a member: status = %d, want 201; body = %s", itemCustomFieldCollectionPath, rec.Code, rec.Body.String())
	}
}

var _ = []error{
	items.ErrCustomFieldNameRequired,
	items.ErrCustomFieldTypeInvalid,
	items.ErrCustomFieldValueInvalid,
	items.ErrCustomFieldDefNotFound,
	items.ErrCustomFieldTypeMismatch,
}
