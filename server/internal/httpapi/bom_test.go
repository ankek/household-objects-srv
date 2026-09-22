package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const bomTestGroup = "grp-bom"

type bomFixture struct {
	houseID, garageID                 string
	itemA, itemB, itemC, itemD, itemE string
	labelCamping, labelTools          string
}

func seedBoMFixture(t *testing.T, s *storage.Storage, groupID string) bomFixture {
	t.Helper()
	if err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    groupID,
		UserID:       groupID + "-owner",
		Username:     groupID + "-owner",
		PasswordHash: "not-a-real-hash",
		Now:          1,
	}); err != nil {
		t.Fatalf("RegisterNewGroup(%s): %v", groupID, err)
	}
	scope, err := s.ForGroup(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%s): %v", groupID, err)
	}
	id := func(suffix string) string { return groupID + "-" + suffix }
	ctx := t.Context()

	house, err := scope.Locations().Create(ctx, storage.CreateLocationParams{ID: id("loc-house"), Name: "House", Now: 1})
	if err != nil {
		t.Fatalf("create House: %v", err)
	}
	garage, err := scope.Locations().Create(ctx, storage.CreateLocationParams{ID: id("loc-garage"), Name: "Garage", Now: 1})
	if err != nil {
		t.Fatalf("create Garage: %v", err)
	}

	camping, err := scope.Labels().Create(ctx, storage.CreateLabelParams{ID: id("lbl-camping"), Name: "Camping", Color: "#123456", Now: 1})
	if err != nil {
		t.Fatalf("create Camping label: %v", err)
	}
	tools, err := scope.Labels().Create(ctx, storage.CreateLabelParams{ID: id("lbl-tools"), Name: "Tools", Color: "#654321", Now: 1})
	if err != nil {
		t.Fatalf("create Tools label: %v", err)
	}

	itemA, err := scope.Items().Create(ctx, storage.CreateItemParams{ID: id("item-a"), Name: "Tent", LocationID: house.ID, Quantity: 3, ShortCode: id("SC-A"), Now: 1})
	if err != nil {
		t.Fatalf("create itemA: %v", err)
	}
	if _, err := scope.Purchase().Create(ctx, storage.CreatePurchaseParams{ID: id("purchase-a"), ItemID: itemA.ID, PurchasedOn: "2026-01-01", PurchasePriceMinor: 1200, Now: 1}); err != nil {
		t.Fatalf("create itemA purchase: %v", err)
	}
	if err := scope.ItemLabels().Attach(ctx, storage.AttachLabelParams{ID: id("edge-a-camping"), ItemID: itemA.ID, LabelID: camping.ID, Now: 1}); err != nil {
		t.Fatalf("attach camping to itemA: %v", err)
	}
	if _, err := scope.Identifications().Create(ctx, storage.CreateIdentificationParams{ID: id("ident-a-1"), ItemID: itemA.ID, Kind: "serial", Value: "SN-A", Now: 1}); err != nil {
		t.Fatalf("create itemA serial: %v", err)
	}
	if _, err := scope.Identifications().Create(ctx, storage.CreateIdentificationParams{ID: id("ident-a-2"), ItemID: itemA.ID, Kind: "barcode", Value: "000111", Now: 1}); err != nil {
		t.Fatalf("create itemA barcode: %v", err)
	}

	itemB, err := scope.Items().Create(ctx, storage.CreateItemParams{ID: id("item-b"), Name: "Wrench Set", LocationID: garage.ID, Quantity: 2, ShortCode: id("SC-B"), Now: 1})
	if err != nil {
		t.Fatalf("create itemB: %v", err)
	}
	if _, err := scope.Purchase().Create(ctx, storage.CreatePurchaseParams{ID: id("purchase-b"), ItemID: itemB.ID, PurchasedOn: "2026-01-02", PurchasePriceMinor: 800, Now: 1}); err != nil {
		t.Fatalf("create itemB purchase: %v", err)
	}
	if err := scope.ItemLabels().Attach(ctx, storage.AttachLabelParams{ID: id("edge-b-tools"), ItemID: itemB.ID, LabelID: tools.ID, Now: 1}); err != nil {
		t.Fatalf("attach tools to itemB: %v", err)
	}

	itemC, err := scope.Items().Create(ctx, storage.CreateItemParams{ID: id("item-c"), Name: "Stray Bolt", LocationID: house.ID, Quantity: 1, ShortCode: id("SC-C"), Now: 1})
	if err != nil {
		t.Fatalf("create itemC: %v", err)
	}
	if _, err := scope.Identifications().Create(ctx, storage.CreateIdentificationParams{ID: id("ident-c-1"), ItemID: itemC.ID, Kind: "other", Value: "weird:value|with|pipes", Now: 1}); err != nil {
		t.Fatalf("create itemC identification: %v", err)
	}

	itemD, err := scope.Items().Create(ctx, storage.CreateItemParams{ID: id("item-d"), Name: "Multi-tag Widget", Quantity: 5, ShortCode: id("SC-D"), Now: 1})
	if err != nil {
		t.Fatalf("create itemD: %v", err)
	}
	if _, err := scope.Purchase().Create(ctx, storage.CreatePurchaseParams{ID: id("purchase-d"), ItemID: itemD.ID, PurchasedOn: "2026-01-04", PurchasePriceMinor: 300, Now: 1}); err != nil {
		t.Fatalf("create itemD purchase: %v", err)
	}
	if err := scope.ItemLabels().Attach(ctx, storage.AttachLabelParams{ID: id("edge-d-camping"), ItemID: itemD.ID, LabelID: camping.ID, Now: 1}); err != nil {
		t.Fatalf("attach camping to itemD: %v", err)
	}
	if err := scope.ItemLabels().Attach(ctx, storage.AttachLabelParams{ID: id("edge-d-tools"), ItemID: itemD.ID, LabelID: tools.ID, Now: 1}); err != nil {
		t.Fatalf("attach tools to itemD: %v", err)
	}

	itemE, err := scope.Items().Create(ctx, storage.CreateItemParams{ID: id("item-e"), Name: "Explicit Only", Quantity: 4, ShortCode: id("SC-E"), Now: 1})
	if err != nil {
		t.Fatalf("create itemE: %v", err)
	}
	if _, err := scope.Purchase().Create(ctx, storage.CreatePurchaseParams{ID: id("purchase-e"), ItemID: itemE.ID, PurchasedOn: "2026-01-05", PurchasePriceMinor: 700, Now: 1}); err != nil {
		t.Fatalf("create itemE purchase: %v", err)
	}

	return bomFixture{
		houseID: house.ID, garageID: garage.ID,
		itemA: itemA.ID, itemB: itemB.ID, itemC: itemC.ID, itemD: itemD.ID, itemE: itemE.ID,
		labelCamping: camping.ID, labelTools: tools.ID,
	}
}

