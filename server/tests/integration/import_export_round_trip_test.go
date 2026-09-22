package integration

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport/goldenfixture"
	_ "modernc.org/sqlite"
	"net/http"
	"strings"
	"testing"
)

type snapshotItemRow struct {
	id        string
	version   int64
	updatedAt int64
	changeSeq int64
}

func snapshotItems(t *testing.T, dbPath, groupID string) []snapshotItemRow {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open independent connection to %s: %v", dbPath, err)
	}
	defer func() { _ = conn.Close() }()

	rows, err := conn.QueryContext(t.Context(),
		`SELECT id, version, updated_at, change_seq FROM items WHERE group_id = ? AND deleted_at IS NULL ORDER BY id`, groupID)
	if err != nil {
		t.Fatalf("query items: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []snapshotItemRow
	for rows.Next() {
		var s snapshotItemRow
		if err := rows.Scan(&s.id, &s.version, &s.updatedAt, &s.changeSeq); err != nil {
			t.Fatalf("scan item row: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate item rows: %v", err)
	}
	return out
}

func readGroupChangeSeqCounter(t *testing.T, dbPath, groupID string) int64 {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open independent connection to %s: %v", dbPath, err)
	}
	defer func() { _ = conn.Close() }()

	var n int64
	if err := conn.QueryRowContext(t.Context(), `SELECT change_seq_counter FROM groups WHERE id = ?`, groupID).Scan(&n); err != nil {
		t.Fatalf("read change_seq_counter: %v", err)
	}
	return n
}

func assertItemSnapshotsEqual(t *testing.T, before, after []snapshotItemRow) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("item row count changed: before = %d, after = %d", len(before), len(after))
	}
	for i := range before {
		b, a := before[i], after[i]
		if b.id != a.id {
			t.Fatalf("row %d: id changed: before = %q, after = %q (row set itself is no longer the same)", i, b.id, a.id)
		}
		if b.version != a.version {
			t.Errorf("item %q: version changed: before = %d, after = %d", b.id, b.version, a.version)
		}
		if b.updatedAt != a.updatedAt {
			t.Errorf("item %q: updated_at changed: before = %d, after = %d", b.id, b.updatedAt, a.updatedAt)
		}
		if b.changeSeq != a.changeSeq {
			t.Errorf("item %q: change_seq changed: before = %d, after = %d", b.id, b.changeSeq, a.changeSeq)
		}
	}
}

type importUploadResponseMirror struct {
	ImportID string `json:"import_id"`
}

type importRowErrorMirror struct {
	Line    int    `json:"line"`
	Column  string `json:"column,omitempty"`
	Message string `json:"message"`
}

type importRowMirror struct {
	Line    int                    `json:"line"`
	Action  string                 `json:"action"`
	ItemID  string                 `json:"item_id,omitempty"`
	Name    string                 `json:"name,omitempty"`
	Changes []string               `json:"changes,omitempty"`
	Errors  []importRowErrorMirror `json:"errors,omitempty"`
}

