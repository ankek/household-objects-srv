package integration

import (
	"net/http"
	"testing"
)

func TestSameTenantCrossItemIsolation(t *testing.T) {
	srv := newLiveServer(t, false)

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", "alice-correct-horse-battery-x1"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", "alice-correct-horse-battery-x1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	createItem := func(name string) itemResponse {
		t.Helper()
		rec := srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody(name))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create item %q: status = %d, want 201: %s", name, rec.Code, rec.Body.String())
		}
		var it itemResponse
		mustDecode(t, rec, &it)
		return it
	}
	item1 := createItem("Item One")
	item2 := createItem("Item Two")
	path1 := "/api/v1/items/" + item1.ID
	path2 := "/api/v1/items/" + item2.ID

	t.Run("warranty read isolation", func(t *testing.T) {
		if rec := srv.do(t, http.MethodPost, path1+"/warranty", cookie, warrantyCreateBody("Item1Holder")); rec.Code != http.StatusCreated {
			t.Fatalf("create item1 warranty: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
		if rec := srv.do(t, http.MethodPost, path2+"/warranty", cookie, warrantyCreateBody("Item2Holder")); rec.Code != http.StatusCreated {
			t.Fatalf("create item2 warranty: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}

		rec := srv.do(t, http.MethodGet, path1+"/warranty", cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("get item1 warranty: status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var w1 warrantyResponse
		mustDecode(t, rec, &w1)
		if w1.Holder != "Item1Holder" {
			t.Errorf("item1's warranty holder = %q, want %q -- item2's row leaked in", w1.Holder, "Item1Holder")
		}

		rec = srv.do(t, http.MethodGet, path2+"/warranty", cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("get item2 warranty: status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var w2 warrantyResponse
		mustDecode(t, rec, &w2)
		if w2.Holder != "Item2Holder" {
			t.Errorf("item2's warranty holder = %q, want %q -- item1's row leaked in", w2.Holder, "Item2Holder")
		}
	})

	t.Run("sale read isolation", func(t *testing.T) {
		if rec := srv.do(t, http.MethodPost, path1+"/sale", cookie, saleCreateBody("Item1Buyer")); rec.Code != http.StatusCreated {
			t.Fatalf("create item1 sale: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
		if rec := srv.do(t, http.MethodPost, path2+"/sale", cookie, saleCreateBody("Item2Buyer")); rec.Code != http.StatusCreated {
			t.Fatalf("create item2 sale: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}

		rec := srv.do(t, http.MethodGet, path1+"/sale", cookie, nil)
		var s1 saleResponse
		mustDecode(t, rec, &s1)
		if s1.BuyerName != "Item1Buyer" {
			t.Errorf("item1's sale buyer_name = %q, want %q -- item2's row leaked in", s1.BuyerName, "Item1Buyer")
		}

		rec = srv.do(t, http.MethodGet, path2+"/sale", cookie, nil)
		var s2 saleResponse
		mustDecode(t, rec, &s2)
		if s2.BuyerName != "Item2Buyer" {
			t.Errorf("item2's sale buyer_name = %q, want %q -- item1's row leaked in", s2.BuyerName, "Item2Buyer")
		}
	})

	t.Run("purchase read isolation", func(t *testing.T) {
		if rec := srv.do(t, http.MethodPost, path1+"/purchase", cookie, purchaseCreateBody("Item1Vendor")); rec.Code != http.StatusCreated {
			t.Fatalf("create item1 purchase: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
		if rec := srv.do(t, http.MethodPost, path2+"/purchase", cookie, purchaseCreateBody("Item2Vendor")); rec.Code != http.StatusCreated {
			t.Fatalf("create item2 purchase: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}

		rec := srv.do(t, http.MethodGet, path1+"/purchase", cookie, nil)
		var p1 purchaseResponse
		mustDecode(t, rec, &p1)
		if p1.Vendor != "Item1Vendor" {
			t.Errorf("item1's purchase vendor = %q, want %q -- item2's row leaked in", p1.Vendor, "Item1Vendor")
		}

		rec = srv.do(t, http.MethodGet, path2+"/purchase", cookie, nil)
		var p2 purchaseResponse
		mustDecode(t, rec, &p2)
		if p2.Vendor != "Item2Vendor" {
			t.Errorf("item2's purchase vendor = %q, want %q -- item1's row leaked in", p2.Vendor, "Item2Vendor")
		}
	})

	t.Run("identification cross-item write rejected, sibling unaffected", func(t *testing.T) {
		if rec := srv.do(t, http.MethodPost, path1+"/identifications", cookie, identificationCreateBody("serial", "ITEM1-SN")); rec.Code != http.StatusCreated {
			t.Fatalf("create item1 identification: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}

		rec := srv.do(t, http.MethodPost, path2+"/identifications", cookie, identificationCreateBody("serial", "ITEM2-SN"))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create item2 identification: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
		var ident2 identificationResponse
		mustDecode(t, rec, &ident2)

		crossPath := path1 + "/identifications/" + ident2.ID
		if rec := srv.do(t, http.MethodPut, crossPath, cookie, identificationUpdateBody("serial", "HIJACKED", ident2.Version)); rec.Code != http.StatusNotFound {
			t.Errorf("PUT %s (item1 prefix, item2's own id): status = %d, want 404 -- a same-tenant cross-item write must be refused", crossPath, rec.Code)
		}
		if rec := srv.do(t, http.MethodDelete, crossPath, cookie, nil); rec.Code != http.StatusNotFound {
			t.Errorf("DELETE %s (item1 prefix, item2's own id): status = %d, want 404 -- a same-tenant cross-item delete must be refused", crossPath, rec.Code)
		}

		rec = srv.do(t, http.MethodGet, path2+"/identifications", cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list item2 identifications: status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var list identificationListResponse
		mustDecode(t, rec, &list)
		if len(list.Identifications) != 1 {
			t.Fatalf("item2 identifications after cross-item probes = %d, want 1", len(list.Identifications))
		}
		if got := list.Identifications[0]; got.Value != "ITEM2-SN" || got.Version != ident2.Version {
			t.Errorf("item2's identification after cross-item probes = %+v, want value=ITEM2-SN version=%d unchanged", got, ident2.Version)
		}
	})

	t.Run("item custom field cross-item write rejected, sibling unaffected", func(t *testing.T) {
		if rec := srv.do(t, http.MethodPost, path1+"/custom-fields", cookie, itemCustomFieldCreateBody("Color", "Item1Value")); rec.Code != http.StatusCreated {
			t.Fatalf("create item1 custom field: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}

		rec := srv.do(t, http.MethodPost, path2+"/custom-fields", cookie, itemCustomFieldCreateBody("Color", "Item2Value"))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create item2 custom field: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
		var cf2 itemCustomFieldResponse
		mustDecode(t, rec, &cf2)

		crossPath := path1 + "/custom-fields/" + cf2.ID
		if rec := srv.do(t, http.MethodPut, crossPath, cookie, itemCustomFieldUpdateBody("Color", "HIJACKED", cf2.Version)); rec.Code != http.StatusNotFound {
			t.Errorf("PUT %s (item1 prefix, item2's own id): status = %d, want 404 -- a same-tenant cross-item write must be refused", crossPath, rec.Code)
		}
		if rec := srv.do(t, http.MethodDelete, crossPath, cookie, nil); rec.Code != http.StatusNotFound {
			t.Errorf("DELETE %s (item1 prefix, item2's own id): status = %d, want 404 -- a same-tenant cross-item delete must be refused", crossPath, rec.Code)
		}

		rec = srv.do(t, http.MethodGet, path2+"/custom-fields", cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list item2 custom fields: status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var list itemCustomFieldListResponse
		mustDecode(t, rec, &list)
		if len(list.CustomFields) != 1 {
			t.Fatalf("item2 custom fields after cross-item probes = %d, want 1", len(list.CustomFields))
		}
		if got := list.CustomFields[0]; got.TextValue == nil || *got.TextValue != "Item2Value" || got.Version != cf2.Version {
			t.Errorf("item2's custom field after cross-item probes = %+v, want text_value=Item2Value version=%d unchanged", got, cf2.Version)
		}
	})

	t.Run("stock adjustment list isolation, 3-row/2-item fixture", func(t *testing.T) {
		if rec := srv.do(t, http.MethodPost, path1+"/stock-adjustments", cookie, stockAdjustmentCreateBody(3, "restock", "item1-a")); rec.Code != http.StatusCreated {
			t.Fatalf("create item1 stock adjustment 1: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
		if rec := srv.do(t, http.MethodPost, path1+"/stock-adjustments", cookie, stockAdjustmentCreateBody(-1, "correction", "item1-b")); rec.Code != http.StatusCreated {
			t.Fatalf("create item1 stock adjustment 2: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
		if rec := srv.do(t, http.MethodPost, path2+"/stock-adjustments", cookie, stockAdjustmentCreateBody(9, "restock", "item2-a")); rec.Code != http.StatusCreated {
			t.Fatalf("create item2 stock adjustment: status = %d, want 201: %s", rec.Code, rec.Body.String())
		}

		rec := srv.do(t, http.MethodGet, path1+"/stock-adjustments", cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list item1 stock adjustments: status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var list1 stockAdjustmentListResponse
		mustDecode(t, rec, &list1)
		if len(list1.StockAdjustments) != 2 {
			t.Fatalf("item1 stock adjustments = %d, want exactly 2", len(list1.StockAdjustments))
		}
		for _, row := range list1.StockAdjustments {
			if row.Note == "item2-a" {
				t.Errorf("item1's stock-adjustment list contains item2's own row (note=%q) -- a same-tenant cross-item read leak", row.Note)
			}
		}

		rec = srv.do(t, http.MethodGet, path2+"/stock-adjustments", cookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list item2 stock adjustments: status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var list2 stockAdjustmentListResponse
		mustDecode(t, rec, &list2)
		if len(list2.StockAdjustments) != 1 {
			t.Fatalf("item2 stock adjustments = %d, want exactly 1", len(list2.StockAdjustments))
		}
		if list2.StockAdjustments[0].Note != "item2-a" {
			t.Errorf("item2's stock adjustment note = %q, want item2-a", list2.StockAdjustments[0].Note)
		}
	})
}
