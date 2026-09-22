package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/items"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type fakeItemRepository struct {
	getFn            func(ctx context.Context, itemID string) (storage.Item, error)
	listFn           func(ctx context.Context, page storage.Page) ([]storage.Item, error)
	listFilteredFn   func(ctx context.Context, filter storage.ItemFilter, page storage.Page) ([]storage.Item, error)
	createFn         func(ctx context.Context, p storage.CreateItemParams) (storage.Item, error)
	updateFn         func(ctx context.Context, p storage.UpdateItemParams) (storage.Item, error)
	deleteFn         func(ctx context.Context, itemID string, now int64) error
	getByShortCodeFn func(ctx context.Context, shortCode string) (storage.Item, error)
	getByIDsFn       func(ctx context.Context, ids []string) ([]storage.Item, error)
}

func (f fakeItemRepository) Get(ctx context.Context, itemID string) (storage.Item, error) {
	return f.getFn(ctx, itemID)
}

func (f fakeItemRepository) GetByShortCode(ctx context.Context, shortCode string) (storage.Item, error) {
	if f.getByShortCodeFn == nil {
		panic("httpapi: fakeItemRepository.GetByShortCode called with no getByShortCodeFn set")
	}
	return f.getByShortCodeFn(ctx, shortCode)
}

func (f fakeItemRepository) GetByIDs(ctx context.Context, ids []string) ([]storage.Item, error) {
	if f.getByIDsFn == nil {
		panic("httpapi: fakeItemRepository.GetByIDs called with no getByIDsFn set")
	}
	return f.getByIDsFn(ctx, ids)
}

func (f fakeItemRepository) List(ctx context.Context, page storage.Page) ([]storage.Item, error) {
	return f.listFn(ctx, page)
}

func (f fakeItemRepository) ListFiltered(ctx context.Context, filter storage.ItemFilter, page storage.Page) ([]storage.Item, error) {
	if f.listFilteredFn != nil {
		return f.listFilteredFn(ctx, filter, page)
	}
	return f.listFn(ctx, page)
}

func (f fakeItemRepository) Create(ctx context.Context, p storage.CreateItemParams) (storage.Item, error) {
	return f.createFn(ctx, p)
}

func (f fakeItemRepository) Update(ctx context.Context, p storage.UpdateItemParams) (storage.Item, error) {
	return f.updateFn(ctx, p)
}

func (f fakeItemRepository) Delete(ctx context.Context, itemID string, now int64) error {
	return f.deleteFn(ctx, itemID, now)
}

type itemsFakeScope struct {
	group storage.GroupID
	repo  storage.ItemRepository
}

func (s itemsFakeScope) GroupID() storage.GroupID      { return s.group }
func (s itemsFakeScope) Items() storage.ItemRepository { return s.repo }

func (s itemsFakeScope) Warranty() storage.WarrantyRepository { return nil }

func (s itemsFakeScope) Sale() storage.SaleRepository { return nil }

func (s itemsFakeScope) Purchase() storage.PurchaseRepository          { return nil }
func (s itemsFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }

func (s itemsFakeScope) Identifications() storage.IdentificationRepository { return nil }

func (s itemsFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }

func (s itemsFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }

func (s itemsFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s itemsFakeScope) Locations() storage.LocationRepository               { return nil }

func (s itemsFakeScope) Labels() storage.LabelRepository           { return nil }
func (s itemsFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s itemsFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s itemsFakeScope) Members() storage.MemberRepository { return nil }

type itemsFakeScopes struct{ repo storage.ItemRepository }

func (s itemsFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return itemsFakeScope{group: g, repo: s.repo}, nil
}

