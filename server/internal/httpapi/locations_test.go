package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeLocationRepository struct {
	getFn         func(ctx context.Context, id string) (storage.Location, error)
	listFn        func(ctx context.Context) ([]storage.Location, error)
	createFn      func(ctx context.Context, p storage.CreateLocationParams) (storage.Location, error)
	updateFn      func(ctx context.Context, p storage.UpdateLocationParams) (storage.Location, error)
	deleteFn      func(ctx context.Context, p storage.DeleteLocationParams) error
	descendantsFn func(ctx context.Context, locationID string) ([]string, error)
	treeFn        func(ctx context.Context) ([]*storage.LocationNode, error)
}

func (f fakeLocationRepository) Get(ctx context.Context, id string) (storage.Location, error) {
	return f.getFn(ctx, id)
}
func (f fakeLocationRepository) List(ctx context.Context) ([]storage.Location, error) {
	return f.listFn(ctx)
}
func (f fakeLocationRepository) Create(ctx context.Context, p storage.CreateLocationParams) (storage.Location, error) {
	return f.createFn(ctx, p)
}
func (f fakeLocationRepository) Update(ctx context.Context, p storage.UpdateLocationParams) (storage.Location, error) {
	return f.updateFn(ctx, p)
}
func (f fakeLocationRepository) Delete(ctx context.Context, p storage.DeleteLocationParams) error {
	return f.deleteFn(ctx, p)
}

func (f fakeLocationRepository) Descendants(ctx context.Context, locationID string) ([]string, error) {
	if f.descendantsFn != nil {
		return f.descendantsFn(ctx, locationID)
	}
	return nil, nil
}

func (f fakeLocationRepository) Tree(ctx context.Context) ([]*storage.LocationNode, error) {
	if f.treeFn != nil {
		return f.treeFn(ctx)
	}
	return nil, nil
}

type locationFakeScope struct {
	group storage.GroupID
	repo  storage.LocationRepository
}

