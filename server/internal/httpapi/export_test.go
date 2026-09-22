package httpapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type exportFakeScope struct {
	group            storage.GroupID
	items            storage.ItemRepository
	customFieldDefs  storage.CustomFieldDefRepository
	locations        storage.LocationRepository
	itemLabels       storage.ItemLabelRepository
	identifications  storage.IdentificationRepository
	attachments      storage.AttachmentRepository
	itemCustomFields storage.ItemCustomFieldRepository
	warranty         storage.WarrantyRepository
	purchase         storage.PurchaseRepository
	sale             storage.SaleRepository
}

func (s exportFakeScope) GroupID() storage.GroupID                      { return s.group }
func (s exportFakeScope) Items() storage.ItemRepository                 { return s.items }
func (s exportFakeScope) Warranty() storage.WarrantyRepository          { return s.warranty }
func (s exportFakeScope) Sale() storage.SaleRepository                  { return s.sale }
func (s exportFakeScope) Purchase() storage.PurchaseRepository          { return s.purchase }
func (s exportFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }
func (s exportFakeScope) Identifications() storage.IdentificationRepository {
	return s.identifications
}
func (s exportFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return s.customFieldDefs }
func (s exportFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository {
	return s.itemCustomFields
}
func (s exportFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s exportFakeScope) Locations() storage.LocationRepository               { return s.locations }
func (s exportFakeScope) Labels() storage.LabelRepository                     { return nil }
func (s exportFakeScope) ItemLabels() storage.ItemLabelRepository             { return s.itemLabels }
func (s exportFakeScope) Attachments() storage.AttachmentRepository           { return s.attachments }
func (s exportFakeScope) Members() storage.MemberRepository                   { return nil }

type exportFakeScopes struct{ scope storage.Scope }

func (s exportFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) { return s.scope, nil }

func exportTestConfig(scope storage.Scope) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = exportFakeScopes{scope: scope}
	return cfg
}

func notFoundGet[T any](context.Context, string) (T, error) {
	var zero T
	return zero, storage.ErrNotFound
}

func emptyList[T any](context.Context, string) ([]T, error) { return nil, nil }

func exportEmptyScope(items storage.ItemRepository, defs storage.CustomFieldDefRepository, locations storage.LocationRepository) exportFakeScope {
	return exportFakeScope{
		group:            storage.MustGroupID(testGroup),
		items:            items,
		customFieldDefs:  defs,
		locations:        locations,
		itemLabels:       fakeItemLabelRepository{listFn: emptyList[storage.Label]},
		identifications:  fakeIdentificationRepository{listFn: emptyList[storage.Identification]},
		attachments:      fakeAttachmentRepository{listForItemFn: emptyList[storage.Attachment]},
		itemCustomFields: fakeItemCustomFieldRepository{listFn: emptyList[storage.ItemCustomField]},
		warranty:         fakeWarrantyRepository{getFn: notFoundGet[storage.Warranty]},
		purchase:         fakePurchaseRepository{getFn: notFoundGet[storage.Purchase]},
		sale:             fakeSaleRepository{getFn: notFoundGet[storage.Sale]},
	}
}

func noCustomFieldDefs() storage.CustomFieldDefRepository {
	return fakeCustomFieldDefRepository{listFn: func(context.Context) ([]storage.CustomFieldDef, error) { return nil, nil }}
}

func noLocations() storage.LocationRepository {
	return fakeLocationRepository{listFn: func(context.Context) ([]storage.Location, error) { return nil, nil }}
}

func itemsOf(items ...storage.Item) storage.ItemRepository {
	return fakeItemRepository{
		listFn: func(_ context.Context, page storage.Page) ([]storage.Item, error) {
			lo := page.Offset
			if lo >= int64(len(items)) {
				return nil, nil
			}
			hi := lo + page.Limit
			if hi > int64(len(items)) {
				hi = int64(len(items))
			}
			return items[lo:hi], nil
		},
	}
}

func mustGetCSV(t *testing.T, cfg Config) *httptest.ResponseRecorder {
	t.Helper()
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/items.csv", nil))
	return rec
}

func parseCSV(t *testing.T, rec *httptest.ResponseRecorder) [][]string {
	t.Helper()
	records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("response body is not valid CSV: %v; body = %q", err, rec.Body.String())
	}
	return records
}