func itemsTestConfig(repo storage.ItemRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = itemsFakeScopes{repo: repo}
	return cfg
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestItemCreateHandlerSucceedsWithNameOnly(t *testing.T) {
	var gotParams storage.CreateItemParams
	repo := fakeItemRepository{
		createFn: func(_ context.Context, p storage.CreateItemParams) (storage.Item, error) {
			gotParams = p
			return storage.Item{ID: "itm-1", Name: p.Name, ShortCode: p.ShortCode, Version: 1}, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPost, "/api/v1/items", map[string]string{"name": "A Lamp"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", rec.Code, rec.Body.String())
	}
	if gotParams.Name != "A Lamp" {
		t.Errorf("repo.Create saw Name = %q", gotParams.Name)
	}

	var got itemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != "itm-1" || got.Name != "A Lamp" {
		t.Errorf("response body = %+v", got)
	}
}

func TestItemCreateHandlerRejectsEmptyName(t *testing.T) {
	repo := fakeItemRepository{
		createFn: func(context.Context, storage.CreateItemParams) (storage.Item, error) {
			t.Fatal("repo.Create was called for a request with no name")
			return storage.Item{}, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPost, "/api/v1/items", map[string]string{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestItemCreateHandlerRejectsMalformedJSON(t *testing.T) {
	repo := fakeItemRepository{
		createFn: func(context.Context, storage.CreateItemParams) (storage.Item, error) {
			t.Fatal("repo.Create was called for malformed JSON")
			return storage.Item{}, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/items", bytes.NewBufferString("{not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestItemGetHandlerReturns404ForNotFound(t *testing.T) {
	repo := fakeItemRepository{
		getFn: func(context.Context, string) (storage.Item, error) { return storage.Item{}, storage.ErrNotFound },
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/items/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", rec.Code, rec.Body.String())
	}
	var p problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem body: %v", err)
	}
	if p.Detail != "" {
		t.Errorf("problem detail = %q, want empty (404-never-403: no detail that could distinguish unknown from foreign-group)", p.Detail)
	}
}

func TestItemGetHandlerSucceeds(t *testing.T) {
	repo := fakeItemRepository{
		getFn: func(_ context.Context, id string) (storage.Item, error) {
			return storage.Item{ID: id, Name: "Found It", Version: 3}, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/items/itm-7", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	var got itemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "itm-7" || got.Name != "Found It" || got.Version != 3 {
		t.Errorf("response = %+v", got)
	}
}

func TestItemListHandlerReturnsAnEmptyArrayNeverNull(t *testing.T) {
	repo := fakeItemRepository{
		listFn: func(context.Context, storage.Page) ([]storage.Item, error) { return nil, nil },
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/items", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte(`"items":[]`)) {
		t.Errorf("body = %s, want an empty array for items, not null", got)
	}
}

func TestItemListHandlerAppliesDefaultAndCustomPaging(t *testing.T) {
	var got storage.Page
	repo := fakeItemRepository{
		listFn: func(_ context.Context, p storage.Page) ([]storage.Item, error) {
			got = p
			return nil, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items", nil); rec.Code != http.StatusOK {
		t.Fatalf("default paging: status = %d", rec.Code)
	}
	if got.Limit != defaultItemListLimit || got.Offset != 0 {
		t.Errorf("default paging: Page = %+v, want {Limit: %d, Offset: 0}", got, defaultItemListLimit)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items?limit=10&offset=20", nil); rec.Code != http.StatusOK {
		t.Fatalf("custom paging: status = %d", rec.Code)
	}
	if got.Limit != 10 || got.Offset != 20 {
		t.Errorf("custom paging: Page = %+v, want {Limit: 10, Offset: 20}", got)
	}
}

func TestItemListHandlerRejectsOutOfRangeLimit(t *testing.T) {
	repo := fakeItemRepository{
		listFn: func(context.Context, storage.Page) ([]storage.Item, error) {
			return nil, errors.New("should not be reached")
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, q := range []string{"limit=0", "limit=-1", "limit=abc", "offset=-1"} {
		rec := doJSON(t, h, http.MethodGet, "/api/v1/items?"+q, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("?%s: status = %d, want 400", q, rec.Code)
		}
	}
}

func TestItemUpdateHandlerAppliesUnderMatchingVersion(t *testing.T) {
	var gotParams storage.UpdateItemParams
	repo := fakeItemRepository{
		updateFn: func(_ context.Context, p storage.UpdateItemParams) (storage.Item, error) {
			gotParams = p
			return storage.Item{ID: p.ItemID, Name: p.Name, Version: p.ExpectedVersion + 1}, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPut, "/api/v1/items/itm-1", map[string]any{
		"name": "Renamed", "version": 1,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if gotParams.ItemID != "itm-1" || gotParams.ExpectedVersion != 1 {
		t.Errorf("repo.Update saw %+v", gotParams)
	}
}

func TestItemUpdateHandlerReturns409ForVersionMismatch(t *testing.T) {
	repo := fakeItemRepository{
		updateFn: func(context.Context, storage.UpdateItemParams) (storage.Item, error) {
			return storage.Item{}, storage.ErrVersionMismatch
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPut, "/api/v1/items/itm-1", map[string]any{"name": "x", "version": 1})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rec.Code, rec.Body.String())
	}
}

func TestItemUpdateHandlerReturns404ForNotFound(t *testing.T) {
	repo := fakeItemRepository{
		updateFn: func(context.Context, storage.UpdateItemParams) (storage.Item, error) {
			return storage.Item{}, storage.ErrNotFound
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPut, "/api/v1/items/itm-1", map[string]any{"name": "x", "version": 1})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", rec.Code, rec.Body.String())
	}
}

func TestItemUpdateHandlerRejectsMissingVersion(t *testing.T) {
	repo := fakeItemRepository{
		updateFn: func(context.Context, storage.UpdateItemParams) (storage.Item, error) {
			t.Fatal("repo.Update was called with no version supplied")
			return storage.Item{}, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPut, "/api/v1/items/itm-1", map[string]any{"name": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestItemDeleteHandlerSucceeds(t *testing.T) {
	var gotItemID string
	repo := fakeItemRepository{
		deleteFn: func(_ context.Context, itemID string, now int64) error {
			gotItemID = itemID
			if now <= 0 {
				t.Errorf("now = %d, want a positive Unix-millisecond timestamp", now)
			}
			return nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodDelete, "/api/v1/items/itm-1", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s, want 204", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 body = %q, want empty", rec.Body.String())
	}
	if gotItemID != "itm-1" {
		t.Errorf("repo.Delete called with itemID %q, want %q -- the {itemID} wildcard is not being resolved", gotItemID, "itm-1")
	}
}

func TestItemDeleteHandlerReturns404ForNotFound(t *testing.T) {
	repo := fakeItemRepository{
		deleteFn: func(context.Context, string, int64) error { return storage.ErrNotFound },
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodDelete, "/api/v1/items/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", rec.Code, rec.Body.String())
	}
	var p problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem body: %v", err)
	}
	if p.Detail != "" {
		t.Errorf("problem detail = %q, want empty (404-never-403: no detail that could distinguish unknown from foreign-group)", p.Detail)
	}
}

func TestItemDeleteHandlerReturns500ForAnUnexpectedFault(t *testing.T) {
	repo := fakeItemRepository{
		deleteFn: func(context.Context, string, int64) error { return sql.ErrConnDone },
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodDelete, "/api/v1/items/itm-1", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s, want 500", rec.Code, rec.Body.String())
	}
}

var _ storage.ItemRepository = fakeItemRepository{}

func TestItemsPackageIsWiredThroughTheFakeRepository(t *testing.T) {
	repo := fakeItemRepository{
		createFn: func(_ context.Context, p storage.CreateItemParams) (storage.Item, error) {
			return storage.Item{ID: p.ID, Name: p.Name}, nil
		},
	}
	item, err := items.Create(t.Context(), repo, items.CreateRequest{Name: "x"})
	if err != nil || item.Name != "x" {
		t.Fatalf("items.Create(fakeItemRepository) = %+v, %v", item, err)
	}
}

type filterSpy struct {
	got  *storage.ItemFilter
	page *storage.Page
}

func (s *filterSpy) repo() fakeItemRepository {
	return fakeItemRepository{
		listFn: func(context.Context, storage.Page) ([]storage.Item, error) {
			return nil, errors.New("List must not be called; itemListHandler goes through ListFiltered")
		},
		listFilteredFn: func(_ context.Context, f storage.ItemFilter, p storage.Page) ([]storage.Item, error) {
			filter, page := f, p
			s.got, s.page = &filter, &page
			return []storage.Item{}, nil
		},
	}
}

type descendantsOnlyLocations struct {
	fn func(ctx context.Context, id string) ([]string, error)
}

func (r descendantsOnlyLocations) Descendants(ctx context.Context, id string) ([]string, error) {
	if r.fn == nil {
		return nil, nil
	}
	return r.fn(ctx, id)
}

func (r descendantsOnlyLocations) Get(context.Context, string) (storage.Location, error) {
	panic("itemListHandler must not read individual locations")
}
func (r descendantsOnlyLocations) List(context.Context) ([]storage.Location, error) {
	panic("itemListHandler must not list locations")
}
func (r descendantsOnlyLocations) Create(context.Context, storage.CreateLocationParams) (storage.Location, error) {
	panic("itemListHandler must not create locations")
}
func (r descendantsOnlyLocations) Update(context.Context, storage.UpdateLocationParams) (storage.Location, error) {
	panic("itemListHandler must not update locations")
}
func (r descendantsOnlyLocations) Delete(context.Context, storage.DeleteLocationParams) error {
	panic("itemListHandler must not delete locations")
}
func (r descendantsOnlyLocations) Tree(context.Context) ([]*storage.LocationNode, error) {
	panic("itemListHandler must not build the location tree")
}

type itemAndLocationScope struct {
	itemsFakeScope
	locations storage.LocationRepository
}

func (s itemAndLocationScope) Locations() storage.LocationRepository { return s.locations }

type itemAndLocationScopes struct {
	items       storage.ItemRepository
	descendants func(ctx context.Context, id string) ([]string, error)
}

func (s itemAndLocationScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return itemAndLocationScope{
		itemsFakeScope: itemsFakeScope{group: g, repo: s.items},
		locations:      descendantsOnlyLocations{fn: s.descendants},
	}, nil
}

func itemFilterConfig(repo storage.ItemRepository, descendants func(ctx context.Context, id string) ([]string, error)) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = itemAndLocationScopes{items: repo, descendants: descendants}
	return cfg
}

func TestItemListPassesFiltersThrough(t *testing.T) {
	for name, tc := range map[string]struct {
		path       string
		wantQuery  string
		wantLocs   []string
		wantLabels []string
	}{
		"no filters at all":   {"/api/v1/items", "", nil, nil},
		"search":              {"/api/v1/items?q=drill", "drill", nil, nil},
		"search with a space": {"/api/v1/items?q=cordless+drill", "cordless drill", nil, nil},
		"one location":        {"/api/v1/items?location_id=loc-garage", "", []string{"loc-garage"}, nil},
		"one label":           {"/api/v1/items?label_id=lbl-fragile", "", nil, []string{"lbl-fragile"}},
		"repeated labels":     {"/api/v1/items?label_id=lbl-a&label_id=lbl-b", "", nil, []string{"lbl-a", "lbl-b"}},
		"all three":           {"/api/v1/items?q=drill&location_id=loc-garage&label_id=lbl-a", "drill", []string{"loc-garage"}, []string{"lbl-a"}},
	} {
		t.Run(name, func(t *testing.T) {
			spy := &filterSpy{}
			h, err := NewRouter(itemFilterConfig(spy.repo(), nil))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			if rec := doJSON(t, h, http.MethodGet, tc.path, nil); rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200: %s", tc.path, rec.Code, rec.Body.String())
			}
			if spy.got == nil {
				t.Fatal("the handler never reached ListFiltered")
			}
			if spy.got.Query != tc.wantQuery {
				t.Errorf("Query = %q, want %q", spy.got.Query, tc.wantQuery)
			}
			if strings.Join(spy.got.LocationIDs, ",") != strings.Join(tc.wantLocs, ",") {
				t.Errorf("LocationIDs = %v, want %v", spy.got.LocationIDs, tc.wantLocs)
			}
			if strings.Join(spy.got.LabelIDs, ",") != strings.Join(tc.wantLabels, ",") {
				t.Errorf("LabelIDs = %v, want %v", spy.got.LabelIDs, tc.wantLabels)
			}
		})
	}
}

func TestItemListPassesCustomFieldWarrantyAndDateFiltersThrough(t *testing.T) {
	for name, tc := range map[string]struct {
		path            string
		wantCustom      []storage.CustomFieldMatch
		wantStatus      string
		wantCreatedFrom *int64
		wantCreatedTo   *int64
		wantUpdatedFrom *int64
		wantUpdatedTo   *int64
	}{
		"no filters at all": {"/api/v1/items", nil, "", nil, nil, nil, nil},
		"one custom field":  {"/api/v1/items?custom_field=Colour:Red", []storage.CustomFieldMatch{{Name: "Colour", Value: "Red"}}, "", nil, nil, nil, nil},
		"repeated custom fields": {
			"/api/v1/items?custom_field=Colour:Red&custom_field=Weight:12.5",
			[]storage.CustomFieldMatch{{Name: "Colour", Value: "Red"}, {Name: "Weight", Value: "12.5"}},
			"", nil, nil, nil, nil,
		},
		"warranty status":   {"/api/v1/items?warranty_status=active", nil, "active", nil, nil, nil, nil},
		"created_from only": {"/api/v1/items?created_from=1000", nil, "", i64Ptr(1000), nil, nil, nil},
		"created_to only":   {"/api/v1/items?created_to=2000", nil, "", nil, i64Ptr(2000), nil, nil},
		"updated range":     {"/api/v1/items?updated_from=1000&updated_to=2000", nil, "", nil, nil, i64Ptr(1000), i64Ptr(2000)},
		"everything at once": {
			"/api/v1/items?custom_field=Colour:Red&warranty_status=lifetime&created_from=1000&created_to=2000&updated_from=3000&updated_to=4000",
			[]storage.CustomFieldMatch{{Name: "Colour", Value: "Red"}}, "lifetime",
			i64Ptr(1000), i64Ptr(2000), i64Ptr(3000), i64Ptr(4000),
		},
	} {
		t.Run(name, func(t *testing.T) {
			spy := &filterSpy{}
			h, err := NewRouter(itemFilterConfig(spy.repo(), nil))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			if rec := doJSON(t, h, http.MethodGet, tc.path, nil); rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200: %s", tc.path, rec.Code, rec.Body.String())
			}
			if spy.got == nil {
				t.Fatal("the handler never reached ListFiltered")
			}
			if !reflect.DeepEqual(spy.got.CustomFields, tc.wantCustom) {
				t.Errorf("CustomFields = %+v, want %+v", spy.got.CustomFields, tc.wantCustom)
			}
			if spy.got.WarrantyStatus != tc.wantStatus {
				t.Errorf("WarrantyStatus = %q, want %q", spy.got.WarrantyStatus, tc.wantStatus)
			}
			if !reflect.DeepEqual(spy.got.CreatedFrom, tc.wantCreatedFrom) {
				t.Errorf("CreatedFrom = %v, want %v", derefOrNil(spy.got.CreatedFrom), derefOrNil(tc.wantCreatedFrom))
			}
			if !reflect.DeepEqual(spy.got.CreatedTo, tc.wantCreatedTo) {
				t.Errorf("CreatedTo = %v, want %v", derefOrNil(spy.got.CreatedTo), derefOrNil(tc.wantCreatedTo))
			}
			if !reflect.DeepEqual(spy.got.UpdatedFrom, tc.wantUpdatedFrom) {
				t.Errorf("UpdatedFrom = %v, want %v", derefOrNil(spy.got.UpdatedFrom), derefOrNil(tc.wantUpdatedFrom))
			}
			if !reflect.DeepEqual(spy.got.UpdatedTo, tc.wantUpdatedTo) {
				t.Errorf("UpdatedTo = %v, want %v", derefOrNil(spy.got.UpdatedTo), derefOrNil(tc.wantUpdatedTo))
			}
		})
	}
}

func i64Ptr(i int64) *int64 { return &i }
func derefOrNil(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestItemListDropsMalformedCustomFieldEntriesIndividually(t *testing.T) {
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	path := "/api/v1/items?custom_field=NoColonHere&custom_field=Colour:Red&custom_field=:OrphanValue"
	if rec := doJSON(t, h, http.MethodGet, path, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	want := []storage.CustomFieldMatch{{Name: "Colour", Value: "Red"}}
	if !reflect.DeepEqual(spy.got.CustomFields, want) {
		t.Errorf("CustomFields = %+v, want %+v -- the two malformed entries must be dropped, not the whole filter", spy.got.CustomFields, want)
	}
}

func TestItemListAllowsAnEmptyCustomFieldValue(t *testing.T) {
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items?custom_field=Colour:", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	want := []storage.CustomFieldMatch{{Name: "Colour", Value: ""}}
	if !reflect.DeepEqual(spy.got.CustomFields, want) {
		t.Errorf("CustomFields = %+v, want %+v", spy.got.CustomFields, want)
	}
}

func TestItemListIgnoresUnparsableDateRangeValues(t *testing.T) {
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items?created_from=not-a-number", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if spy.got.CreatedFrom != nil {
		t.Errorf("CreatedFrom = %v, want nil for an unparsable value", *spy.got.CreatedFrom)
	}
}

func TestItemListExpandsDescendants(t *testing.T) {
	var askedFor string
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), func(_ context.Context, id string) ([]string, error) {
		askedFor = id
		return []string{"loc-garage", "loc-shelf", "loc-box"}, nil
	}))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items?location_id=loc-garage&descendants=true", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if askedFor != "loc-garage" {
		t.Errorf("Descendants was asked for %q, want loc-garage", askedFor)
	}
	if strings.Join(spy.got.LocationIDs, ",") != "loc-garage,loc-shelf,loc-box" {
		t.Errorf("LocationIDs = %v, want the whole subtree", spy.got.LocationIDs)
	}
}

func TestItemListIgnoresDescendantsWithoutALocation(t *testing.T) {
	called := false
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), func(context.Context, string) ([]string, error) {
		called = true
		return []string{"loc-x"}, nil
	}))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items?descendants=true", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", rec.Code)
	}
	if called {
		t.Error("descendants=true with no location_id expanded something")
	}
	if len(spy.got.LocationIDs) != 0 {
		t.Errorf("LocationIDs = %v, want none", spy.got.LocationIDs)
	}
}

func TestItemListNeverWidensOnAnUnresolvableSubtree(t *testing.T) {
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), func(context.Context, string) ([]string, error) {
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items?location_id=loc-gone&descendants=true", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", rec.Code)
	}
	if strings.Join(spy.got.LocationIDs, ",") != "loc-gone" {
		t.Fatalf("LocationIDs = %v, want [loc-gone] -- an unresolvable subtree must still filter (matching nothing), never widen to every location", spy.got.LocationIDs)
	}
}

func TestItemListFiltersAreNeverA400(t *testing.T) {
	for _, path := range []string{
		"/api/v1/items?location_id=loc-does-not-exist",
		"/api/v1/items?label_id=lbl-does-not-exist",
		"/api/v1/items?q=" + url.QueryEscape(`"; DROP TABLE items; --`),
		"/api/v1/items?q=" + url.QueryEscape("SN-999"),
		"/api/v1/items?location_id=loc-a&descendants=yes-please",
		"/api/v1/items?descendants=",
		"/api/v1/items?custom_field=NoColonHere",
		"/api/v1/items?custom_field=" + url.QueryEscape(":OrphanValue"),
		"/api/v1/items?warranty_status=not-a-real-status",
		"/api/v1/items?created_from=not-a-number",
		"/api/v1/items?updated_to=" + url.QueryEscape("2026-01-01"),
	} {
		t.Run(path, func(t *testing.T) {
			spy := &filterSpy{}
			h, err := NewRouter(itemFilterConfig(spy.repo(), func(context.Context, string) ([]string, error) {
				return []string{"loc-a"}, nil
			}))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			if rec := doJSON(t, h, http.MethodGet, path, nil); rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200 -- a filter naming something unknown has an empty answer, not an error: %s", path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestItemListStillPaginatesAlongsideFilters(t *testing.T) {
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items?q=drill&limit=10&offset=20", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if spy.page.Limit != 10 || spy.page.Offset != 20 {
		t.Errorf("page = %+v, want limit 10 offset 20", *spy.page)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/v1/items?q=drill&limit=99999", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET with an out-of-range limit alongside a filter = %d, want 400 -- limit keeps its strict parsing (A91)", rec.Code)
	}
}

func TestItemListPassesSortThrough(t *testing.T) {
	for name, tc := range map[string]struct {
		path string
		want storage.ItemSort
	}{
		"absent":                {"/api/v1/items", storage.ItemSort{}},
		"present but empty":     {"/api/v1/items?sort=", storage.ItemSort{}},
		"name ascending":        {"/api/v1/items?sort=name", storage.ItemSort{Field: storage.ItemSortName}},
		"name descending":       {"/api/v1/items?sort=-name", storage.ItemSort{Field: storage.ItemSortName, Descending: true}},
		"quantity ascending":    {"/api/v1/items?sort=quantity", storage.ItemSort{Field: storage.ItemSortQuantity}},
		"quantity descending":   {"/api/v1/items?sort=-quantity", storage.ItemSort{Field: storage.ItemSortQuantity, Descending: true}},
		"created_at ascending":  {"/api/v1/items?sort=created_at", storage.ItemSort{Field: storage.ItemSortCreatedAt}},
		"created_at descending": {"/api/v1/items?sort=-created_at", storage.ItemSort{Field: storage.ItemSortCreatedAt, Descending: true}},
		"updated_at ascending":  {"/api/v1/items?sort=updated_at", storage.ItemSort{Field: storage.ItemSortUpdatedAt}},
		"updated_at descending": {"/api/v1/items?sort=-updated_at", storage.ItemSort{Field: storage.ItemSortUpdatedAt, Descending: true}},
	} {
		t.Run(name, func(t *testing.T) {
			spy := &filterSpy{}
			h, err := NewRouter(itemFilterConfig(spy.repo(), nil))
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			if rec := doJSON(t, h, http.MethodGet, tc.path, nil); rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200: %s", tc.path, rec.Code, rec.Body.String())
			}
			if spy.got == nil {
				t.Fatal("the handler never reached ListFiltered")
			}
			if spy.got.Sort != tc.want {
				t.Errorf("Sort = %+v, want %+v", spy.got.Sort, tc.want)
			}
		})
	}
}

func TestItemListHandlerRejectsUnknownSort(t *testing.T) {
	repo := fakeItemRepository{
		listFilteredFn: func(context.Context, storage.ItemFilter, storage.Page) ([]storage.Item, error) {
			return nil, errors.New("should not be reached")
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, q := range []string{
		"sort=bogus",
		"sort=-bogus",
		"sort=location",
		"sort=name,quantity",
		"sort=-",
		"sort=NAME",
	} {
		t.Run(q, func(t *testing.T) {
			rec := doJSON(t, h, http.MethodGet, "/api/v1/items?"+q, nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("?%s: status = %d, want 400", q, rec.Code)
			}
		})
	}
}

func TestItemListSortIsValidatedBeforeFilterResolution(t *testing.T) {
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), func(context.Context, string) ([]string, error) {
		panic("descendants must not be resolved when sort is rejected first")
	}))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/items?sort=bogus&location_id=loc-a&descendants=true", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if spy.got != nil {
		t.Fatal("the handler reached ListFiltered despite an invalid sort")
	}
}

func TestItemListSortComposesWithPaginationAndFilters(t *testing.T) {
	spy := &filterSpy{}
	h, err := NewRouter(itemFilterConfig(spy.repo(), nil))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/items?q=drill&sort=-quantity&limit=10&offset=20", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if spy.got == nil {
		t.Fatal("the handler never reached ListFiltered")
	}
	if spy.got.Query != "drill" {
		t.Errorf("Query = %q, want %q", spy.got.Query, "drill")
	}
	if want := (storage.ItemSort{Field: storage.ItemSortQuantity, Descending: true}); spy.got.Sort != want {
		t.Errorf("Sort = %+v, want %+v", spy.got.Sort, want)
	}
	if spy.page.Limit != 10 || spy.page.Offset != 20 {
		t.Errorf("page = %+v, want limit 10 offset 20", *spy.page)
	}
}
