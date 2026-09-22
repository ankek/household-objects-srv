package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/devicetoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"mime/multipart"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

var cheapHasherConfig = auth.Config{
	Params: auth.Params{MemoryKiB: 64, Time: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16},
}

type liveServer struct {
	handler http.Handler
	storage *storage.Storage
	dbPath  string
}

func newLiveServer(t *testing.T, registrationOpen bool) *liveServer {
	t.Helper()
	return newLiveServerAtPath(t, filepath.Join(t.TempDir(), "hho.db"), registrationOpen)
}

func newLiveServerAtPath(t *testing.T, dbPath string, registrationOpen bool) *liveServer {
	t.Helper()

	store, err := storage.Open(context.Background(), storage.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	dataDir := t.TempDir()
	if err := datadir.Ensure(dataDir); err != nil {
		t.Fatalf("datadir.Ensure(%s): %v", dataDir, err)
	}

	hasher, err := auth.NewHasher(cheapHasherConfig)
	if err != nil {
		t.Fatalf("auth.NewHasher: %v", err)
	}

	groupsSvc, err := groups.NewService(store, hasher)
	if err != nil {
		t.Fatalf("groups.NewService: %v", err)
	}
	gate, err := groups.NewRegistrationGate(groupsSvc, registrationOpen)
	if err != nil {
		t.Fatalf("groups.NewRegistrationGate: %v", err)
	}

	loginSvc, err := session.NewService(store, hasher)
	if err != nil {
		t.Fatalf("session.NewService: %v", err)
	}
	sessAuth, err := session.NewAuthenticator(store)
	if err != nil {
		t.Fatalf("session.NewAuthenticator: %v", err)
	}

	devAuth, err := devicetoken.NewAuthenticator(store)
	if err != nil {
		t.Fatalf("devicetoken.NewAuthenticator: %v", err)
	}
	devSvc, err := devicetoken.NewService(store)
	if err != nil {
		t.Fatalf("devicetoken.NewService: %v", err)
	}

	inviteSvc, err := invite.NewService(store, hasher)
	if err != nil {
		t.Fatalf("invite.NewService: %v", err)
	}

	cfg := httpapi.Config{
		Version:              "test",
		Schema:               store,
		Logger:               slog.New(slog.DiscardHandler),
		Authenticator:        middleware.Chain(sessAuth, devAuth),
		Scopes:               store,
		Registrar:            gate,
		LoginService:         loginSvc,
		SessionAuthenticator: sessAuth,
		DeviceTokenService:   devSvc,
		Sessions:             store,
		DeviceTokens:         store,
		InviteService:        inviteSvc,
		Invites:              store,
		InviteRedeemer:       inviteSvc,
		DataDir:              dataDir,
		MaxAttachmentBytes:   attachments.DefaultMaxUploadBytes,
		Store:                store,
	}

	h, err := httpapi.NewRouter(cfg)
	if err != nil {
		t.Fatalf("httpapi.NewRouter: %v", err)
	}

	return &liveServer{handler: h, storage: store, dbPath: dbPath}
}

func (s *liveServer) do(t *testing.T, method, path, cookie string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func (s *liveServer) doBearer(t *testing.T, method, path, bearerToken string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func (s *liveServer) doMultipart(t *testing.T, method, path, cookie string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", contentType)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func mustDecode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problem.Problem {
	t.Helper()
	var p problem.Problem
	mustDecode(t, rec, &p)
	return p
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie in response; headers = %v", session.CookieName, rec.Header())
	return ""
}

func (s *liveServer) execSQL(t *testing.T, query string, args ...any) {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+s.dbPath)
	if err != nil {
		t.Fatalf("open independent connection to %s: %v", s.dbPath, err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func regBody(username, password string) []byte {
	b, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return b
}

func redeemBody(token, username, password string) []byte {
	b, _ := json.Marshal(map[string]string{"token": token, "username": username, "password": password})
	return b
}

func itemCreateBody(name string) []byte {
	b, _ := json.Marshal(map[string]any{"name": name})
	return b
}

func itemUpdateBody(name string, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "version": version})
	return b
}

func warrantyCreateBody(holder string) []byte {
	b, _ := json.Marshal(map[string]any{"holder": holder})
	return b
}

func warrantyUpdateBody(holder string, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"holder": holder, "version": version})
	return b
}

func saleCreateBody(buyerName string) []byte {
	b, _ := json.Marshal(map[string]any{"buyer_name": buyerName})
	return b
}

func saleUpdateBody(buyerName string, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"buyer_name": buyerName, "version": version})
	return b
}

func purchaseCreateBody(vendor string) []byte {
	b, _ := json.Marshal(map[string]any{"vendor": vendor})
	return b
}

func purchaseUpdateBody(vendor string, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"vendor": vendor, "version": version})
	return b
}

func identificationCreateBody(kind, value string) []byte {
	b, _ := json.Marshal(map[string]any{"kind": kind, "value": value})
	return b
}

func identificationUpdateBody(kind, value string, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"kind": kind, "value": value, "version": version})
	return b
}

