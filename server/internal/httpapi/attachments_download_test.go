package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeDownloadFixture(t *testing.T, root, groupID, name string, content []byte) {
	t.Helper()
	dir := filepath.Join(root, "attachments", groupID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
		t.Fatalf("write fixture %s/%s: %v", dir, name, err)
	}
}

func doAttachmentDownload(t *testing.T, h http.Handler, itemID, attachmentID, rangeHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/"+itemID+"/attachments/"+attachmentID, nil)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doAttachmentThumbnailDownload(t *testing.T, h http.Handler, itemID, attachmentID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/"+itemID+"/attachments/"+attachmentID+"/thumbnail", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getRepoReturning(row storage.Attachment) fakeAttachmentRepository {
	return fakeAttachmentRepository{getFn: func(context.Context, string, string) (storage.Attachment, error) {
		return row, nil
	}}
}

func TestAttachmentDownloadReturnsExactBytes(t *testing.T) {
	content := []byte("the exact original attachment bytes, unmodified, streamed verbatim end to end")
	wantSum := sha256.Sum256(content)

	repo := getRepoReturning(storage.Attachment{
		ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1",
		ContentType: "application/pdf", OriginalFilename: "manual.pdf",
	})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1", content)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDownload(t, h, "item-1", "att-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	gotSum := sha256.Sum256(rec.Body.Bytes())
	if hex.EncodeToString(gotSum[:]) != hex.EncodeToString(wantSum[:]) {
		t.Errorf("downloaded bytes do not match the original: got sha256 %x, want %x", gotSum, wantSum)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type = %q, want %q", got, "application/pdf")
	}
}

func TestAttachmentDownloadRejectsAForeignOrUnknownAttachment(t *testing.T) {
	repo := fakeAttachmentRepository{getFn: func(context.Context, string, string) (storage.Attachment, error) {
		return storage.Attachment{}, storage.ErrNotFound
	}}
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDownload(t, h, "item-1", "att-from-another-group", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Detail != "" {
		t.Errorf("404 carries a detail (%q); an existence oracle for a foreign-group attachment is exactly what NFR-010 forbids", p.Detail)
	}
}

func TestAttachmentDownloadRejectsAnUnauthenticatedRequest(t *testing.T) {
	repo := getRepoReturning(storage.Attachment{ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1", ContentType: "application/pdf"})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	cfg.Authenticator = rejectingAuth

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDownload(t, h, "item-1", "att-1", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestAttachmentDownloadSetsXContentTypeOptionsNosniff(t *testing.T) {
	repo := getRepoReturning(storage.Attachment{ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1", ContentType: "application/pdf"})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1", []byte("%PDF-1.4 pretend"))

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDownload(t, h, "item-1", "att-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

func TestAttachmentDownloadUsesAttachmentDispositionForAnUnsafeContentType(t *testing.T) {
	repo := getRepoReturning(storage.Attachment{
		ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1",
		ContentType: "text/html", OriginalFilename: "evil.html",
	})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1", []byte("<script>alert(1)</script>"))

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDownload(t, h, "item-1", "att-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := rec.Header().Get("Content-Disposition")
	if len(got) < len("attachment") || got[:len("attachment")] != "attachment" {
		t.Errorf("Content-Disposition = %q, want it to start with %q for an unsafe content type", got, "attachment")
	}
}

func TestAttachmentDownloadUsesInlineDispositionForASafeImageContentType(t *testing.T) {
	pngSignature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	repo := getRepoReturning(storage.Attachment{
		ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1",
		ContentType: "image/png", OriginalFilename: "photo.png",
	})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1", pngSignature)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDownload(t, h, "item-1", "att-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := rec.Header().Get("Content-Disposition")
	if len(got) < len("inline") || got[:len("inline")] != "inline" {
		t.Errorf("Content-Disposition = %q, want it to start with %q for image/png", got, "inline")
	}
}

func TestAttachmentDownloadSupportsRangeRequests(t *testing.T) {
	content := []byte("0123456789ABCDEF")
	repo := getRepoReturning(storage.Attachment{ID: "att-1", ItemID: "item-1", StoragePath: testGroup + "/att-1", ContentType: "application/pdf"})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1", content)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentDownload(t, h, "item-1", "att-1", "bytes=2-5")
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206: %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Body.String(), "2345"; got != want {
		t.Errorf("partial body = %q, want %q", got, want)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 2-5/16" {
		t.Errorf("Content-Range = %q, want %q", got, "bytes 2-5/16")
	}
}

func TestAttachmentThumbnailDownloadSucceeds(t *testing.T) {
	thumbContent := []byte("pretend this is a JPEG-encoded thumbnail")
	repo := getRepoReturning(storage.Attachment{
		ID: "att-1", ItemID: "item-1", ContentType: "image/png",
		ThumbnailPath: sql.NullString{String: testGroup + "/att-1-thumb.jpg", Valid: true},
	})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)
	writeDownloadFixture(t, cfg.DataDir, testGroup, "att-1-thumb.jpg", thumbContent)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentThumbnailDownload(t, h, "item-1", "att-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != string(thumbContent) {
		t.Errorf("thumbnail body = %q, want %q", rec.Body.String(), thumbContent)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want %q (thumbnails are always JPEG, never the original's own content_type)", got, "image/jpeg")
	}
}

func TestAttachmentThumbnailDownloadAnswersNotFoundWhenNoneExists(t *testing.T) {
	repo := getRepoReturning(storage.Attachment{ID: "att-1", ItemID: "item-1", ContentType: "application/pdf"})
	cfg := attachmentsTestConfig(t, liveAttachmentItem(), repo)

	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doAttachmentThumbnailDownload(t, h, "item-1", "att-1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestToAttachmentBodyMapsHasThumbnail(t *testing.T) {
	cases := []struct {
		name          string
		thumbnailPath sql.NullString
		want          bool
	}{
		{"no thumbnail", sql.NullString{}, false},
		{"has thumbnail", sql.NullString{String: "grp/att-thumb.jpg", Valid: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := toAttachmentBody(storage.Attachment{ThumbnailPath: tc.thumbnailPath})
			if body.HasThumbnail != tc.want {
				t.Errorf("HasThumbnail = %v, want %v", body.HasThumbnail, tc.want)
			}
		})
	}
}

func TestAttachmentContentDispositionPercentEncodesHeaderInjectionAttempts(t *testing.T) {
	got := attachmentContentDisposition("attachment", "evil\r\nSet-Cookie: pwned=1")
	for i := range len(got) {
		if got[i] == '\r' || got[i] == '\n' {
			t.Fatalf("attachmentContentDisposition result contains a raw CR/LF byte: %q", got)
		}
	}
	if got == "attachment" {
		t.Fatalf("attachmentContentDisposition dropped the filename parameter entirely: %q", got)
	}
}
