package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const reportsTestGroup = "grp-reports"

func reportsTestStorage(t *testing.T) *storage.Storage {
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
	return s
}

type reportsFixture struct {
	houseID string

	houseItemID string
	looseItemID string

	warrantyTodayID     string
	warrantyTodayDate   string
	warrantyInWindowID  string
	warrantyOutsideID   string
	warrantyExpiresOn   string
	warrantyOutsideDate string

	purchaseInRangeID  string
	purchaseOutRangeID string
	rangeFrom, rangeTo string

	labelFragileID, labelValuableID string
	twoLabelsItemID                 string
}

const reportsWithinDays = 5

func seedReportsFixture(t *testing.T, s *storage.Storage, groupID string) reportsFixture {
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

	house, err := scope.Locations().Create(t.Context(), storage.CreateLocationParams{ID: id("loc-house"), Name: "House", Now: 1})
	if err != nil {
		t.Fatalf("create House: %v", err)
	}

	houseItem, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: id("item-house"), Name: "Umbrella Stand", LocationID: house.ID, ShortCode: id("SC-HOUSE"), Now: 1})
	if err != nil {
		t.Fatalf("create house item: %v", err)
	}
	if _, err := scope.Purchase().Create(t.Context(), storage.CreatePurchaseParams{ID: id("purchase-house"), ItemID: houseItem.ID, PurchasedOn: "2026-01-15", PurchasePriceMinor: 1500, Now: 1}); err != nil {
		t.Fatalf("create house item purchase: %v", err)
	}

	looseItem, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: id("item-loose"), Name: "Loose Screw", ShortCode: id("SC-LOOSE"), Now: 1})
	if err != nil {
		t.Fatalf("create loose item: %v", err)
	}

	today := time.Now()
	warrantyToday, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: id("item-warranty-today"), Name: "Fire Extinguisher", ShortCode: id("SC-WARR-TODAY"), Now: 1})
	if err != nil {
		t.Fatalf("create warranty-today item: %v", err)
	}
	todayExpires := today.Format("2006-01-02")
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{ID: id("warranty-today"), ItemID: warrantyToday.ID, ExpiresOn: todayExpires, Now: 1}); err != nil {
		t.Fatalf("create warranty-today: %v", err)
	}

	warrantyInWindow, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: id("item-warranty-in"), Name: "Fridge", ShortCode: id("SC-WARR-IN"), Now: 1})
	if err != nil {
		t.Fatalf("create warranty-in-window item: %v", err)
	}
	inWindowExpires := today.AddDate(0, 0, reportsWithinDays).Format("2006-01-02")
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{ID: id("warranty-in"), ItemID: warrantyInWindow.ID, ExpiresOn: inWindowExpires, Now: 1}); err != nil {
		t.Fatalf("create warranty-in-window: %v", err)
	}

	warrantyOutside, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: id("item-warranty-out"), Name: "Dishwasher", ShortCode: id("SC-WARR-OUT"), Now: 1})
	if err != nil {
		t.Fatalf("create warranty-outside item: %v", err)
	}
	outsideExpires := today.AddDate(0, 0, reportsWithinDays+1).Format("2006-01-02")
	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{ID: id("warranty-out"), ItemID: warrantyOutside.ID, ExpiresOn: outsideExpires, Now: 1}); err != nil {
		t.Fatalf("create warranty-outside: %v", err)
	}

	const rangeFrom, rangeTo = "2026-03-01", "2026-03-10"
	purchaseIn, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: id("item-purchase-in"), Name: "Kettle", ShortCode: id("SC-PUR-IN"), Now: 1})
	if err != nil {
		t.Fatalf("create purchase-in-range item: %v", err)
	}
	if _, err := scope.Purchase().Create(t.Context(), storage.CreatePurchaseParams{ID: id("purchase-in"), ItemID: purchaseIn.ID, PurchasedOn: "2026-03-05", PurchasePriceMinor: 250, Now: 1}); err != nil {
		t.Fatalf("create purchase-in-range: %v", err)
	}
	purchaseOut, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: id("item-purchase-out"), Name: "Mixer", ShortCode: id("SC-PUR-OUT"), Now: 1})
	if err != nil {
		t.Fatalf("create purchase-out-of-range item: %v", err)
	}
	if _, err := scope.Purchase().Create(t.Context(), storage.CreatePurchaseParams{ID: id("purchase-out"), ItemID: purchaseOut.ID, PurchasedOn: "2026-04-01", PurchasePriceMinor: 999, Now: 1}); err != nil {
		t.Fatalf("create purchase-out-of-range: %v", err)
	}

	fragile, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{ID: id("label-fragile"), Name: "Fragile", Color: "#ff0000", Now: 1})
	if err != nil {
		t.Fatalf("create Fragile label: %v", err)
	}
	valuable, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{ID: id("label-valuable"), Name: "Valuable", Color: "#00ff00", Now: 1})
	if err != nil {
		t.Fatalf("create Valuable label: %v", err)
	}
	twoLabelsItem, err := scope.Items().Create(t.Context(), storage.CreateItemParams{ID: id("item-two-labels"), Name: "Vase", ShortCode: id("SC-TWO-LABELS"), Now: 1})
	if err != nil {
		t.Fatalf("create two-labels item: %v", err)
	}
	if _, err := scope.Purchase().Create(t.Context(), storage.CreatePurchaseParams{ID: id("purchase-two-labels"), ItemID: twoLabelsItem.ID, PurchasedOn: "2026-01-10", PurchasePriceMinor: 500, Now: 1}); err != nil {
		t.Fatalf("create two-labels item purchase: %v", err)
	}
	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{ID: id("edge-fragile"), ItemID: twoLabelsItem.ID, LabelID: fragile.ID, Now: 1}); err != nil {
		t.Fatalf("attach fragile to two-labels item: %v", err)
	}
	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{ID: id("edge-valuable"), ItemID: twoLabelsItem.ID, LabelID: valuable.ID, Now: 1}); err != nil {
		t.Fatalf("attach valuable to two-labels item: %v", err)
	}

	return reportsFixture{
		houseID:             house.ID,
		houseItemID:         houseItem.ID,
		looseItemID:         looseItem.ID,
		warrantyTodayID:     warrantyToday.ID,
		warrantyTodayDate:   todayExpires,
		warrantyInWindowID:  warrantyInWindow.ID,
		warrantyOutsideID:   warrantyOutside.ID,
		warrantyExpiresOn:   inWindowExpires,
		warrantyOutsideDate: outsideExpires,
		purchaseInRangeID:   purchaseIn.ID,
		purchaseOutRangeID:  purchaseOut.ID,
		rangeFrom:           rangeFrom,
		rangeTo:             rangeTo,
		labelFragileID:      fragile.ID,
		labelValuableID:     valuable.ID,
		twoLabelsItemID:     twoLabelsItem.ID,
	}
}