func groupVisibilityUpdateBody(warrantyVisible, saleVisible, purchaseVisible bool, version int64) []byte {
	b, _ := json.Marshal(map[string]any{
		"warranty_visible": warrantyVisible,
		"sale_visible":     saleVisible,
		"purchase_visible": purchaseVisible,
		"version":          version,
	})
	return b
}

func customFieldDefCreateBody(name, fieldType string, displayOrder int64) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "field_type": fieldType, "display_order": displayOrder})
	return b
}

func customFieldDefUpdateBody(name, fieldType string, displayOrder, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "field_type": fieldType, "display_order": displayOrder, "version": version})
	return b
}

func labelCreateBody(name, color string) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "color": color})
	return b
}

func labelUpdateBody(name, color string, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "color": color, "version": version})
	return b
}

func locationCreateBody(name, parentID string) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "parent_id": parentID})
	return b
}

func locationUpdateBody(name, parentID string, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "parent_id": parentID, "version": version})
	return b
}

func itemCustomFieldCreateBody(name, textValue string) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "field_type": "text", "text_value": textValue})
	return b
}

func itemCustomFieldUpdateBody(name, textValue string, version int64) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "field_type": "text", "text_value": textValue, "version": version})
	return b
}

func stockAdjustmentCreateBody(delta int64, reason, note string) []byte {
	b, _ := json.Marshal(map[string]any{"delta": delta, "reason": reason, "note": note})
	return b
}

func attachmentUploadMultipartBody(t *testing.T, category, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("category", category); err != nil {
		t.Fatalf("WriteField(category): %v", err)
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func testImageJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode test JPEG: %v", err)
	}
	return buf.Bytes()
}

type registerResponse struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

type loginResponse struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type inviteCreateResponse struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

type inviteRedeemResponse struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

type sessionListItem struct {
	ID            string `json:"id"`
	UserAgent     string `json:"user_agent"`
	CreatedFromIP string `json:"created_from_ip"`
	CreatedAt     int64  `json:"created_at"`
	ExpiresAt     int64  `json:"expires_at"`
	Revoked       bool   `json:"revoked"`
	RevokedAt     int64  `json:"revoked_at,omitempty"`
}

type sessionListResponse struct {
	Sessions []sessionListItem `json:"sessions"`
}

type deviceTokenIssueResponse struct {
	ID          string `json:"id"`
	DeviceLabel string `json:"device_label"`
	Token       string `json:"token"`
}

type deviceTokenListItem struct {
	ID          string `json:"id"`
	DeviceLabel string `json:"device_label"`
	CreatedAt   int64  `json:"created_at"`
	Revoked     bool   `json:"revoked"`
	RevokedAt   int64  `json:"revoked_at,omitempty"`
}

type deviceTokenListResponse struct {
	DeviceTokens []deviceTokenListItem `json:"device_tokens"`
}

type inviteListItem struct {
	ID               string `json:"id"`
	CreatedByUserID  string `json:"created_by_user_id"`
	CreatedAt        int64  `json:"created_at"`
	ExpiresAt        int64  `json:"expires_at"`
	Redeemed         bool   `json:"redeemed"`
	RedeemedAt       int64  `json:"redeemed_at,omitempty"`
	RedeemedByUserID string `json:"redeemed_by_user_id,omitempty"`
}

type inviteListResponse struct {
	Invites []inviteListItem `json:"invites"`
}

type itemResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LocationID  string `json:"location_id"`
	Quantity    int64  `json:"quantity"`
	ShortCode   string `json:"short_code"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	Version     int64  `json:"version"`
}

type itemListResponse struct {
	Items []itemResponse `json:"items"`
}

func labelsQRBatchBody(itemIDs ...string) []byte {
	b, _ := json.Marshal(map[string]any{"item_ids": itemIDs})
	return b
}

type labelsQRBatchItemResponse struct {
	ID        string `json:"id"`
	ShortCode string `json:"short_code"`
	Name      string `json:"name"`
	QRSVG     string `json:"qr_svg"`
}

type labelsQRBatchResponse struct {
	Items []labelsQRBatchItemResponse `json:"items"`
}

type warrantyResponse struct {
	ItemID     string `json:"item_id"`
	Holder     string `json:"holder"`
	Provider   string `json:"provider"`
	StartsOn   string `json:"starts_on"`
	ExpiresOn  string `json:"expires_on"`
	IsLifetime bool   `json:"is_lifetime"`
	Notes      string `json:"notes"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	Version    int64  `json:"version"`
}

type saleResponse struct {
	ItemID         string `json:"item_id"`
	BuyerName      string `json:"buyer_name"`
	SoldOn         string `json:"sold_on"`
	SalePriceMinor int64  `json:"sale_price_minor"`
	Notes          string `json:"notes"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	Version        int64  `json:"version"`
}

type purchaseResponse struct {
	ItemID             string `json:"item_id"`
	Vendor             string `json:"vendor"`
	PurchasedOn        string `json:"purchased_on"`
	PurchasePriceMinor int64  `json:"purchase_price_minor"`
	OrderReference     string `json:"order_reference"`
	Notes              string `json:"notes"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
	Version            int64  `json:"version"`
}

