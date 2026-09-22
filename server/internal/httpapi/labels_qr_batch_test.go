package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var labelsQRBatchTestItems = map[string]storage.Item{
	"018f1e2a-89ab-7cde-8123-000000000001": {
		ID: "018f1e2a-89ab-7cde-8123-000000000001", Name: "Lamp", ShortCode: "LAMP001",
	},
	"018f1e2a-89ab-7cde-8123-000000000002": {
		ID: "018f1e2a-89ab-7cde-8123-000000000002", Name: "Chair", ShortCode: "CHAIR02",
	},
	"018f1e2a-89ab-7cde-8123-000000000003": {
		ID: "018f1e2a-89ab-7cde-8123-000000000003", Name: "Desk", ShortCode: "DESK003",
	},
}

func labelsQRBatchRepo(t *testing.T, calls *int) fakeItemRepository {
	return fakeItemRepository{
		getByIDsFn: func(_ context.Context, ids []string) ([]storage.Item, error) {
			t.Helper()
			if calls != nil {
				*calls++
			}
			var found []storage.Item
			for _, id := range ids {
				if item, ok := labelsQRBatchTestItems[id]; ok {
					found = append(found, item)
				}
			}
			return found, nil
		},
	}
}

func TestLabelsQRBatchHandlerHappyPathReturnsRequestedItems(t *testing.T) {
	var calls int
	repo := labelsQRBatchRepo(t, &calls)
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	requested := []string{
		"018f1e2a-89ab-7cde-8123-000000000003",
		"018f1e2a-89ab-7cde-8123-000000000001",
	}
	rec := doJSON(t, h, http.MethodPost, "/api/v1/labels/qr/batch", map[string]any{"item_ids": requested})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Errorf("repo.GetByIDs was called %d times, want exactly 1 (one round trip regardless of sheet size)", calls)
	}

	var got labelsQRBatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Items) != len(requested) {
		t.Fatalf("len(Items) = %d, want %d; body: %s", len(got.Items), len(requested), rec.Body.String())
	}
	for i, wantID := range requested {
		want := labelsQRBatchTestItems[wantID]
		item := got.Items[i]
		if item.ID != want.ID {
			t.Errorf("Items[%d].ID = %q, want %q (response order must match the request's own order)", i, item.ID, want.ID)
		}
		if item.ShortCode != want.ShortCode {
			t.Errorf("Items[%d].ShortCode = %q, want %q", i, item.ShortCode, want.ShortCode)
		}
		if item.Name != want.Name {
			t.Errorf("Items[%d].Name = %q, want %q", i, item.Name, want.Name)
		}
		if !strings.Contains(item.QRSVG, "<svg") {
			t.Errorf("Items[%d].QRSVG = %q, want it to contain an <svg> element", i, item.QRSVG)
		}
		wantPayload := "http://example.com/i/" + want.ID
		if !strings.Contains(item.QRSVG, wantPayload) {
			t.Errorf("Items[%d].QRSVG does not contain the expected absolute payload %q: %s", i, wantPayload, item.QRSVG)
		}
	}
}

func TestLabelsQRBatchHandlerOmitsForeignOrNonexistentIDsIndistinguishably(t *testing.T) {
	repo := labelsQRBatchRepo(t, nil)
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	requested := []string{
		"018f1e2a-89ab-7cde-8123-000000000001",
		"018f1e2a-89ab-7cde-8123-00000000dead",
		"018f1e2a-89ab-7cde-8123-00000000beef",
	}
	rec := doJSON(t, h, http.MethodPost, "/api/v1/labels/qr/batch", map[string]any{"item_ids": requested})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 (a foreign/nonexistent id must never fail the whole batch)", rec.Code, rec.Body.String())
	}

	var got labelsQRBatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1 (only the caller's own item); body: %s", len(got.Items), rec.Body.String())
	}
	if got.Items[0].ID != requested[0] {
		t.Errorf("Items[0].ID = %q, want %q", got.Items[0].ID, requested[0])
	}
	if strings.Contains(rec.Body.String(), "dead") || strings.Contains(rec.Body.String(), "beef") {
		t.Errorf("response body names a foreign/nonexistent id anywhere: %s", rec.Body.String())
	}
}

func TestLabelsQRBatchHandlerRejectsEmptyItemIDs(t *testing.T) {
	repo := fakeItemRepository{
		getByIDsFn: func(context.Context, []string) ([]storage.Item, error) {
			t.Fatal("repo.GetByIDs was called for an empty item_ids request")
			return nil, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPost, "/api/v1/labels/qr/batch", map[string]any{"item_ids": []string{}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestLabelsQRBatchHandlerRejectsTooManyItemIDs(t *testing.T) {
	repo := fakeItemRepository{
		getByIDsFn: func(context.Context, []string) ([]storage.Item, error) {
			t.Fatal("repo.GetByIDs was called for a request over the item_ids bound")
			return nil, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	tooMany := make([]string, maxLabelBatchItems+1)
	for i := range tooMany {
		tooMany[i] = "018f1e2a-89ab-7cde-8123-000000000001"
	}
	rec := doJSON(t, h, http.MethodPost, "/api/v1/labels/qr/batch", map[string]any{"item_ids": tooMany})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestLabelsQRBatchHandlerRejectsMalformedJSON(t *testing.T) {
	repo := fakeItemRepository{
		getByIDsFn: func(context.Context, []string) ([]storage.Item, error) {
			t.Fatal("repo.GetByIDs was called for malformed JSON")
			return nil, nil
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/labels/qr/batch", bytes.NewBufferString("{not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestLabelsQRBatchHandlerRepositoryFailureAnswers500(t *testing.T) {
	repo := fakeItemRepository{
		getByIDsFn: func(context.Context, []string) ([]storage.Item, error) {
			return nil, errors.New("labels_qr_batch_test: simulated storage fault")
		},
	}
	h, err := NewRouter(itemsTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := doJSON(t, h, http.MethodPost, "/api/v1/labels/qr/batch", map[string]any{
		"item_ids": []string{"018f1e2a-89ab-7cde-8123-000000000001"},
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s, want 500", rec.Code, rec.Body.String())
	}
}

func TestLabelsQRBatchHandlerHonoursPublicBaseURLResolver(t *testing.T) {
	repo := labelsQRBatchRepo(t, nil)
	cfg := itemsTestConfig(repo)
	cfg.PublicBaseURLResolver = func(*http.Request) string { return "https://hho.example.net/hho" }
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	const id = "018f1e2a-89ab-7cde-8123-000000000001"
	rec := doJSON(t, h, http.MethodPost, "/api/v1/labels/qr/batch", map[string]any{"item_ids": []string{id}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}

	var got labelsQRBatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(got.Items))
	}
	want := "https://hho.example.net/hho/i/" + id
	if !strings.Contains(got.Items[0].QRSVG, want) {
		t.Errorf("QRSVG does not contain the resolver-qualified payload %q: %s", want, got.Items[0].QRSVG)
	}
}