func reportsConfig(t *testing.T, groupID string) Config {
	t.Helper()
	s := reportsTestStorage(t)
	cfg := testConfig()
	cfg.Authenticator = middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
		return middleware.Identity{Group: groupID, UserID: groupID + "-owner", Role: "owner"}, nil
	})
	cfg.Store = s
	cfg.Scopes = s
	return cfg
}

func doReportsRequest(t *testing.T, cfg Config, target string) *httptest.ResponseRecorder {
	t.Helper()
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestReportsValuationHandlerByLocation(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/valuation?group_by=location")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body reportValuationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON (%v): %s", err, rec.Body.String())
	}

	byKey := make(map[string]reportValuationRow, len(body.Rows))
	for _, row := range body.Rows {
		byKey[row.GroupKey] = row
	}

	house, ok := byKey[fx.houseID]
	if !ok {
		t.Fatalf("no row for House (%q); rows = %+v", fx.houseID, body.Rows)
	}
	if house.ItemCount != 1 || house.TotalValueMinor != 1500 {
		t.Errorf("House row = %+v, want ItemCount 1, TotalValueMinor 1500", house)
	}

	unassigned, ok := byKey[""]
	if !ok {
		t.Fatalf("no Unassigned row; rows = %+v", body.Rows)
	}
	if unassigned.ItemCount != 7 || unassigned.TotalValueMinor != 250+999+500 {
		t.Errorf("Unassigned row = %+v, want ItemCount 7, TotalValueMinor %d", unassigned, 250+999+500)
	}
}