func (s locationFakeScope) GroupID() storage.GroupID                          { return s.group }
func (s locationFakeScope) Items() storage.ItemRepository                     { return nil }
func (s locationFakeScope) Warranty() storage.WarrantyRepository              { return nil }
func (s locationFakeScope) Sale() storage.SaleRepository                      { return nil }
func (s locationFakeScope) Purchase() storage.PurchaseRepository              { return nil }
func (s locationFakeScope) Visibility() storage.GroupVisibilityRepository     { return nil }
func (s locationFakeScope) Identifications() storage.IdentificationRepository { return nil }
func (s locationFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }
func (s locationFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository {
	return nil
}
func (s locationFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s locationFakeScope) Locations() storage.LocationRepository               { return s.repo }

func (s locationFakeScope) Labels() storage.LabelRepository           { return nil }
func (s locationFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s locationFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s locationFakeScope) Members() storage.MemberRepository { return nil }

type locationFakeScopes struct {
	repo storage.LocationRepository
}

func (s locationFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return locationFakeScope{group: g, repo: s.repo}, nil
}

func locationTestConfig(repo storage.LocationRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = locationFakeScopes{repo: repo}
	return cfg
}

const (
	locationCollectionPath = "/api/v1/locations"
	locationItemPath       = "/api/v1/locations/loc-1"
)

func TestLocationRoutesAreMounted(t *testing.T) {
	seen := map[string]string{}
	repo := fakeLocationRepository{
		listFn: func(context.Context) ([]storage.Location, error) {
			seen["GET-list"] = ""
			return []storage.Location{}, nil
		},
		createFn: func(_ context.Context, p storage.CreateLocationParams) (storage.Location, error) {
			seen["POST"] = ""
			return storage.Location{ID: p.ID, Name: p.Name, Version: 1}, nil
		},
		getFn: func(_ context.Context, id string) (storage.Location, error) {
			seen["GET-item"] = id
			return storage.Location{ID: id, Name: "n", Version: 1}, nil
		},
		updateFn: func(_ context.Context, p storage.UpdateLocationParams) (storage.Location, error) {
			seen["PUT"] = p.LocationID
			return storage.Location{ID: p.LocationID, Name: p.Name, Version: 2}, nil
		},
		deleteFn: func(_ context.Context, p storage.DeleteLocationParams) error {
			seen["DELETE"] = p.LocationID
			return nil
		},
	}
	h, err := NewRouter(locationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, locationCollectionPath, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200: %s", locationCollectionPath, rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodPost, locationCollectionPath, map[string]any{"name": "Garage"}); rec.Code != http.StatusCreated {
		t.Fatalf("POST %s: status = %d, want 201: %s", locationCollectionPath, rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodGet, locationItemPath, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200: %s", locationItemPath, rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodPut, locationItemPath, map[string]any{"name": "Garage", "version": 1}); rec.Code != http.StatusOK {
		t.Fatalf("PUT %s: status = %d, want 200: %s", locationItemPath, rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodDelete, locationItemPath, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE %s: status = %d, want 204: %s", locationItemPath, rec.Code, rec.Body.String())
	}

	want := map[string]string{"GET-list": "", "POST": "", "GET-item": "loc-1", "PUT": "loc-1", "DELETE": "loc-1"}
	for k, v := range want {
		if seen[k] != v {
			t.Errorf("%s reached repository with id %q, want %q (seen=%+v)", k, seen[k], v, seen)
		}
	}
}

func TestLocationCreateIsWritableByAMember(t *testing.T) {
	repo := fakeLocationRepository{
		createFn: func(_ context.Context, p storage.CreateLocationParams) (storage.Location, error) {
			return storage.Location{ID: "loc-1", Name: p.Name, Version: 1}, nil
		},
	}
	cfg := locationTestConfig(repo)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{"name": "Garage"}); err != nil {
		t.Fatalf("encode request body: %v", err)
	}
	req := asTestUser(httptest.NewRequest(http.MethodPost, locationCollectionPath, &buf), "member")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("POST %s as a member: status = 403, want 201 -- locations are member-writable item "+
			"data (the SAME posture identifications/item-custom-fields/stock-adjustments have): a 403 "+
			"here means this route was mounted inside middleware.RequireOwner by mistake", locationCollectionPath)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s as a member: status = %d, want 201; body = %s", locationCollectionPath, rec.Code, rec.Body.String())
	}
}

func TestLocationHandlersTranslateStorageErrors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		body       any
		repo       fakeLocationRepository
		wantStatus int
	}{
		{
			name: "list: an unexpected fault is 500", method: http.MethodGet, path: locationCollectionPath,
			repo: fakeLocationRepository{listFn: func(context.Context) ([]storage.Location, error) {
				return nil, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "create: a blank name is 400", method: http.MethodPost, path: locationCollectionPath,
			body: map[string]any{"name": ""},
			repo: fakeLocationRepository{createFn: func(context.Context, storage.CreateLocationParams) (storage.Location, error) {
				t.Error("the repository was called despite a blank name")
				return storage.Location{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "create: an unknown parent_id is 400, not 404", method: http.MethodPost, path: locationCollectionPath,
			body: map[string]any{"name": "Garage", "parent_id": "does-not-exist"},
			repo: fakeLocationRepository{createFn: func(context.Context, storage.CreateLocationParams) (storage.Location, error) {
				return storage.Location{}, storage.ErrLocationParentNotFound
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "create: an unexpected fault is 500", method: http.MethodPost, path: locationCollectionPath,
			body: map[string]any{"name": "Garage"},
			repo: fakeLocationRepository{createFn: func(context.Context, storage.CreateLocationParams) (storage.Location, error) {
				return storage.Location{}, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "get: not found is 404", method: http.MethodGet, path: locationItemPath,
			repo: fakeLocationRepository{getFn: func(context.Context, string) (storage.Location, error) {
				return storage.Location{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "get: an unexpected fault is 500", method: http.MethodGet, path: locationItemPath,
			repo: fakeLocationRepository{getFn: func(context.Context, string) (storage.Location, error) {
				return storage.Location{}, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "update: a blank name is 400", method: http.MethodPut, path: locationItemPath,
			body: map[string]any{"name": "", "version": 1},
			repo: fakeLocationRepository{updateFn: func(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
				t.Error("the repository was called despite a blank name")
				return storage.Location{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: a missing version is 400", method: http.MethodPut, path: locationItemPath,
			body: map[string]any{"name": "Garage"},
			repo: fakeLocationRepository{updateFn: func(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
				t.Error("the repository was called despite a missing version")
				return storage.Location{}, nil
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: not found is 404", method: http.MethodPut, path: locationItemPath,
			body: map[string]any{"name": "Garage", "version": 1},
			repo: fakeLocationRepository{updateFn: func(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
				return storage.Location{}, storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "update: a stale version is 409", method: http.MethodPut, path: locationItemPath,
			body: map[string]any{"name": "Garage", "version": 1},
			repo: fakeLocationRepository{updateFn: func(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
				return storage.Location{}, storage.ErrVersionMismatch
			}},
			wantStatus: http.StatusConflict,
		},
		{
			name: "update: an unknown parent_id is 400, not 404", method: http.MethodPut, path: locationItemPath,
			body: map[string]any{"name": "Garage", "parent_id": "does-not-exist", "version": 1},
			repo: fakeLocationRepository{updateFn: func(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
				return storage.Location{}, storage.ErrLocationParentNotFound
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: a cycle is 400, not 409 or 404", method: http.MethodPut, path: locationItemPath,
			body: map[string]any{"name": "Garage", "parent_id": "loc-descendant", "version": 1},
			repo: fakeLocationRepository{updateFn: func(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
				return storage.Location{}, storage.ErrLocationCycle
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "update: an unexpected fault is 500", method: http.MethodPut, path: locationItemPath,
			body: map[string]any{"name": "Garage", "version": 1},
			repo: fakeLocationRepository{updateFn: func(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
				return storage.Location{}, sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "delete: not found is 404", method: http.MethodDelete, path: locationItemPath,
			repo: fakeLocationRepository{deleteFn: func(context.Context, storage.DeleteLocationParams) error {
				return storage.ErrNotFound
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete: an unexpected fault is 500", method: http.MethodDelete, path: locationItemPath,
			repo: fakeLocationRepository{deleteFn: func(context.Context, storage.DeleteLocationParams) error {
				return sql.ErrConnDone
			}},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(locationTestConfig(tc.repo))
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

func TestLocationUpdatePassesLocationIDThrough(t *testing.T) {
	var got storage.UpdateLocationParams
	repo := fakeLocationRepository{
		updateFn: func(_ context.Context, p storage.UpdateLocationParams) (storage.Location, error) {
			got = p
			return storage.Location{ID: p.LocationID, Name: p.Name, Version: 2}, nil
		},
	}
	h, err := NewRouter(locationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, locationItemPath, map[string]any{"name": "Attic", "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got.LocationID != "loc-1" {
		t.Errorf("got LocationID %q, want %q", got.LocationID, "loc-1")
	}
}

func TestLocationWriteHandlersRejectMalformedJSON(t *testing.T) {
	repo := fakeLocationRepository{
		createFn: func(context.Context, storage.CreateLocationParams) (storage.Location, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Location{}, nil
		},
		updateFn: func(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
			t.Error("the repository was called for an undecodable body")
			return storage.Location{}, nil
		},
	}
	h, err := NewRouter(locationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, locationCollectionPath},
		{http.MethodPut, locationItemPath},
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

type deleteGuardRepo struct {
	got *storage.DeleteLocationParams
	err error
}

func (r *deleteGuardRepo) repo() fakeLocationRepository {
	return fakeLocationRepository{
		deleteFn: func(_ context.Context, p storage.DeleteLocationParams) error {
			captured := p
			r.got = &captured
			return r.err
		},
	}
}

func TestLocationDeleteReadsTheThreeStatesOfReassignTo(t *testing.T) {
	for name, tc := range map[string]struct {
		path         string
		wantReassign bool
		wantTarget   string
	}{
		"absent":             {locationItemPath, false, ""},
		"present and empty":  {locationItemPath + "?reassign_to=", true, ""},
		"present with an id": {locationItemPath + "?reassign_to=loc-basement", true, "loc-basement"},
	} {
		t.Run(name, func(t *testing.T) {
			spy := &deleteGuardRepo{}
			h, err := NewRouter(locationTestConfig(spy.repo()))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}

			if rec := doJSON(t, h, http.MethodDelete, tc.path, nil); rec.Code != http.StatusNoContent {
				t.Fatalf("DELETE %s = %d, want 204: %s", tc.path, rec.Code, rec.Body.String())
			}
			if spy.got == nil {
				t.Fatal("the handler never reached the repository")
			}
			if spy.got.Reassign != tc.wantReassign {
				t.Errorf("Reassign = %v, want %v", spy.got.Reassign, tc.wantReassign)
			}
			if spy.got.ReassignTo != tc.wantTarget {
				t.Errorf("ReassignTo = %q, want %q", spy.got.ReassignTo, tc.wantTarget)
			}
		})
	}
}

func TestLocationDeleteGuardAnswers409WithCounts(t *testing.T) {
	spy := &deleteGuardRepo{err: &storage.LocationNotEmptyError{LocationID: "loc-1", ChildCount: 3, ItemCount: 12}}
	h, err := NewRouter(locationTestConfig(spy.repo()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodDelete, locationItemPath, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("DELETE of a non-empty location = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json -- the extra members must not cost this response its problem media type", ct)
	}

	var body struct {
		Type       string `json:"type"`
		Title      string `json:"title"`
		Status     int    `json:"status"`
		Detail     string `json:"detail"`
		ChildCount int64  `json:"child_count"`
		ItemCount  int64  `json:"item_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem body: %v (%s)", err, rec.Body.String())
	}
	if body.Type != "urn:hho:problem:location-not-empty" {
		t.Errorf("type = %q, want urn:hho:problem:location-not-empty -- clients branch on this", body.Type)
	}
	if body.Status != http.StatusConflict {
		t.Errorf("status field = %d, want 409", body.Status)
	}
	if body.ChildCount != 3 || body.ItemCount != 12 {
		t.Errorf("counts = %d children, %d items; want 3 and 12", body.ChildCount, body.ItemCount)
	}
	if body.Detail == "" {
		t.Error("no detail on the 409; a refusal a caller cannot act on is worse than no refusal")
	}
}

func TestLocationDeleteGuardIsNotConfusedWithAVersionConflict(t *testing.T) {
	spy := &deleteGuardRepo{err: storage.ErrVersionMismatch}
	h, err := NewRouter(locationTestConfig(spy.repo()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodDelete, locationItemPath, nil)
	if strings.Contains(rec.Body.String(), "location-not-empty") {
		t.Fatalf("a non-guard error rendered as the delete-guard's 409: %s", rec.Body.String())
	}
}

func TestLocationDeleteTranslatesReassignTargetErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err        error
		wantDetail string
	}{
		"unresolvable target":           {storage.ErrLocationParentNotFound, "does not resolve"},
		"target inside its own subtree": {storage.ErrLocationReassignTarget, "descendants"},
	} {
		t.Run(name, func(t *testing.T) {
			spy := &deleteGuardRepo{err: tc.err}
			h, err := NewRouter(locationTestConfig(spy.repo()))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}

			rec := doJSON(t, h, http.MethodDelete, locationItemPath+"?reassign_to=loc-x", nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("DELETE with %s = %d, want 400: %s", name, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantDetail) {
				t.Errorf("400 detail does not mention %q: %s", tc.wantDetail, rec.Body.String())
			}
		})
	}
}

func TestLocationDeleteAnswers404BeforeTheGuard(t *testing.T) {
	spy := &deleteGuardRepo{err: storage.ErrNotFound}
	h, err := NewRouter(locationTestConfig(spy.repo()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodDelete, locationItemPath+"?reassign_to=loc-x", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE of an unknown location = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	for _, leak := range []string{"child_count", "item_count", "location-not-empty"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("the 404 body carries %q: %s -- a not-found must say nothing about what the id would have held", leak, rec.Body.String())
		}
	}
}

func TestLocationTreeIsNotSwallowedByTheWildcard(t *testing.T) {
	treeCalled := false
	repo := fakeLocationRepository{
		treeFn: func(context.Context) ([]*storage.LocationNode, error) {
			treeCalled = true
			return nil, nil
		},
		getFn: func(_ context.Context, id string) (storage.Location, error) {
			t.Fatalf("GET /locations/tree was routed to the {locationID} wildcard with id %q", id)
			return storage.Location{}, nil
		},
	}
	h, err := NewRouter(locationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/locations/tree", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /locations/tree = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !treeCalled {
		t.Fatal("the tree handler was never reached")
	}
}

func TestLocationTreeWireShape(t *testing.T) {
	repo := fakeLocationRepository{
		treeFn: func(context.Context) ([]*storage.LocationNode, error) {
			shelf := &storage.LocationNode{
				Location:       storage.Location{ID: "loc-shelf", Name: "Shelf", ParentID: sql.NullString{String: "loc-garage", Valid: true}, CreatedAt: 5, UpdatedAt: 6, Version: 2},
				ItemCount:      2,
				TotalItemCount: 2,
				Children:       []*storage.LocationNode{},
			}
			garage := &storage.LocationNode{
				Location:       storage.Location{ID: "loc-garage", Name: "Garage", CreatedAt: 1, UpdatedAt: 2, Version: 1},
				ItemCount:      1,
				TotalItemCount: 3,
				Children:       []*storage.LocationNode{shelf},
			}
			return []*storage.LocationNode{garage}, nil
		},
	}
	h, err := NewRouter(locationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/locations/tree", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Tree []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			ParentID       string `json:"parent_id"`
			CreatedAt      int64  `json:"created_at"`
			UpdatedAt      int64  `json:"updated_at"`
			Version        int64  `json:"version"`
			ItemCount      int64  `json:"item_count"`
			TotalItemCount int64  `json:"total_item_count"`
			Children       []struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				ParentID       string `json:"parent_id"`
				ItemCount      int64  `json:"item_count"`
				TotalItemCount int64  `json:"total_item_count"`
				Children       []any  `json:"children"`
			} `json:"children"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(body.Tree) != 1 {
		t.Fatalf("tree has %d roots, want 1", len(body.Tree))
	}
	root := body.Tree[0]
	if root.ID != "loc-garage" || root.Name != "Garage" || root.Version != 1 || root.CreatedAt != 1 || root.UpdatedAt != 2 {
		t.Errorf("root node = %+v; a node must carry the same location fields every other location response does", root)
	}
	if root.ParentID != "" {
		t.Errorf("a root carries parent_id %q, want it absent", root.ParentID)
	}
	if root.ItemCount != 1 || root.TotalItemCount != 3 {
		t.Errorf("root counts = %d/%d, want 1/3", root.ItemCount, root.TotalItemCount)
	}
	if len(root.Children) != 1 {
		t.Fatalf("root has %d children, want 1", len(root.Children))
	}
	child := root.Children[0]
	if child.ID != "loc-shelf" || child.ParentID != "loc-garage" || child.ItemCount != 2 || child.TotalItemCount != 2 {
		t.Errorf("child node = %+v, want loc-shelf under loc-garage with 2/2", child)
	}
	if child.Children == nil {
		t.Error("a leaf's children decoded as null; it must be [] so a client can iterate without a null check")
	}
}

func TestLocationTreeRendersLeafChildrenAsAnEmptyArray(t *testing.T) {
	repo := fakeLocationRepository{
		treeFn: func(context.Context) ([]*storage.LocationNode, error) {
			return []*storage.LocationNode{{
				Location: storage.Location{ID: "loc-1", Name: "Solo", Version: 1},
				Children: []*storage.LocationNode{},
			}}, nil
		},
	}
	h, err := NewRouter(locationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/locations/tree", nil)
	if strings.Contains(rec.Body.String(), `"children":null`) {
		t.Fatalf("a leaf rendered children as null: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"children":[]`) {
		t.Fatalf("a leaf did not render children as []: %s", rec.Body.String())
	}
}

func TestLocationTreeRendersAnEmptyTreeAsAnEmptyArray(t *testing.T) {
	repo := fakeLocationRepository{
		treeFn: func(context.Context) ([]*storage.LocationNode, error) { return nil, nil },
	}
	h, err := NewRouter(locationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/locations/tree", nil)
	if !strings.Contains(rec.Body.String(), `"tree":[]`) {
		t.Fatalf("an empty tree rendered as %s, want {\"tree\":[]}", rec.Body.String())
	}
}

func TestLocationTreeTranslatesAStorageFailure(t *testing.T) {
	repo := fakeLocationRepository{
		treeFn: func(context.Context) ([]*storage.LocationNode, error) { return nil, errors.New("boom") },
	}
	h, err := NewRouter(locationTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/locations/tree", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET with a failing storage = %d, want 500", rec.Code)
	}
}