func bomConfig(t *testing.T, groupID string) Config {
	t.Helper()
	s, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(t.TempDir(), "db", "hho.db"), ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	cfg := testConfig()
	cfg.Authenticator = middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
		return middleware.Identity{Group: groupID, UserID: groupID + "-owner", Role: "owner"}, nil
	})
	cfg.Scopes = s
	cfg.Store = s
	return cfg
}

func doBoMRequest(t *testing.T, cfg Config, target string) *httptest.ResponseRecorder {
	t.Helper()
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func bomCSVRecords(t *testing.T, cfg Config, target string) [][]string {
	t.Helper()
	rec := doBoMRequest(t, cfg, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	records, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("response body is not valid CSV: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("CSV response has no rows at all, not even a header")
	}
	if want := bomCSVHeader; !equalStrings(records[0], want) {
		t.Fatalf("CSV header = %v, want %v", records[0], want)
	}
	return records
}

func bomRowByID(records [][]string, itemID string) []string {
	for _, rec := range records[1:] {
		if rec[0] == itemID {
			return rec
		}
	}
	return nil
}

func TestBoMHandlerByLocationIsUnionOfNamedLocations(t *testing.T) {
	cfg := bomConfig(t, bomTestGroup)
	fx := seedBoMFixture(t, cfg.Store, bomTestGroup)

	target := "/api/v1/export/bom?location_id=" + fx.houseID + "&location_id=" + fx.garageID
	records := bomCSVRecords(t, cfg, target)

	if got, want := len(records)-1, 3; got != want {
		t.Fatalf("got %d data rows, want %d; records = %v", got, want, records)
	}

	rowA := bomRowByID(records, fx.itemA)
	if rowA == nil {
		t.Fatalf("no row for itemA; records = %v", records)
	}
	wantIdent := "serial:SN-A|barcode:000111"
	if rowA[2] != fx.houseID || rowA[3] != "House" || rowA[4] != "3" || rowA[5] != "1200" || rowA[6] != wantIdent {
		t.Errorf("itemA row = %v, want location %q/House, quantity 3, value_minor 1200, identifications %q", rowA, fx.houseID, wantIdent)
	}

	rowB := bomRowByID(records, fx.itemB)
	if rowB == nil {
		t.Fatalf("no row for itemB; records = %v", records)
	}
	if rowB[2] != fx.garageID || rowB[3] != "Garage" || rowB[4] != "2" || rowB[5] != "800" || rowB[6] != "" {
		t.Errorf("itemB row = %v, want location %q/Garage, quantity 2, value_minor 800, no identifications", rowB, fx.garageID)
	}

	rowC := bomRowByID(records, fx.itemC)
	if rowC == nil {
		t.Fatalf("no row for itemC; records = %v", records)
	}
	wantEscaped := `other:weird\:value\|with\|pipes`
	if rowC[5] != "0" || rowC[6] != wantEscaped {
		t.Errorf("itemC row = %v, want value_minor 0, identifications %q (escaped ':'/'|')", rowC, wantEscaped)
	}

	if rowA[7] != "1200" {
		t.Errorf("itemA running_total_minor = %q, want %q", rowA[7], "1200")
	}
	if rowB[7] != "2000" {
		t.Errorf("itemB running_total_minor = %q, want %q", rowB[7], "2000")
	}
	if rowC[7] != "2000" {
		t.Errorf("itemC running_total_minor = %q, want %q", rowC[7], "2000")
	}
}

func TestBoMHandlerByLabelIsUnionNotIntersection(t *testing.T) {
	cfg := bomConfig(t, bomTestGroup)
	fx := seedBoMFixture(t, cfg.Store, bomTestGroup)

	target := "/api/v1/export/bom?label_id=" + fx.labelCamping + "&label_id=" + fx.labelTools
	records := bomCSVRecords(t, cfg, target)

	if got, want := len(records)-1, 3; got != want {
		t.Fatalf("got %d data rows, want %d; records = %v", got, want, records)
	}
	for _, id := range []string{fx.itemA, fx.itemB, fx.itemD} {
		if bomRowByID(records, id) == nil {
			t.Errorf("no row for item %q; records = %v", id, records)
		}
	}
	for _, id := range []string{fx.itemC, fx.itemE} {
		if bomRowByID(records, id) != nil {
			t.Errorf("unexpected row for item %q (carries neither label); records = %v", id, records)
		}
	}

	rowD := bomRowByID(records, fx.itemD)
	if rowD[7] != "2300" {
		t.Errorf("itemD running_total_minor = %q, want %q", rowD[7], "2300")
	}
}

func TestBoMHandlerByExplicitItemIDsDropsUnknownIDsSilently(t *testing.T) {
	cfg := bomConfig(t, bomTestGroup)
	fx := seedBoMFixture(t, cfg.Store, bomTestGroup)

	target := "/api/v1/export/bom?item_id=" + fx.itemE + "&item_id=" + bomTestGroup + "-does-not-exist"
	records := bomCSVRecords(t, cfg, target)

	if got, want := len(records)-1, 1; got != want {
		t.Fatalf("got %d data rows, want %d (the unknown id must be silently dropped, not error); records = %v", got, want, records)
	}
	rowE := bomRowByID(records, fx.itemE)
	if rowE == nil {
		t.Fatalf("no row for itemE; records = %v", records)
	}
	if rowE[2] != "" || rowE[3] != "" || rowE[4] != "4" || rowE[5] != "700" || rowE[7] != "700" {
		t.Errorf("itemE row = %v, want no location, quantity 4, value_minor 700, running_total_minor 700", rowE)
	}
}

func TestBoMHandlerEmptySelectionReturnsHeaderOnly(t *testing.T) {
	cfg := bomConfig(t, bomTestGroup)
	seedBoMFixture(t, cfg.Store, bomTestGroup)

	records := bomCSVRecords(t, cfg, "/api/v1/export/bom")
	if got, want := len(records)-1, 0; got != want {
		t.Fatalf("got %d data rows for an empty selection, want %d (BoM is opt-in, never \"every item\"); records = %v", got, want, records)
	}
}

func TestBoMHandlerRejectsMultipleSelectionKinds(t *testing.T) {
	cfg := bomConfig(t, bomTestGroup)
	fx := seedBoMFixture(t, cfg.Store, bomTestGroup)

	cases := []string{
		"/api/v1/export/bom?location_id=" + fx.houseID + "&label_id=" + fx.labelCamping,
		"/api/v1/export/bom?location_id=" + fx.houseID + "&item_id=" + fx.itemA,
		"/api/v1/export/bom?label_id=" + fx.labelCamping + "&item_id=" + fx.itemA,
		"/api/v1/export/bom?location_id=" + fx.houseID + "&label_id=" + fx.labelCamping + "&item_id=" + fx.itemA,
	}
	for _, target := range cases {
		t.Run(target, func(t *testing.T) {
			rec := doBoMRequest(t, cfg, target)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != problem.MediaType {
				t.Errorf("Content-Type = %q, want %q", got, problem.MediaType)
			}
			var body problem.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not valid JSON (%v): %s", err, rec.Body.String())
			}
			if body.Status != http.StatusBadRequest {
				t.Errorf("problem.status = %d, want %d", body.Status, http.StatusBadRequest)
			}
			for _, name := range []string{"location_id", "label_id", "item_id"} {
				if !strings.Contains(body.Detail, name) {
					t.Errorf("problem.detail = %q, want it to name %q", body.Detail, name)
				}
			}
		})
	}
}

func TestBoMHandlerIgnoresBlankRepeatedValues(t *testing.T) {
	cfg := bomConfig(t, bomTestGroup)
	seedBoMFixture(t, cfg.Store, bomTestGroup)

	records := bomCSVRecords(t, cfg, "/api/v1/export/bom?location_id=&label_id=&item_id=")
	if got, want := len(records)-1, 0; got != want {
		t.Fatalf("got %d data rows, want %d (every value was blank, so no mode is selected); records = %v", got, want, records)
	}
}

func TestBoMHandlerCSVHeaders(t *testing.T) {
	cfg := bomConfig(t, bomTestGroup)
	fx := seedBoMFixture(t, cfg.Store, bomTestGroup)

	rec := doBoMRequest(t, cfg, "/api/v1/export/bom?item_id="+fx.itemE)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != exportCSVContentType {
		t.Errorf("Content-Type = %q, want %q", got, exportCSVContentType)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "bom") || !strings.Contains(got, ".csv") {
		t.Errorf("Content-Disposition = %q, want an attachment naming a bom .csv file", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "private, no-store")
	}
}

func TestBoMHandlerRejectsUnauthenticated(t *testing.T) {
	cfg := testConfig()
	rec := doBoMRequest(t, cfg, "/api/v1/export/bom")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}