func TestReportsValuationHandlerByLabel(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/valuation?group_by=label")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body reportValuationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON (%v): %s", err, rec.Body.String())
	}
	if len(body.Rows) != 2 {
		t.Fatalf("group_by=label returned %d rows, want 2 (Fragile, Valuable); rows = %+v", len(body.Rows), body.Rows)
	}

	byKey := make(map[string]reportValuationRow, len(body.Rows))
	for _, row := range body.Rows {
		byKey[row.GroupKey] = row
	}

	fragile, ok := byKey[fx.labelFragileID]
	if !ok {
		t.Fatalf("no row for Fragile (%q); rows = %+v", fx.labelFragileID, body.Rows)
	}
	if fragile.ItemCount != 1 || fragile.TotalValueMinor != 500 || fragile.GroupLabel != "Fragile" {
		t.Errorf("Fragile row = %+v, want ItemCount 1, TotalValueMinor 500, GroupLabel Fragile", fragile)
	}

	valuable, ok := byKey[fx.labelValuableID]
	if !ok {
		t.Fatalf("no row for Valuable (%q); rows = %+v", fx.labelValuableID, body.Rows)
	}
	if valuable.ItemCount != 1 || valuable.TotalValueMinor != 500 || valuable.GroupLabel != "Valuable" {
		t.Errorf("Valuable row = %+v, want ItemCount 1, TotalValueMinor 500, GroupLabel Valuable", valuable)
	}
}

func TestReportsValuationHandlerCSV(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/valuation?group_by=location&format=csv")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != exportCSVContentType {
		t.Errorf("Content-Type = %q, want %q", got, exportCSVContentType)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "valuation") || !strings.Contains(got, ".csv") {
		t.Errorf("Content-Disposition = %q, want an attachment naming a valuation .csv file", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}

	records, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("response body is not valid CSV: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("CSV response has no rows at all, not even a header")
	}
	if want := []string{"group_key", "group_label", "item_count", "total_value_minor"}; !equalStrings(records[0], want) {
		t.Errorf("CSV header = %v, want %v", records[0], want)
	}

	var houseRow []string
	for _, rec := range records[1:] {
		if rec[0] == fx.houseID {
			houseRow = rec
		}
	}
	if houseRow == nil {
		t.Fatalf("no CSV row for House (%q); records = %v", fx.houseID, records)
	}
	if houseRow[2] != "1" || houseRow[3] != "1500" {
		t.Errorf("House CSV row = %v, want item_count 1, total_value_minor 1500", houseRow)
	}
}

func TestReportsValuationHandlerRequiresGroupBy(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/valuation")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if !strings.Contains(p.Detail, "group_by") {
		p2 := p
		t.Errorf("problem detail = %q, want it to mention group_by; problem = %+v", p.Detail, p2)
	}
}

func TestReportsValuationHandlerRejectsInvalidGroupBy(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/valuation?group_by=nonsense")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestReportsHandlerRejectsUnknownFormat(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/item-count-by-location?format=xml")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestReportsWarrantyExpiringHandlerDefaultWindow(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/warranty-expiring")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body reportWarrantyExpiringResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON (%v): %s", err, rec.Body.String())
	}

	var found *reportWarrantyExpiringRow
	for i := range body.Rows {
		if body.Rows[i].ItemID == fx.warrantyInWindowID {
			found = &body.Rows[i]
		}
	}
	if found == nil {
		t.Fatalf("default within_days=30 window did not include the in-window warranty; rows = %+v", body.Rows)
	}
	if found.ExpiresOn != fx.warrantyExpiresOn {
		t.Errorf("ExpiresOn = %q, want %q", found.ExpiresOn, fx.warrantyExpiresOn)
	}
	if found.DaysRemaining != reportsWithinDays {
		t.Errorf("DaysRemaining = %d, want %d", found.DaysRemaining, reportsWithinDays)
	}
}

