package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"testing"
)

func listRepoReturning(rows []storage.Attachment) fakeAttachmentRepository {
	return fakeAttachmentRepository{listForItemFn: func(context.Context, string) ([]storage.Attachment, error) {
		return rows, nil
	}}
}

func TestAttachmentListRouteIsMounted(t *testing.T) {
	var seenItemID string
	repo := fakeAttachmentRepository{listForItemFn: func(_ context.Context, itemID string) ([]storage.Attachment, error) {
		seenItemID = itemID
		return []storage.Attachment{}, nil
	}}

	h, err := NewRouter(attachmentsTestConfig(t, liveAttachmentItem(), repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/v1/items/item-1/attachments", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if seenItemID != "item-1" {
		t.Errorf("ListForItem reached its handler with itemID %q, want %q -- the {itemID} wildcard is not being resolved", seenItemID, "item-1")
	}
}

func TestAttachmentListRendersAnEmptyArrayNotNull(t *testing.T) {
	h, err := NewRouter(attachmentsTestConfig(t, liveAttachmentItem(), listRepoReturning(nil)))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodGet, "/api/v1/items/item-1/attachments", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"attachments":[]}`+"\n" && got != `{"attachments":[]}` {
		t.Errorf("body = %q, want an empty array, never null", got)
	}
}

func TestAttachmentListRendersEveryRowInTheOrderTheRepositoryReturned(t *testing.T) {
	rows := []storage.Attachment{
		{ID: "att-1", ItemID: "item-1", Category: "receipt", OriginalFilename: "a.pdf", ContentType: "application/pdf", Version: 1},
		{ID: "att-2", ItemID: "item-1", Category: "image", OriginalFilename: "b.png", ContentType: "image/png", Version: 1, ThumbnailPath: sql.NullString{String: "item-1/att-2-thumb.jpg", Valid: true}},
	}
	h, err := NewRouter(attachmentsTestConfig(t, liveAttachmentItem(), listRepoReturning(rows)))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodGet, "/api/v1/items/item-1/attachments", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body attachmentListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Attachments) != 2 {
		t.Fatalf("attachments = %+v, want exactly 2 elements in repository order", body.Attachments)
	}
	if body.Attachments[0].ID != "att-1" || body.Attachments[1].ID != "att-2" {
		t.Fatalf("ids = [%q %q], want [\"att-1\" \"att-2\"] -- the handler must not reorder the repository's own result", body.Attachments[0].ID, body.Attachments[1].ID)
	}
	if body.Attachments[0].HasThumbnail {
		t.Error("att-1 has_thumbnail = true, want false (no ThumbnailPath set)")
	}
	if !body.Attachments[1].HasThumbnail {
		t.Error("att-2 has_thumbnail = false, want true (ThumbnailPath set)")
	}
}

func TestAttachmentListRejectsAnUnknownOrForeignItem(t *testing.T) {
	repo := fakeAttachmentRepository{listForItemFn: func(context.Context, string) ([]storage.Attachment, error) {
		return nil, storage.ErrNotFound
	}}
	h, err := NewRouter(attachmentsTestConfig(t, liveAttachmentItem(), repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodGet, "/api/v1/items/item-from-another-group/attachments", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Detail != "" {
		t.Errorf("404 carries detail %q; a not-found must say nothing request-specific (NFR-010)", p.Detail)
	}
}

func TestAttachmentListHandlerReportsAnUnexpectedRepositoryFaultAs500(t *testing.T) {
	repo := fakeAttachmentRepository{listForItemFn: func(context.Context, string) ([]storage.Attachment, error) {
		return nil, sql.ErrConnDone
	}}
	h, err := NewRouter(attachmentsTestConfig(t, liveAttachmentItem(), repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodGet, "/api/v1/items/item-1/attachments", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
}