type groupVisibilityResponse struct {
	WarrantyVisible bool  `json:"warranty_visible"`
	SaleVisible     bool  `json:"sale_visible"`
	PurchaseVisible bool  `json:"purchase_visible"`
	Version         int64 `json:"version"`
}

type groupMemberListItem struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	JoinedAt int64  `json:"joined_at"`
}

type groupMembersResponse struct {
	Members []groupMemberListItem `json:"members"`
}

type identificationResponse struct {
	ID        string `json:"id"`
	ItemID    string `json:"item_id"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	Version   int64  `json:"version"`
}

type identificationListResponse struct {
	Identifications []identificationResponse `json:"identifications"`
}

type customFieldDefResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	FieldType    string `json:"field_type"`
	DisplayOrder int64  `json:"display_order"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	Version      int64  `json:"version"`
}

type customFieldDefListResponse struct {
	CustomFieldDefs []customFieldDefResponse `json:"custom_field_defs"`
}

type locationResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ParentID  string `json:"parent_id"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	Version   int64  `json:"version"`
}

type locationListResponse struct {
	Locations []locationResponse `json:"locations"`
}

type itemCustomFieldResponse struct {
	ID          string   `json:"id"`
	ItemID      string   `json:"item_id"`
	FieldDefID  string   `json:"field_def_id"`
	Name        string   `json:"name"`
	FieldType   string   `json:"field_type"`
	TextValue   *string  `json:"text_value"`
	NumberValue *float64 `json:"number_value"`
	BoolValue   *bool    `json:"bool_value"`
	DateValue   *string  `json:"date_value"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
	Version     int64    `json:"version"`
}

type itemCustomFieldListResponse struct {
	CustomFields []itemCustomFieldResponse `json:"custom_fields"`
}

type stockAdjustmentResponse struct {
	ID                string `json:"id"`
	ItemID            string `json:"item_id"`
	Delta             int64  `json:"delta"`
	Reason            string `json:"reason"`
	Note              string `json:"note"`
	ResultingQuantity int64  `json:"resulting_quantity"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
	Version           int64  `json:"version"`
}

type stockAdjustmentListResponse struct {
	StockAdjustments []stockAdjustmentResponse `json:"stock_adjustments"`
}

type labelResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	Version   int64  `json:"version"`
}

type labelListResponse struct {
	Labels []labelResponse `json:"labels"`
}

type attachmentResponse struct {
	ID               string `json:"id"`
	ItemID           string `json:"item_id"`
	Category         string `json:"category"`
	OriginalFilename string `json:"original_filename"`
	ContentType      string `json:"content_type"`
	SizeBytes        int64  `json:"size_bytes"`
	SHA256           string `json:"sha256"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
	Version          int64  `json:"version"`
	HasThumbnail     bool   `json:"has_thumbnail"`
}

func itemCreateBodyWithLocation(name, locationID string) []byte {
	b, _ := json.Marshal(map[string]any{"name": name, "location_id": locationID})
	return b
}

func warrantyCreateBodyWithExpiry(holder, expiresOn string) []byte {
	b, _ := json.Marshal(map[string]any{"holder": holder, "expires_on": expiresOn})
	return b
}

func purchaseCreateBodyWithPriceAndDate(vendor string, priceMinor int64, purchasedOn string) []byte {
	b, _ := json.Marshal(map[string]any{
		"vendor":               vendor,
		"purchase_price_minor": priceMinor,
		"purchased_on":         purchasedOn,
	})
	return b
}

type reportValuationRow struct {
	GroupKey        string `json:"group_key"`
	GroupLabel      string `json:"group_label"`
	ItemCount       int64  `json:"item_count"`
	TotalValueMinor int64  `json:"total_value_minor"`
}

type reportValuationResponse struct {
	Rows []reportValuationRow `json:"rows"`
}

type reportWarrantyExpiringRow struct {
	ItemID        string `json:"item_id"`
	ItemName      string `json:"item_name"`
	ExpiresOn     string `json:"expires_on"`
	DaysRemaining int    `json:"days_remaining"`
}

type reportWarrantyExpiringResponse struct {
	Rows []reportWarrantyExpiringRow `json:"rows"`
}

type reportPurchaseRow struct {
	ItemID             string `json:"item_id"`
	ItemName           string `json:"item_name"`
	PurchasedOn        string `json:"purchased_on"`
	Vendor             string `json:"vendor"`
	PurchasePriceMinor int64  `json:"purchase_price_minor"`
}

type reportPurchasesResponse struct {
	Rows []reportPurchaseRow `json:"rows"`
}

type reportLocationItemCountRow struct {
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	ItemCount    int64  `json:"item_count"`
}

type reportItemCountByLocationResponse struct {
	Rows []reportLocationItemCountRow `json:"rows"`
}