func TestExportItemsCSVHandlerHeadersAndEmptyGroup(t *testing.T) {
	scope := exportEmptyScope(itemsOf(), noCustomFieldDefs(), noLocations())
	rec := mustGetCSV(t, exportTestConfig(scope))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != exportCSVContentType {
		t.Errorf("Content-Type = %q, want %q", got, exportCSVContentType)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, ".csv") {
		t.Errorf("Content-Disposition = %q, want it to declare an attachment with a .csv filename", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "private, no-store")
	}

	records := parseCSV(t, rec)
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1 (header only): %v", len(records), records)
	}
	if got, want := records[0], importexport.FixedColumns; !equalStrings(got, want) {
		t.Errorf("header = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestExportItemsCSVHandlerAssemblesOneRowPerItem(t *testing.T) {
	item := storage.Item{
		ID:         "itm-1",
		Name:       "Drill",
		Quantity:   2,
		ShortCode:  "SC1",
		LocationID: sql.NullString{String: "loc-child", Valid: true},
	}
	locations := []storage.Location{
		{ID: "loc-root", Name: "Garage"},
		{ID: "loc-child", Name: "Shelf", ParentID: sql.NullString{String: "loc-root", Valid: true}},
	}
	def := storage.CustomFieldDef{ID: "def-1", Name: "Colour", FieldType: "text"}

	scope := exportFakeScope{
		group: storage.MustGroupID(testGroup),
		items: itemsOf(item),
		customFieldDefs: fakeCustomFieldDefRepository{
			listFn: func(context.Context) ([]storage.CustomFieldDef, error) { return []storage.CustomFieldDef{def}, nil },
		},
		locations: fakeLocationRepository{
			listFn: func(context.Context) ([]storage.Location, error) { return locations, nil },
		},
		itemLabels: fakeItemLabelRepository{
			listFn: func(_ context.Context, itemID string) ([]storage.Label, error) {
				return []storage.Label{{ID: "lbl-1", Name: "Tools"}}, nil
			},
		},
		identifications: fakeIdentificationRepository{
			listFn: func(_ context.Context, itemID string) ([]storage.Identification, error) {
				return []storage.Identification{{Kind: "serial", Value: "SN123"}}, nil
			},
		},
		attachments: fakeAttachmentRepository{
			listForItemFn: func(_ context.Context, itemID string) ([]storage.Attachment, error) {
				return []storage.Attachment{{Category: "photo", OriginalFilename: "drill.jpg", Sha256: "abc123"}}, nil
			},
		},
		itemCustomFields: fakeItemCustomFieldRepository{
			listFn: func(_ context.Context, itemID string) ([]storage.ItemCustomField, error) {
				return []storage.ItemCustomField{
					{FieldDefID: sql.NullString{String: "def-1", Valid: true}, Name: "Colour", FieldType: "text", TextValue: sql.NullString{String: "Red", Valid: true}},
					{Name: "Serial Note", FieldType: "text", TextValue: sql.NullString{String: "Ad hoc", Valid: true}},
				}, nil
			},
		},
		warranty: fakeWarrantyRepository{
			getFn: func(_ context.Context, itemID string) (storage.Warranty, error) {
				return storage.Warranty{Holder: "Alice", Provider: "Acme"}, nil
			},
		},
		purchase: fakePurchaseRepository{
			getFn: func(_ context.Context, itemID string) (storage.Purchase, error) {
				return storage.Purchase{Vendor: "Store"}, nil
			},
		},
		sale: fakeSaleRepository{getFn: notFoundGet[storage.Sale]},
	}

	rec := mustGetCSV(t, exportTestConfig(scope))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	records := parseCSV(t, rec)
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2 (header + one item): %v", len(records), records)
	}
	header, row := records[0], records[1]

	col := func(name string) string {
		for i, h := range header {
			if h == name {
				return row[i]
			}
		}
		t.Fatalf("header has no column %q: %v", name, header)
		return ""
	}

	if got := col(importexport.ColumnID); got != "itm-1" {
		t.Errorf("id column = %q, want %q", got, "itm-1")
	}
	if got := col(importexport.ColumnLocationPath); got != "Garage/Shelf" {
		t.Errorf("location_path column = %q, want %q", got, "Garage/Shelf")
	}
	if got := col(importexport.ColumnLocationID); got != "loc-child" {
		t.Errorf("location_id column = %q, want %q", got, "loc-child")
	}
	if got := col(importexport.ColumnLabels); got != "Tools" {
		t.Errorf("labels column = %q, want %q", got, "Tools")
	}
	if got := col(importexport.ColumnIdentifications); got != "serial:SN123" {
		t.Errorf("identifications column = %q, want %q", got, "serial:SN123")
	}
	if got := col(importexport.ColumnAttachments); got != "photo:drill.jpg:abc123" {
		t.Errorf("attachments column = %q, want %q", got, "photo:drill.jpg:abc123")
	}
	if got := col(importexport.ColumnWarrantyHolder); got != "Alice" {
		t.Errorf("warranty_holder column = %q, want %q", got, "Alice")
	}
	if got := col(importexport.ColumnPurchaseVendor); got != "Store" {
		t.Errorf("purchase_vendor column = %q, want %q", got, "Store")
	}
	if got := col(importexport.ColumnSaleBuyerName); got != "" {
		t.Errorf("sale_buyer_name column = %q, want empty (no sale block)", got)
	}
	if got := col("cf:Colour"); got != "Red" {
		t.Errorf("cf:Colour column = %q, want %q", got, "Red")
	}
	if got := col("cfx:text:Serial Note"); got != "Ad hoc" {
		t.Errorf("cfx:text:Serial Note column = %q, want %q", got, "Ad hoc")
	}
}

