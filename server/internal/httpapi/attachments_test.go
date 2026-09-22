package httpapi

import (
	"bytes"
	"context"
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeAttachmentItemRepository struct {
	getFn func(ctx context.Context, itemID string) (storage.Item, error)
}

func (f fakeAttachmentItemRepository) Get(ctx context.Context, itemID string) (storage.Item, error) {
	return f.getFn(ctx, itemID)
}
func (fakeAttachmentItemRepository) GetByShortCode(context.Context, string) (storage.Item, error) {
	panic("attachments_test: GetByShortCode: the upload handler never calls this")
}
func (fakeAttachmentItemRepository) GetByIDs(context.Context, []string) ([]storage.Item, error) {
	panic("attachments_test: GetByIDs: the upload handler never calls this")
}
func (fakeAttachmentItemRepository) List(context.Context, storage.Page) ([]storage.Item, error) {
	panic("attachments_test: List: the upload handler never calls this")
}
func (fakeAttachmentItemRepository) ListFiltered(context.Context, storage.ItemFilter, storage.Page) ([]storage.Item, error) {
	panic("attachments_test: ListFiltered: the upload handler never calls this")
}
func (fakeAttachmentItemRepository) Create(context.Context, storage.CreateItemParams) (storage.Item, error) {
	panic("attachments_test: Create: the upload handler never calls this")
}
func (fakeAttachmentItemRepository) Update(context.Context, storage.UpdateItemParams) (storage.Item, error) {
	panic("attachments_test: Update: the upload handler never calls this")
}
func (fakeAttachmentItemRepository) Delete(context.Context, string, int64) error {
	panic("attachments_test: Delete: the upload handler never calls this")
}

type fakeAttachmentRepository struct {
	createFn           func(ctx context.Context, p storage.CreateAttachmentParams) (storage.Attachment, error)
	setThumbnailPathFn func(ctx context.Context, itemID, id, thumbnailPath string, now int64) (storage.Attachment, error)
	getFn              func(ctx context.Context, itemID, id string) (storage.Attachment, error)
	deleteFn           func(ctx context.Context, itemID, id string, now int64) error
	listForItemFn      func(ctx context.Context, itemID string) ([]storage.Attachment, error)
}

func (f fakeAttachmentRepository) Create(ctx context.Context, p storage.CreateAttachmentParams) (storage.Attachment, error) {
	return f.createFn(ctx, p)
}

func (f fakeAttachmentRepository) ListForItem(ctx context.Context, itemID string) ([]storage.Attachment, error) {
	if f.listForItemFn != nil {
		return f.listForItemFn(ctx, itemID)
	}
	panic("attachments_test: fakeAttachmentRepository.ListForItem: this test never configured listForItemFn")
}

func (f fakeAttachmentRepository) Get(ctx context.Context, itemID, id string) (storage.Attachment, error) {
	if f.getFn != nil {
		return f.getFn(ctx, itemID, id)
	}
	panic("attachments_test: fakeAttachmentRepository.Get: this test never configured getFn")
}

func (f fakeAttachmentRepository) SetThumbnailPath(ctx context.Context, itemID, id, thumbnailPath string, now int64) (storage.Attachment, error) {
	if f.setThumbnailPathFn != nil {
		return f.setThumbnailPathFn(ctx, itemID, id, thumbnailPath, now)
	}
	return storage.Attachment{}, nil
}

func (f fakeAttachmentRepository) Delete(ctx context.Context, itemID, id string, now int64) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, itemID, id, now)
	}
	panic("attachments_test: fakeAttachmentRepository.Delete: this test never configured deleteFn")
}

func (fakeAttachmentRepository) ListAll(context.Context) ([]storage.Attachment, error) {
	panic("attachments_test: fakeAttachmentRepository.ListAll: no handler in this package calls this")
}

func (fakeAttachmentRepository) ClearThumbnailPath(context.Context, string, string, int64) error {
	panic("attachments_test: fakeAttachmentRepository.ClearThumbnailPath: no handler in this package calls this")
}

type attachmentsFakeScope struct {
	group       storage.GroupID
	items       storage.ItemRepository
	attachments storage.AttachmentRepository
}

