package integration

import (
	"encoding/csv"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"net/http"
	"strings"
	"testing"
)

func TestExportItemsCSVEndToEndThroughRealRouterAndDatabase(t *testing.T) {
	srv := newLiveServer(t, false)

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", "alice-correct-horse-battery"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", "alice-correct-horse-battery"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/locations", cookie, locationCreateBody("Garage", ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create location Garage: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var garage locationResponse
	mustDecode(t, rec, &garage)

	rec = srv.do(t, http.MethodPost, "/api/v1/locations", cookie, locationCreateBody("Shelf 1", garage.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create location Shelf 1: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var shelf locationResponse
	mustDecode(t, rec, &shelf)

	rec = srv.do(t, http.MethodPost, "/api/v1/custom-field-defs", cookie, customFieldDefCreateBody("Color", "text", 1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create custom field def Color: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var colorDef customFieldDefResponse
	mustDecode(t, rec, &colorDef)

	rec = srv.do(t, http.MethodPost, "/api/v1/custom-field-defs", cookie, customFieldDefCreateBody("Weight (kg)", "number", 2))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create custom field def Weight (kg): status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/labels", cookie, labelCreateBody("Fragile", "#FF0000"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create label Fragile: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var fragile labelResponse
	mustDecode(t, rec, &fragile)

	rec = srv.do(t, http.MethodPost, "/api/v1/labels", cookie, labelCreateBody("Style: Modern|Chic", "#00FF00"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create label Style: Modern|Chic: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var style labelResponse
	mustDecode(t, rec, &style)

	rec = srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody("Drill, Cordless"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item1: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var item1 itemResponse
	mustDecode(t, rec, &item1)

	rec = srv.do(t, http.MethodPut, "/api/v1/items/"+item1.ID, cookie, mustJSON(t, map[string]any{
		"name": "Drill, Cordless", "description": "18V, yellow case.\nCondition: \"used\", works fine.",
		"quantity": 3, "location_id": shelf.ID, "version": 1,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("update item1 (quantity/location/description): status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	for _, labelID := range []string{fragile.ID, style.ID} {
		rec = srv.do(t, http.MethodPut, "/api/v1/items/"+item1.ID+"/labels/"+labelID, cookie, nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("attach label %s to item1: status = %d, want 204: %s", labelID, rec.Code, rec.Body.String())
		}
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item1.ID+"/identifications", cookie, identificationCreateBody("serial", "SN-123:Special|Edition"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create identification on item1: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	attBody, attContentType := attachmentUploadMultipartBody(t, "image", "drill.jpg", testImageJPEGBytes(t))
	rec = srv.doMultipart(t, http.MethodPost, "/api/v1/items/"+item1.ID+"/attachments", cookie, attBody, attContentType)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload attachment on item1: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var att attachmentResponse
	mustDecode(t, rec, &att)
	if att.SHA256 == "" {
		t.Fatal("uploaded attachment has empty SHA256; nothing to compare the export's attachments cell against")
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item1.ID+"/custom-fields", cookie, mustJSON(t, map[string]any{
		"field_def_id": colorDef.ID, "name": "Color", "field_type": "text", "text_value": "Red, Bright",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create live custom field value on item1: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item1.ID+"/custom-fields", cookie, mustJSON(t, map[string]any{
		"name": "Registered", "field_type": "boolean", "bool_value": true,
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ad hoc custom field value on item1: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item1.ID+"/warranty", cookie, mustJSON(t, map[string]any{
		"holder": "Acme, Inc.", "provider": "Acme Warranty Services",
		"starts_on": "2024-01-01", "expires_on": "2026-01-01", "is_lifetime": false,
		"notes": "Registered \"online\"\nSee receipt.",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create warranty on item1: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item1.ID+"/purchase", cookie, mustJSON(t, map[string]any{
		"vendor": "Hardware Store", "purchased_on": "2024-01-02", "purchase_price_minor": 12999,
		"order_reference": "ORD-1,A", "notes": "Bought on sale",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create purchase on item1: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody("Empty Widget"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item2: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var item2 itemResponse
	mustDecode(t, rec, &item2)

	rec = srv.do(t, http.MethodGet, "/api/v1/export/items.csv", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/csv; charset=utf-8")
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, ".csv") {
		t.Errorf("Content-Disposition = %q, want it to declare an attachment with a .csv filename", got)
	}

	records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("export response body is not valid CSV: %v; body = %q", err, rec.Body.String())
	}
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3 (header + 2 items): %v", len(records), records)
	}
	header := records[0]

	col := func(row []string, name string) string {
		t.Helper()
		for i, h := range header {
			if h == name {
				return row[i]
			}
		}
		t.Fatalf("column %q not found in header %v", name, header)
		return ""
	}
	rowByID := func(id string) []string {
		t.Helper()
		for _, r := range records[1:] {
			if col(r, importexport.ColumnID) == id {
				return r
			}
		}
		t.Fatalf("no exported row for item id %q", id)
		return nil
	}
	rowsPipeSet := func(cell string) map[string]bool {
		out := make(map[string]bool)
		for _, raw := range importexport.SplitEscaped(cell, '|') {
			out[importexport.UnescapeSubValue(raw)] = true
		}
		return out
	}

	row1 := rowByID(item1.ID)
	row2 := rowByID(item2.ID)

	for _, tc := range []struct{ col, want string }{
		{importexport.ColumnName, "Drill, Cordless"},
		{importexport.ColumnDescription, "18V, yellow case.\nCondition: \"used\", works fine."},
		{importexport.ColumnQuantity, "3"},
		{importexport.ColumnLocationPath, "Garage/Shelf 1"},
		{importexport.ColumnLocationID, shelf.ID},
		{importexport.ColumnWarrantyHolder, "Acme, Inc."},
		{importexport.ColumnWarrantyProvider, "Acme Warranty Services"},
		{importexport.ColumnWarrantyStartsOn, "2024-01-01"},
		{importexport.ColumnWarrantyExpiresOn, "2026-01-01"},
		{importexport.ColumnWarrantyIsLifetime, "false"},
		{importexport.ColumnWarrantyNotes, "Registered \"online\"\nSee receipt."},
		{importexport.ColumnPurchaseVendor, "Hardware Store"},
		{importexport.ColumnPurchasePurchasedOn, "2024-01-02"},
		{importexport.ColumnPurchasePriceMinor, "12999"},
		{importexport.ColumnPurchaseOrderReference, "ORD-1,A"},
		{importexport.ColumnPurchaseNotes, "Bought on sale"},
		{importexport.ColumnSaleBuyerName, ""},
		{"cf:Color", "Red, Bright"},
		{"cf:Weight (kg)", ""},
		{"cfx:boolean:Registered", "true"},
	} {
		if got := col(row1, tc.col); got != tc.want {
			t.Errorf("item1 column %q = %q, want %q", tc.col, got, tc.want)
		}
	}
	if got, want := rowsPipeSet(col(row1, importexport.ColumnLabels)), map[string]bool{"Fragile": true, "Style: Modern|Chic": true}; len(got) != len(want) || !got["Fragile"] || !got["Style: Modern|Chic"] {
		t.Errorf("item1 labels = %v, want %v", got, want)
	}
	idPairs := importexport.SplitEscaped(col(row1, importexport.ColumnIdentifications), '|')
	if len(idPairs) != 1 {
		t.Fatalf("item1 identification pairs = %v, want exactly 1", idPairs)
	}
	kv := importexport.SplitEscaped(idPairs[0], ':')
	if len(kv) != 2 || importexport.UnescapeSubValue(kv[0]) != "serial" || importexport.UnescapeSubValue(kv[1]) != "SN-123:Special|Edition" {
		t.Errorf("item1 identification = %v, want [serial SN-123:Special|Edition]", kv)
	}
	attTriples := importexport.SplitEscaped(col(row1, importexport.ColumnAttachments), '|')
	if len(attTriples) != 1 {
		t.Fatalf("item1 attachment triples = %v, want exactly 1", attTriples)
	}
	attFields := importexport.SplitEscaped(attTriples[0], ':')
	if len(attFields) != 3 {
		t.Fatalf("item1 attachment fields = %v, want exactly 3", attFields)
	}
	if category, filename, sha := importexport.UnescapeSubValue(attFields[0]), importexport.UnescapeSubValue(attFields[1]), importexport.UnescapeSubValue(attFields[2]); category != "image" || filename != "drill.jpg" || sha != att.SHA256 {
		t.Errorf("item1 attachment = [%q %q %q], want [image drill.jpg %q]", category, filename, sha, att.SHA256)
	}

	for _, tc := range []struct{ col, want string }{
		{importexport.ColumnName, "Empty Widget"},
		{importexport.ColumnQuantity, "0"},
		{importexport.ColumnLocationPath, ""},
		{importexport.ColumnLocationID, ""},
		{importexport.ColumnLabels, ""},
		{importexport.ColumnIdentifications, ""},
		{importexport.ColumnAttachments, ""},
		{importexport.ColumnWarrantyHolder, ""},
		{importexport.ColumnPurchaseVendor, ""},
		{importexport.ColumnSaleBuyerName, ""},
		{"cf:Color", ""},
		{"cf:Weight (kg)", ""},
		{"cfx:boolean:Registered", ""},
	} {
		if got := col(row2, tc.col); got != tc.want {
			t.Errorf("item2 column %q = %q, want %q", tc.col, got, tc.want)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%+v): %v", v, err)
	}
	return b
}