func TestReportsWarrantyExpiringHandlerNarrowWindowExcludesTheFarWarranty(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/warranty-expiring?within_days="+strconv.Itoa(reportsWithinDays))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body reportWarrantyExpiringResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON (%v): %s", err, rec.Body.String())
	}

	var sawIn, sawOut bool
	for _, row := range body.Rows {
		if row.ItemID == fx.warrantyInWindowID {
			sawIn = true
		}
		if row.ItemID == fx.warrantyOutsideID {
			sawOut = true
		}
	}
	if !sawIn {
		t.Errorf("within_days=%d window did not include the in-window warranty; rows = %+v", reportsWithinDays, body.Rows)
	}
	if sawOut {
		t.Errorf("within_days=%d window incorrectly included the out-of-window warranty (expires %s); rows = %+v", reportsWithinDays, fx.warrantyOutsideDate, body.Rows)
	}
}

func TestReportsWarrantyExpiringHandlerCSV(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/warranty-expiring?within_days="+strconv.Itoa(reportsWithinDays)+"&format=csv")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != exportCSVContentType {
		t.Errorf("Content-Type = %q, want %q", got, exportCSVContentType)
	}

	records, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("response body is not valid CSV: %v", err)
	}
	if want := []string{"item_id", "item_name", "expires_on", "days_remaining"}; !equalStrings(records[0], want) {
		t.Errorf("CSV header = %v, want %v", records[0], want)
	}

	var inRow []string
	for _, rec := range records[1:] {
		if rec[0] == fx.warrantyInWindowID {
			inRow = rec
		}
		if rec[0] == fx.warrantyOutsideID {
			t.Errorf("CSV incorrectly included the out-of-window warranty; records = %v", records)
		}
	}
	if inRow == nil {
		t.Fatalf("no CSV row for the in-window warranty (%q); records = %v", fx.warrantyInWindowID, records)
	}
	if inRow[2] != fx.warrantyExpiresOn || inRow[3] != strconv.Itoa(reportsWithinDays) {
		t.Errorf("CSV row = %v, want expires_on %q, days_remaining %d", inRow, fx.warrantyExpiresOn, reportsWithinDays)
	}
}

func TestReportsWarrantyExpiringHandlerZeroWithinDaysMeansTodayOnly(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/warranty-expiring?within_days=0")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body reportWarrantyExpiringResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON (%v): %s", err, rec.Body.String())
	}

	if len(body.Rows) != 1 {
		t.Fatalf("within_days=0 returned %d rows, want 1 (today's own warranty only); rows = %+v", len(body.Rows), body.Rows)
	}
	got := body.Rows[0]
	if got.ItemID != fx.warrantyTodayID || got.ExpiresOn != fx.warrantyTodayDate || got.DaysRemaining != 0 {
		t.Errorf("row = %+v, want item %q expiring %q at 0 days remaining", got, fx.warrantyTodayID, fx.warrantyTodayDate)
	}
	for _, row := range body.Rows {
		if row.ItemID == fx.warrantyInWindowID || row.ItemID == fx.warrantyOutsideID {
			t.Errorf("within_days=0 incorrectly included a non-today warranty: %+v", row)
		}
	}
}

func TestReportsWarrantyExpiringHandlerRejectsUnparsableWithinDays(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/warranty-expiring?within_days=abc")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestReportsWarrantyExpiringHandlerRejectsNegativeWithinDays(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/warranty-expiring?within_days=-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestReportsPurchasesHandler(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/purchases?from="+fx.rangeFrom+"&to="+fx.rangeTo)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body reportPurchasesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON (%v): %s", err, rec.Body.String())
	}

	if len(body.Rows) != 1 {
		t.Fatalf("PurchasesInRange returned %d rows, want 1; rows = %+v", len(body.Rows), body.Rows)
	}
	got := body.Rows[0]
	if got.ItemID != fx.purchaseInRangeID || got.PurchasedOn != "2026-03-05" || got.PurchasePriceMinor != 250 {
		t.Errorf("row = %+v, want item %q at 2026-03-05, price 250", got, fx.purchaseInRangeID)
	}
	if got.ItemID == fx.purchaseOutRangeID {
		t.Errorf("PurchasesInRange incorrectly included the out-of-range purchase %q", fx.purchaseOutRangeID)
	}
}