func (s attachmentsFakeScope) GroupID() storage.GroupID                        { return s.group }
func (s attachmentsFakeScope) Items() storage.ItemRepository                   { return s.items }
func (s attachmentsFakeScope) Attachments() storage.AttachmentRepository       { return s.attachments }
func (s attachmentsFakeScope) Members() storage.MemberRepository               { return nil }
func (attachmentsFakeScope) Warranty() storage.WarrantyRepository              { return nil }
func (attachmentsFakeScope) Sale() storage.SaleRepository                      { return nil }
func (attachmentsFakeScope) Purchase() storage.PurchaseRepository              { return nil }
func (attachmentsFakeScope) Visibility() storage.GroupVisibilityRepository     { return nil }
func (attachmentsFakeScope) Identifications() storage.IdentificationRepository { return nil }
func (attachmentsFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }
func (attachmentsFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository {
	return nil
}
func (attachmentsFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (attachmentsFakeScope) Labels() storage.LabelRepository                     { return nil }
func (attachmentsFakeScope) ItemLabels() storage.ItemLabelRepository             { return nil }
func (attachmentsFakeScope) Locations() storage.LocationRepository               { return nil }

type attachmentsFakeScopes struct {
	items       storage.ItemRepository
	attachments storage.AttachmentRepository
}

func (s attachmentsFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return attachmentsFakeScope{group: g, items: s.items, attachments: s.attachments}, nil
}

func attachmentsTestConfig(t *testing.T, items storage.ItemRepository, repo storage.AttachmentRepository) Config {
	t.Helper()
	root := t.TempDir()
	if err := datadir.Ensure(root); err != nil {
		t.Fatalf("datadir.Ensure(%s): %v", root, err)
	}

	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = attachmentsFakeScopes{items: items, attachments: repo}
	cfg.DataDir = root
	cfg.MaxAttachmentBytes = attachments.DefaultMaxUploadBytes
	return cfg
}

func liveAttachmentItem() fakeAttachmentItemRepository {
	return fakeAttachmentItemRepository{getFn: func(context.Context, string) (storage.Item, error) {
		return storage.Item{ID: "item-1"}, nil
	}}
}

func creatingAttachmentRepository() fakeAttachmentRepository {
	return fakeAttachmentRepository{createFn: func(_ context.Context, p storage.CreateAttachmentParams) (storage.Attachment, error) {
		return storage.Attachment{
			ID:               p.ID,
			ItemID:           p.ItemID,
			Category:         p.Category,
			OriginalFilename: p.OriginalFilename,
			ContentType:      p.ContentType,
			SizeBytes:        p.SizeBytes,
			Sha256:           p.SHA256,
			Version:          1,
		}, nil
	}}
}

func multipartUploadBody(t *testing.T, category, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if category != "" {
		if err := w.WriteField("category", category); err != nil {
			t.Fatalf("WriteField(category): %v", err)
		}
	}
	if filename != "" {
		part, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func doAttachmentUpload(t *testing.T, h http.Handler, itemID, category, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := multipartUploadBody(t, category, filename, content)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/"+itemID+"/attachments", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAttachmentUploadSucceeds(t *testing.T) {
	pngSignature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}

	var createdParams storage.CreateAttachmentParams
	repo := fakeAttachmentRepository{createFn: func(_ context.Context, p storage.CreateAttachmentParams) (storage.Attachment, error) {
		createdParams = p
		return storage.Attachment{
			ID: "att-1", ItemID: p.ItemID, Category: p.Category, OriginalFilename: p.OriginalFilename,
			ContentType: p.ContentType, SizeBytes: p.SizeBytes, Sha256: p.SHA256, Version: 1,
		}, nil
	}}

	h, err := NewRouter(attachmentsTestConfig(t, liveAttachmentItem(), repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentUpload(t, h, "item-1", attachments.CategoryImage, "photo.png", pngSignature)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	if createdParams.Category != attachments.CategoryImage {
		t.Errorf("Category = %q, want %q", createdParams.Category, attachments.CategoryImage)
	}
	if createdParams.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want %q (sniffed, never the client's claim)", createdParams.ContentType, "image/png")
	}
	if createdParams.OriginalFilename != "photo.png" {
		t.Errorf("OriginalFilename = %q, want %q", createdParams.OriginalFilename, "photo.png")
	}
	if createdParams.SizeBytes != int64(len(pngSignature)) {
		t.Errorf("SizeBytes = %d, want %d", createdParams.SizeBytes, len(pngSignature))
	}

	body := decodeBody(t, rec)
	if got := body["content_type"]; got != "image/png" {
		t.Errorf("response content_type = %v, want %q", got, "image/png")
	}
	if id, _ := body["id"].(string); id == "" {
		t.Error("response carries no id")
	}
}

func TestAttachmentUploadRejectsAnInvalidCategory(t *testing.T) {
	repoCalled := false
	repo := fakeAttachmentRepository{createFn: func(context.Context, storage.CreateAttachmentParams) (storage.Attachment, error) {
		repoCalled = true
		return storage.Attachment{}, nil
	}}

	h, err := NewRouter(attachmentsTestConfig(t, liveAttachmentItem(), repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentUpload(t, h, "item-1", "not-a-real-category", "photo.jpg", []byte("hi"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if repoCalled {
		t.Error("AttachmentRepository.Create was called despite the invalid category")
	}
}

func TestAttachmentUploadRejectsAMissingFilePart(t *testing.T) {
	h, err := NewRouter(attachmentsTestConfig(t, liveAttachmentItem(), creatingAttachmentRepository()))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentUpload(t, h, "item-1", attachments.CategoryImage, "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestAttachmentUploadRejectsAFileOverTheConfiguredLimit(t *testing.T) {
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), creatingAttachmentRepository())
	cfg.MaxAttachmentBytes = 4

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentUpload(t, h, "item-1", attachments.CategoryImage, "photo.jpg", []byte("this is more than four bytes"))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}

	p := decodeProblem(t, rec)
	if p.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("problem status = %d, want 413", p.Status)
	}
}

func TestAttachmentUploadRejectsAForeignOrUnknownItem(t *testing.T) {
	items := fakeAttachmentItemRepository{getFn: func(context.Context, string) (storage.Item, error) {
		return storage.Item{}, storage.ErrNotFound
	}}
	repoCalled := false
	repo := fakeAttachmentRepository{createFn: func(context.Context, storage.CreateAttachmentParams) (storage.Attachment, error) {
		repoCalled = true
		return storage.Attachment{}, nil
	}}

	h, err := NewRouter(attachmentsTestConfig(t, items, repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentUpload(t, h, "item-from-another-group", attachments.CategoryImage, "photo.jpg", []byte("hello"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if repoCalled {
		t.Error("AttachmentRepository.Create was called despite the parent item belonging to another group")
	}

	p := decodeProblem(t, rec)
	if p.Detail != "" {
		t.Errorf("404 carries a detail (%q); an existence oracle for a foreign-group item is exactly what NFR-010 forbids", p.Detail)
	}
}

func TestAttachmentUploadRejectsAnUnauthenticatedRequest(t *testing.T) {
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), creatingAttachmentRepository())
	cfg.Authenticator = rejectingAuth

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentUpload(t, h, "item-1", attachments.CategoryImage, "photo.jpg", []byte("hello"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadBodyLimitIsLargerThanTheConfiguredAttachmentSize(t *testing.T) {
	cfg := Config{MaxAttachmentBytes: 5 << 20}
	if got, want := uploadBodyLimit(cfg), maxAttachmentBytes(cfg); got <= want {
		t.Errorf("uploadBodyLimit(cfg) = %d, want strictly greater than maxAttachmentBytes(cfg) = %d", got, want)
	}
}

func TestMaxAttachmentBytesDefaultsWhenUnconfigured(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		if got := maxAttachmentBytes(Config{MaxAttachmentBytes: limit}); got != attachments.DefaultMaxUploadBytes {
			t.Errorf("maxAttachmentBytes(Config{MaxAttachmentBytes: %d}) = %d, want %d (DefaultMaxUploadBytes)", limit, got, attachments.DefaultMaxUploadBytes)
		}
	}
}
