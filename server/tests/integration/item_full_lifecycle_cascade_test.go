package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
)

func itemCustomFieldCreateBodyWithDef(name, fieldDefID, textValue string) []byte {
	b, _ := json.Marshal(map[string]any{
		"name":         name,
		"field_type":   "text",
		"field_def_id": fieldDefID,
		"text_value":   textValue,
	})
	return b
}

func TestItemFullLifecycleCreateAttachAdjustDeleteCascade(t *testing.T) {
	srv := newLiveServer(t, false)

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", "alice-correct-horse-battery-9"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", "alice-correct-horse-battery-9"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody("Cordless Drill"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var item itemResponse
	mustDecode(t, rec, &item)
	if item.ID == "" {
		t.Fatal("create item: empty id")
	}
	itemPath := "/api/v1/items/" + item.ID

	if rec := srv.do(t, http.MethodPost, itemPath+"/warranty", cookie, warrantyCreateBody("Acme Tools")); rec.Code != http.StatusCreated {
		t.Fatalf("create warranty: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if rec := srv.do(t, http.MethodPost, itemPath+"/sale", cookie, saleCreateBody("Bob's Hardware")); rec.Code != http.StatusCreated {
		t.Fatalf("create sale: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if rec := srv.do(t, http.MethodPost, itemPath+"/purchase", cookie, purchaseCreateBody("Acme Supply Co")); rec.Code != http.StatusCreated {
		t.Fatalf("create purchase: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodPost, itemPath+"/identifications", cookie, identificationCreateBody("serial", "SN-00042"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create identification: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var ident identificationResponse
	mustDecode(t, rec, &ident)
	identPath := itemPath + "/identifications/" + ident.ID

	rec = srv.do(t, http.MethodPost, "/api/v1/custom-field-defs", cookie, customFieldDefCreateBody("Color", "text", 1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create custom-field def: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var fieldDef customFieldDefResponse
	mustDecode(t, rec, &fieldDef)

	rec = srv.do(t, http.MethodPost, itemPath+"/custom-fields", cookie, itemCustomFieldCreateBodyWithDef("Color", fieldDef.ID, "Yellow"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item custom field: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var cf itemCustomFieldResponse
	mustDecode(t, rec, &cf)
	cfPath := itemPath + "/custom-fields/" + cf.ID

	rec = srv.do(t, http.MethodPost, itemPath+"/stock-adjustments", cookie, stockAdjustmentCreateBody(5, "restock", "found in garage"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create stock adjustment: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var adj stockAdjustmentResponse
	mustDecode(t, rec, &adj)
	if adj.ResultingQuantity != 5 {
		t.Fatalf("stock adjustment resulting_quantity = %d, want 5", adj.ResultingQuantity)
	}

	for _, path := range []string{itemPath + "/warranty", itemPath + "/sale", itemPath + "/purchase"} {
		if rec := srv.do(t, http.MethodGet, path, cookie, nil); rec.Code != http.StatusOK {
			t.Fatalf("pre-delete GET %s: status = %d, want 200: %s", path, rec.Code, rec.Body.String())
		}
	}

	rec = srv.do(t, http.MethodDelete, itemPath, cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete item: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	if rec := srv.do(t, http.MethodGet, itemPath, cookie, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("GET item after delete: status = %d, want 404", rec.Code)
	}

	for _, tc := range []struct{ name, path string }{
		{"warranty", itemPath + "/warranty"},
		{"sale", itemPath + "/sale"},
		{"purchase", itemPath + "/purchase"},
	} {
		if rec := srv.do(t, http.MethodGet, tc.path, cookie, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s after item delete: status = %d, want 404 -- the cascade did not tombstone it", tc.path, rec.Code)
		}
	}

	if rec := srv.do(t, http.MethodPut, identPath, cookie, identificationUpdateBody("serial", "SN-00042-EDIT", ident.Version)); rec.Code != http.StatusNotFound {
		t.Errorf("PUT %s after item delete: status = %d, want 404 -- the cascade did not tombstone the identification", identPath, rec.Code)
	}

	if rec := srv.do(t, http.MethodPut, cfPath, cookie, itemCustomFieldUpdateBody("Color", "Red", cf.Version)); rec.Code != http.StatusNotFound {
		t.Errorf("PUT %s after item delete: status = %d, want 404 -- the cascade did not tombstone the item custom field", cfPath, rec.Code)
	}

	if rec := srv.do(t, http.MethodGet, itemPath+"/stock-adjustments", cookie, nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET %s after item delete: status = %d, want 404 (parent pre-check, A107's documented non-cascade)", itemPath+"/stock-adjustments", rec.Code)
	}
	conn, err := sql.Open("sqlite", "file:"+srv.dbPath)
	if err != nil {
		t.Fatalf("open independent connection to %s: %v", srv.dbPath, err)
	}
	defer func() { _ = conn.Close() }()
	var deletedAt any
	if err := conn.QueryRow(`SELECT deleted_at FROM stock_adjustments WHERE id = ?`, adj.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("query stock_adjustments.deleted_at for %s: %v", adj.ID, err)
	}
	if deletedAt != nil {
		t.Errorf("stock_adjustments row %s deleted_at = %v after item delete, want NULL -- A107's append-only history must not be cascaded", adj.ID, deletedAt)
	}
}
