package integration

import (
	"net/http"
	"strconv"
	"testing"
)

func TestPutItemsSetsQuantityWholesaleWithoutAnAdjustmentHistoryRow(t *testing.T) {
	srv := newLiveServer(t, false)

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", "alice-correct-horse-battery-105"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", "alice-correct-horse-battery-105"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody("Box of Screws"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var item itemResponse
	mustDecode(t, rec, &item)
	itemPath := "/api/v1/items/" + item.ID

	rec = srv.do(t, http.MethodPut, itemPath, cookie, itemUpdateBody("Box of Screws", item.Version))
	if rec.Code != http.StatusOK {
		t.Fatalf("first PUT (no quantity change): status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var afterFirstPut itemResponse
	mustDecode(t, rec, &afterFirstPut)

	quantityPutBody := []byte(`{"name":"Box of Screws","quantity":40,"version":` +
		strconv.FormatInt(afterFirstPut.Version, 10) + `}`)
	rec = srv.do(t, http.MethodPut, itemPath, cookie, quantityPutBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT quantity=40: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var afterQuantityPut itemResponse
	mustDecode(t, rec, &afterQuantityPut)
	if afterQuantityPut.Quantity != 40 {
		t.Fatalf("item quantity after PUT = %d, want 40 -- PUT /items must still set quantity wholesale (A105's own precondition)", afterQuantityPut.Quantity)
	}

	rec = srv.do(t, http.MethodGet, itemPath+"/stock-adjustments", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list stock adjustments: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var list stockAdjustmentListResponse
	mustDecode(t, rec, &list)
	if len(list.StockAdjustments) != 0 {
		t.Fatalf("stock_adjustments after two quantity-changing PUTs = %d rows, want 0 -- A105 says PUT /items bypasses the adjustment history; if this now fails, either A105's decision changed (update this test's own doc to match, per T064) or PUT started writing an adjustment row as a side effect nobody decided on", len(list.StockAdjustments))
	}

	rec = srv.do(t, http.MethodPost, itemPath+"/stock-adjustments", cookie, stockAdjustmentCreateBody(10, "restock", "via adjustment route"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create stock adjustment: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var adj stockAdjustmentResponse
	mustDecode(t, rec, &adj)
	if adj.ResultingQuantity != 50 {
		t.Fatalf("resulting_quantity after +10 adjustment on quantity=40 = %d, want 50", adj.ResultingQuantity)
	}

	rec = srv.do(t, http.MethodGet, itemPath+"/stock-adjustments", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list stock adjustments (after adjustment route): status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	mustDecode(t, rec, &list)
	if len(list.StockAdjustments) != 1 {
		t.Fatalf("stock_adjustments after one POST .../stock-adjustments = %d rows, want exactly 1 -- the adjustment route's own history is what stays complete under A105", len(list.StockAdjustments))
	}
}