func TestExportItemsCSVHandlerOrdersRowsByItemID(t *testing.T) {
	scope := exportEmptyScope(
		itemsOf(
			storage.Item{ID: "itm-c", Name: "C"},
			storage.Item{ID: "itm-a", Name: "A"},
			storage.Item{ID: "itm-b", Name: "B"},
		),
		noCustomFieldDefs(),
		noLocations(),
	)
	rec := mustGetCSV(t, exportTestConfig(scope))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	records := parseCSV(t, rec)
	if len(records) != 4 {
		t.Fatalf("records = %d, want 4 (header + 3 items): %v", len(records), records)
	}
	var ids []string
	for _, r := range records[1:] {
		ids = append(ids, r[0])
	}
	want := []string{"itm-a", "itm-b", "itm-c"}
	if !equalStrings(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

func TestExportItemsCSVHandlerPaginatesThroughEveryItem(t *testing.T) {
	n := exportItemPageSize + 5
	items := make([]storage.Item, n)
	for i := range items {
		items[i] = storage.Item{ID: paddedItemID(i), Name: "Item"}
	}
	scope := exportEmptyScope(itemsOf(items...), noCustomFieldDefs(), noLocations())
	rec := mustGetCSV(t, exportTestConfig(scope))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	records := parseCSV(t, rec)
	if len(records) != n+1 {
		t.Fatalf("records = %d, want %d (header + %d items)", len(records), n+1, n)
	}
}

func paddedItemID(i int) string {
	digits := "0123456789"
	b := make([]byte, 0, 8)
	b = append(b, "itm-"...)
	for _, shift := range []int{3, 2, 1, 0} {
		p := 1
		for range shift {
			p *= 10
		}
		b = append(b, digits[(i/p)%10])
	}
	return string(b)
}

func TestExportItemsCSVHandlerReturns500WhenAssemblyFails(t *testing.T) {
	boom := errors.New("boom")
	scope := exportEmptyScope(
		fakeItemRepository{listFn: func(context.Context, storage.Page) ([]storage.Item, error) { return nil, boom }},
		noCustomFieldDefs(),
		noLocations(),
	)
	rec := mustGetCSV(t, exportTestConfig(scope))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Status != http.StatusInternalServerError {
		t.Errorf("problem.Status = %d, want %d", p.Status, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Content-Type"); got == exportCSVContentType {
		t.Error("Content-Type is the CSV type on a 500; headers must not have been written before the failure was known")
	}
}

func TestExportItemsCSVHandlerTreatsAllThreeDetailBlocksAsIndependentlyOptional(t *testing.T) {
	scope := exportEmptyScope(itemsOf(storage.Item{ID: "itm-1", Name: "Bare"}), noCustomFieldDefs(), noLocations())
	rec := mustGetCSV(t, exportTestConfig(scope))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	records := parseCSV(t, rec)
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2: %v", len(records), records)
	}
	header, row := records[0], records[1]
	for _, name := range []string{
		importexport.ColumnWarrantyHolder, importexport.ColumnPurchaseVendor, importexport.ColumnSaleBuyerName,
	} {
		idx := -1
		for i, h := range header {
			if h == name {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("header has no column %q", name)
		}
		if row[idx] != "" {
			t.Errorf("column %q = %q, want empty for an item with no such block", name, row[idx])
		}
	}
}

func TestExportItemsCSVHandlerRejectsUnauthenticated(t *testing.T) {
	cfg := exportTestConfig(exportEmptyScope(itemsOf(), noCustomFieldDefs(), noLocations()))
	cfg.Authenticator = rejectingAuth
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/items.csv", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestBuildLocationPathsWalksNestedAncestorsRootFirst(t *testing.T) {
	locations := []storage.Location{
		{ID: "root", Name: "House"},
		{ID: "mid", Name: "Garage", ParentID: sql.NullString{String: "root", Valid: true}},
		{ID: "leaf", Name: "Shelf 3", ParentID: sql.NullString{String: "mid", Valid: true}},
	}
	paths := buildLocationPaths(locations)

	if got, want := paths["root"], "House"; got != want {
		t.Errorf("paths[root] = %q, want %q", got, want)
	}
	if got, want := paths["mid"], "House/Garage"; got != want {
		t.Errorf("paths[mid] = %q, want %q", got, want)
	}
	if got, want := paths["leaf"], "House/Garage/Shelf 3"; got != want {
		t.Errorf("paths[leaf] = %q, want %q", got, want)
	}
}

func TestBuildLocationPathsNeverInfiniteLoopsOnACycle(t *testing.T) {
	locations := []storage.Location{
		{ID: "a", Name: "A", ParentID: sql.NullString{String: "b", Valid: true}},
		{ID: "b", Name: "B", ParentID: sql.NullString{String: "a", Valid: true}},
	}
	done := make(chan map[string]string, 1)
	go func() { done <- buildLocationPaths(locations) }()
	select {
	case paths := <-done:
		if paths["a"] == "" || paths["b"] == "" {
			t.Errorf("paths = %v, want a non-empty (if degenerate) answer for both cyclic nodes", paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("buildLocationPaths did not terminate on a cyclic parent graph")
	}
}