func TestReportsPurchasesHandlerCSV(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/purchases?from="+fx.rangeFrom+"&to="+fx.rangeTo+"&format=csv")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	records, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("response body is not valid CSV: %v", err)
	}
	if want := []string{"item_id", "item_name", "purchased_on", "vendor", "purchase_price_minor"}; !equalStrings(records[0], want) {
		t.Errorf("CSV header = %v, want %v", records[0], want)
	}
	if len(records) != 2 {
		t.Fatalf("CSV has %d records (incl. header), want 2 (one in-range purchase); records = %v", len(records), records)
	}
	if records[1][0] != fx.purchaseInRangeID || records[1][2] != "2026-03-05" || records[1][4] != "250" {
		t.Errorf("CSV data row = %v, want item %q at 2026-03-05, price 250", records[1], fx.purchaseInRangeID)
	}
}

func TestReportsPurchasesHandlerRequiresFromAndTo(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	for _, tc := range []struct {
		name   string
		target string
	}{
		{"both missing", "/api/v1/reports/purchases"},
		{"to missing", "/api/v1/reports/purchases?from=" + fx.rangeFrom},
		{"from missing", "/api/v1/reports/purchases?to=" + fx.rangeTo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doReportsRequest(t, cfg, tc.target)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestReportsPurchasesHandlerRejectsMalformedRange(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/purchases?from=not-a-date&to=2026-03-10")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestReportsPurchasesHandlerRejectsInvertedRange(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/purchases?from="+fx.rangeTo+"&to="+fx.rangeFrom)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestReportsItemCountByLocationHandler(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/item-count-by-location")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body reportItemCountByLocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON (%v): %s", err, rec.Body.String())
	}

	if len(body.Rows) != 1 {
		t.Fatalf("ItemCountByLocation returned %d rows, want 1 (House only); rows = %+v", len(body.Rows), body.Rows)
	}
	house := body.Rows[0]
	if house.LocationID != fx.houseID || house.ItemCount != 1 {
		t.Errorf("row = %+v, want House (%q) at item_count 1", house, fx.houseID)
	}
}

func TestReportsItemCountByLocationHandlerCSV(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	fx := seedReportsFixture(t, cfg.Store, reportsTestGroup)

	rec := doReportsRequest(t, cfg, "/api/v1/reports/item-count-by-location?format=csv")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	records, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("response body is not valid CSV: %v", err)
	}
	if want := []string{"location_id", "location_name", "item_count"}; !equalStrings(records[0], want) {
		t.Errorf("CSV header = %v, want %v", records[0], want)
	}
	if len(records) != 2 {
		t.Fatalf("CSV has %d records (incl. header), want 2; records = %v", len(records), records)
	}
	if records[1][0] != fx.houseID || records[1][1] != "House" || records[1][2] != "1" {
		t.Errorf("CSV data row = %v, want [%q House 1]", records[1], fx.houseID)
	}
}

func TestReportsHandlerRejectsUnauthenticated(t *testing.T) {
	cfg := reportsConfig(t, reportsTestGroup)
	seedReportsFixture(t, cfg.Store, reportsTestGroup)
	cfg.Authenticator = middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
		return middleware.Identity{}, middleware.ErrUnauthenticated
	})

	for _, path := range []string{
		"/api/v1/reports/valuation?group_by=location",
		"/api/v1/reports/warranty-expiring",
		"/api/v1/reports/purchases?from=2026-01-01&to=2026-01-31",
		"/api/v1/reports/item-count-by-location",
	} {
		rec := doReportsRequest(t, cfg, path)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d; body = %s", path, rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
	}
}