type importSummaryMirror struct {
	Create    int `json:"create"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Error     int `json:"error"`
}

type importPreviewResponseMirror struct {
	ImportID string              `json:"import_id"`
	Rows     []importRowMirror   `json:"rows"`
	Summary  importSummaryMirror `json:"summary"`
}

type importCommitRejectedResponseMirror = importPreviewResponseMirror

type importCommitSummaryMirror struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
}

type importCommitCreatedItemMirror struct {
	Line   int    `json:"line"`
	ItemID string `json:"item_id"`
}

type importCommitResponseMirror struct {
	ImportID     string                          `json:"import_id"`
	Summary      importCommitSummaryMirror       `json:"summary"`
	CreatedItems []importCommitCreatedItemMirror `json:"created_items"`
}

func ensureLocationPath(t *testing.T, srv *liveServer, cookie string, cache map[string]string, path string) string {
	t.Helper()
	if path == "" {
		return ""
	}
	var built strings.Builder
	parentID := ""
	for _, seg := range strings.Split(path, "/") {
		if built.Len() > 0 {
			built.WriteByte('/')
		}
		built.WriteString(seg)
		key := built.String()
		if id, ok := cache[key]; ok {
			parentID = id
			continue
		}
		rec := srv.do(t, http.MethodPost, "/api/v1/locations", cookie, locationCreateBody(seg, parentID))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create location %q (parent %q): status = %d, want 201: %s", seg, parentID, rec.Code, rec.Body.String())
		}
		var loc locationResponse
		mustDecode(t, rec, &loc)
		cache[key] = loc.ID
		parentID = loc.ID
	}
	return parentID
}

func seedGoldenCustomFieldDefs(t *testing.T, srv *liveServer, cookie string) map[string]string {
	t.Helper()
	ids := make(map[string]string, len(goldenfixture.Defs()))
	for i, def := range goldenfixture.Defs() {
		rec := srv.do(t, http.MethodPost, "/api/v1/custom-field-defs", cookie, customFieldDefCreateBody(def.Name, def.FieldType, int64(i+1)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create custom field def %q: status = %d, want 201: %s", def.Name, rec.Code, rec.Body.String())
		}
		var created customFieldDefResponse
		mustDecode(t, rec, &created)
		ids[def.ID] = created.ID
	}
	return ids
}

func seedGoldenLabels(t *testing.T, srv *liveServer, cookie string) map[string]string {
	t.Helper()
	ids := make(map[string]string)
	colors := []string{"#1F77B4", "#FF7F0E", "#2CA02C", "#D62728", "#9467BD", "#8C564B"}
	next := 0
	for _, row := range goldenfixture.Rows() {
		for _, lbl := range row.Labels {
			if _, ok := ids[lbl.Name]; ok {
				continue
			}
			color := colors[next%len(colors)]
			next++
			rec := srv.do(t, http.MethodPost, "/api/v1/labels", cookie, labelCreateBody(lbl.Name, color))
			if rec.Code != http.StatusCreated {
				t.Fatalf("create label %q: status = %d, want 201: %s", lbl.Name, rec.Code, rec.Body.String())
			}
			var created labelResponse
			mustDecode(t, rec, &created)
			ids[lbl.Name] = created.ID
		}
	}
	return ids
}

func manualAttachmentBytes() []byte {
	return []byte("not a real PDF, just standing in for one -- see manualAttachmentBytes's own doc")
}

func seedGoldenFixtureItem(
	t *testing.T,
	srv *liveServer,
	cookie string,
	row importexport.Row,
	locationCache map[string]string,
	labelIDByName map[string]string,
	defIDByLiteral map[string]string,
) string {
	t.Helper()

	locationID := ensureLocationPath(t, srv, cookie, locationCache, row.LocationPath)

	itemBody := map[string]any{
		"name":        row.Item.Name,
		"description": row.Item.Description,
		"quantity":    row.Item.Quantity,
	}
	if locationID != "" {
		itemBody["location_id"] = locationID
	}
	rec := srv.do(t, http.MethodPost, "/api/v1/items", cookie, mustJSON(t, itemBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item %q: status = %d, want 201: %s", row.Item.Name, rec.Code, rec.Body.String())
	}
	var item itemResponse
	mustDecode(t, rec, &item)

	for _, lbl := range row.Labels {
		labelID, ok := labelIDByName[lbl.Name]
		if !ok {
			t.Fatalf("item %q: no seeded label id for %q (seedGoldenLabels did not create it)", row.Item.Name, lbl.Name)
		}
		rec = srv.do(t, http.MethodPut, "/api/v1/items/"+item.ID+"/labels/"+labelID, cookie, nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("attach label %q to item %q: status = %d, want 204: %s", lbl.Name, row.Item.Name, rec.Code, rec.Body.String())
		}
	}

	for _, idn := range row.Identifications {
		rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item.ID+"/identifications", cookie, identificationCreateBody(idn.Kind, idn.Value))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create identification %+v on item %q: status = %d, want 201: %s", idn, row.Item.Name, rec.Code, rec.Body.String())
		}
	}

	for _, att := range row.Attachments {
		content := manualAttachmentBytes()
		if att.Category == "image" {
			content = testImageJPEGBytes(t)
		}
		body, contentType := attachmentUploadMultipartBody(t, att.Category, att.OriginalFilename, content)
		rec = srv.doMultipart(t, http.MethodPost, "/api/v1/items/"+item.ID+"/attachments", cookie, body, contentType)
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload attachment %+v on item %q: status = %d, want 201: %s", att, row.Item.Name, rec.Code, rec.Body.String())
		}
	}

	for _, cf := range row.CustomFields {
		cfBody := map[string]any{"name": cf.Name, "field_type": cf.FieldType}
		if cf.FieldDefID.Valid {
			defID, ok := defIDByLiteral[cf.FieldDefID.String]
			if !ok {
				t.Fatalf("item %q: custom field %q names unseeded def literal id %q", row.Item.Name, cf.Name, cf.FieldDefID.String)
			}
			cfBody["field_def_id"] = defID
		}
		switch cf.FieldType {
		case "text":
			cfBody["text_value"] = cf.TextValue.String
		case "number":
			cfBody["number_value"] = cf.NumberValue.Float64
		case "boolean":
			cfBody["bool_value"] = cf.BoolValue.Int64 != 0
		case "date":
			cfBody["date_value"] = cf.DateValue.String
		default:
			t.Fatalf("item %q: custom field %q has unrecognised field_type %q", row.Item.Name, cf.Name, cf.FieldType)
		}
		rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item.ID+"/custom-fields", cookie, mustJSON(t, cfBody))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create custom field %q on item %q: status = %d, want 201: %s", cf.Name, row.Item.Name, rec.Code, rec.Body.String())
		}
	}

	if row.Warranty != nil {
		rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item.ID+"/warranty", cookie, mustJSON(t, map[string]any{
			"holder": row.Warranty.Holder, "provider": row.Warranty.Provider,
			"starts_on": row.Warranty.StartsOn.String, "expires_on": row.Warranty.ExpiresOn.String,
			"is_lifetime": row.Warranty.IsLifetime != 0, "notes": row.Warranty.Notes,
		}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create warranty on item %q: status = %d, want 201: %s", row.Item.Name, rec.Code, rec.Body.String())
		}
	}
	if row.Purchase != nil {
		rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item.ID+"/purchase", cookie, mustJSON(t, map[string]any{
			"vendor": row.Purchase.Vendor, "purchased_on": row.Purchase.PurchasedOn.String,
			"purchase_price_minor": row.Purchase.PurchasePriceMinor, "order_reference": row.Purchase.OrderReference,
			"notes": row.Purchase.Notes,
		}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create purchase on item %q: status = %d, want 201: %s", row.Item.Name, rec.Code, rec.Body.String())
		}
	}
	if row.Sale != nil {
		rec = srv.do(t, http.MethodPost, "/api/v1/items/"+item.ID+"/sale", cookie, mustJSON(t, map[string]any{
			"buyer_name": row.Sale.BuyerName, "sold_on": row.Sale.SoldOn.String,
			"sale_price_minor": row.Sale.SalePriceMinor, "notes": row.Sale.Notes,
		}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create sale on item %q: status = %d, want 201: %s", row.Item.Name, rec.Code, rec.Body.String())
		}
	}

	return item.ID
}

func TestExportThenReimportGoldenFixtureProducesNoChanges(t *testing.T) {
	srv := newLiveServer(t, false)

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", "alice-correct-horse-battery"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var reg registerResponse
	mustDecode(t, rec, &reg)

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", "alice-correct-horse-battery"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	locationCache := make(map[string]string)
	labelIDByName := seedGoldenLabels(t, srv, cookie)
	defIDByLiteral := seedGoldenCustomFieldDefs(t, srv, cookie)

	rows := goldenfixture.Rows()
	for _, row := range rows {
		seedGoldenFixtureItem(t, srv, cookie, row, locationCache, labelIDByName, defIDByLiteral)
	}

	rec = srv.do(t, http.MethodGet, "/api/v1/export/items.csv", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export (before): status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	preCSV := append([]byte(nil), rec.Body.Bytes()...)
	preRecords, err := csv.NewReader(strings.NewReader(string(preCSV))).ReadAll()
	if err != nil {
		t.Fatalf("export (before) is not valid CSV: %v; body = %q", err, preCSV)
	}
	if len(preRecords) != 1+len(rows) {
		t.Fatalf("export (before) records = %d, want %d (header + %d seeded items)", len(preRecords), 1+len(rows), len(rows))
	}

	before := snapshotItems(t, srv.dbPath, reg.GroupID)
	if len(before) != len(rows) {
		t.Fatalf("snapshotItems (before) = %d rows, want %d", len(before), len(rows))
	}
	beforeChangeSeqCounter := readGroupChangeSeqCounter(t, srv.dbPath, reg.GroupID)

	rec = srv.do(t, http.MethodPost, "/api/v1/import/native/upload", cookie, preCSV)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import upload: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var upload importUploadResponseMirror
	mustDecode(t, rec, &upload)
	if upload.ImportID == "" {
		t.Fatal("import upload response carries no import_id")
	}

	rec = srv.do(t, http.MethodGet, "/api/v1/import/"+upload.ImportID+"/preview", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("import preview: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var preview importPreviewResponseMirror
	mustDecode(t, rec, &preview)
	if preview.Summary.Unchanged != len(rows) || preview.Summary.Create != 0 || preview.Summary.Update != 0 || preview.Summary.Error != 0 {
		t.Fatalf("preview summary = %+v, want {Create:0 Update:0 Unchanged:%d Error:0}", preview.Summary, len(rows))
	}
	for _, r := range preview.Rows {
		if r.Action != "unchanged" {
			t.Errorf("preview row %+v: Action = %q, want %q", r, r.Action, "unchanged")
		}
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/import/"+upload.ImportID+"/commit", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("import commit: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var commit importCommitResponseMirror
	mustDecode(t, rec, &commit)
	if commit.Summary.Unchanged != len(rows) || commit.Summary.Created != 0 || commit.Summary.Updated != 0 {
		t.Fatalf("commit summary = %+v, want {Created:0 Updated:0 Unchanged:%d}", commit.Summary, len(rows))
	}
	if len(commit.CreatedItems) != 0 {
		t.Errorf("commit created_items = %+v, want none", commit.CreatedItems)
	}

	rec = srv.do(t, http.MethodGet, "/api/v1/export/items.csv", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export (after): status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	postCSV := rec.Body.Bytes()
	if !bytes.Equal(preCSV, postCSV) {
		t.Errorf("export (after commit) != export (before import); before:\n%s\nafter:\n%s", preCSV, postCSV)
	}

	after := snapshotItems(t, srv.dbPath, reg.GroupID)
	assertItemSnapshotsEqual(t, before, after)

	if afterChangeSeqCounter := readGroupChangeSeqCounter(t, srv.dbPath, reg.GroupID); afterChangeSeqCounter != beforeChangeSeqCounter {
		t.Errorf("groups.change_seq_counter changed: before = %d, after = %d (a no-op import committed a write somewhere)", beforeChangeSeqCounter, afterChangeSeqCounter)
	}
}

func buildCSVRecord(header []string, values map[string]string) []string {
	row := make([]string, len(header))
	for i, col := range header {
		row[i] = values[col]
	}
	return row
}

func TestImportCommitAllOrNothingLeavesDatabaseUnchanged(t *testing.T) {
	srv := newLiveServer(t, false)

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("bob", "bob-correct-horse-battery"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var reg registerResponse
	mustDecode(t, rec, &reg)

	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("bob", "bob-correct-horse-battery"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody("Well Formed Item"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var item itemResponse
	mustDecode(t, rec, &item)

	rec = srv.do(t, http.MethodGet, "/api/v1/export/items.csv", cookie, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("export records = %d, want 2 (header + 1 item)", len(records))
	}
	header, wellFormedRow := records[0], records[1]

	badRow := buildCSVRecord(header, map[string]string{
		importexport.ColumnName:       "Bad Row",
		importexport.ColumnQuantity:   "1",
		importexport.ColumnLocationID: "does-not-exist-location",
	})

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	for _, row := range [][]string{header, wellFormedRow, badRow} {
		if err := w.Write(row); err != nil {
			t.Fatalf("write CSV row %v: %v", row, err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatalf("flush CSV writer: %v", err)
	}

	before := snapshotItems(t, srv.dbPath, reg.GroupID)
	if len(before) != 1 {
		t.Fatalf("snapshotItems (before) = %d rows, want 1", len(before))
	}
	beforeChangeSeqCounter := readGroupChangeSeqCounter(t, srv.dbPath, reg.GroupID)

	rec = srv.do(t, http.MethodPost, "/api/v1/import/native/upload", cookie, buf.Bytes())
	if rec.Code != http.StatusCreated {
		t.Fatalf("import upload: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var upload importUploadResponseMirror
	mustDecode(t, rec, &upload)

	rec = srv.do(t, http.MethodPost, "/api/v1/import/"+upload.ImportID+"/commit", cookie, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("import commit: status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	var rejected importCommitRejectedResponseMirror
	mustDecode(t, rec, &rejected)
	if rejected.Summary.Error != 1 || rejected.Summary.Unchanged != 1 || rejected.Summary.Create != 0 || rejected.Summary.Update != 0 {
		t.Fatalf("rejected summary = %+v, want {Create:0 Update:0 Unchanged:1 Error:1}", rejected.Summary)
	}
	if len(rejected.Rows) != 2 {
		t.Fatalf("rejected rows = %+v, want exactly 2", rejected.Rows)
	}
	var sawError bool
	for _, r := range rejected.Rows {
		if r.Action != "error" {
			continue
		}
		sawError = true
		if len(r.Errors) == 0 {
			t.Errorf("error row %+v carries no Errors", r)
		}
		var mentionsLocation bool
		for _, e := range r.Errors {
			if e.Column == importexport.ColumnLocationID {
				mentionsLocation = true
			}
		}
		if !mentionsLocation {
			t.Errorf("error row %+v: no error names column %q", r, importexport.ColumnLocationID)
		}
	}
	if !sawError {
		t.Errorf("rejected rows %+v: none classified as \"error\"", rejected.Rows)
	}

	after := snapshotItems(t, srv.dbPath, reg.GroupID)
	assertItemSnapshotsEqual(t, before, after)
	if after[0].id != item.ID {
		t.Errorf("surviving item id = %q, want %q (no partial application swapped identity)", after[0].id, item.ID)
	}

	if afterChangeSeqCounter := readGroupChangeSeqCounter(t, srv.dbPath, reg.GroupID); afterChangeSeqCounter != beforeChangeSeqCounter {
		t.Errorf("groups.change_seq_counter changed: before = %d, after = %d (a rejected commit must never write anything)", beforeChangeSeqCounter, afterChangeSeqCounter)
	}

	if len(after) != 1 {
		t.Errorf("item count after rejected commit = %d, want 1 (the malformed row must never have been created)", len(after))
	}
}
