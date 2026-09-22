package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"testing"
)

type fakeItemLabelRepository struct {
	listFn   func(ctx context.Context, itemID string) ([]storage.Label, error)
	idsFn    func(ctx context.Context, labelID string) ([]string, error)
	attachFn func(ctx context.Context, p storage.AttachLabelParams) error
	detachFn func(ctx context.Context, itemID, labelID string, now int64) error
}

func (f fakeItemLabelRepository) ListForItem(ctx context.Context, itemID string) ([]storage.Label, error) {
	return f.listFn(ctx, itemID)
}

func (f fakeItemLabelRepository) ItemIDsForLabel(ctx context.Context, labelID string) ([]string, error) {
	return f.idsFn(ctx, labelID)
}

func (f fakeItemLabelRepository) Attach(ctx context.Context, p storage.AttachLabelParams) error {
	return f.attachFn(ctx, p)
}

func (f fakeItemLabelRepository) Detach(ctx context.Context, itemID, labelID string, now int64) error {
	return f.detachFn(ctx, itemID, labelID, now)
}

type itemLabelFakeScope struct {
	group storage.GroupID
	repo  storage.ItemLabelRepository
}

func (s itemLabelFakeScope) GroupID() storage.GroupID                      { return s.group }
func (s itemLabelFakeScope) Items() storage.ItemRepository                 { return nil }
func (s itemLabelFakeScope) Warranty() storage.WarrantyRepository          { return nil }
func (s itemLabelFakeScope) Sale() storage.SaleRepository                  { return nil }
func (s itemLabelFakeScope) Purchase() storage.PurchaseRepository          { return nil }
func (s itemLabelFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }
func (s itemLabelFakeScope) Identifications() storage.IdentificationRepository {
	return nil
}
func (s itemLabelFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository   { return nil }
func (s itemLabelFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }
func (s itemLabelFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (s itemLabelFakeScope) Labels() storage.LabelRepository                     { return nil }
func (s itemLabelFakeScope) Locations() storage.LocationRepository               { return nil }

func (s itemLabelFakeScope) ItemLabels() storage.ItemLabelRepository { return s.repo }

func (s itemLabelFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s itemLabelFakeScope) Members() storage.MemberRepository { return nil }

type itemLabelFakeScopes struct {
	repo storage.ItemLabelRepository
}

func (s itemLabelFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return itemLabelFakeScope{group: g, repo: s.repo}, nil
}

func itemLabelTestConfig(repo storage.ItemLabelRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = itemLabelFakeScopes{repo: repo}
	return cfg
}

const (
	itemLabelCollectionPath = "/api/v1/items/itm-1/labels"
	itemLabelEdgePath       = "/api/v1/items/itm-1/labels/lbl-1"
)

func TestItemLabelRoutesAreMounted(t *testing.T) {
	seen := map[string]bool{}
	repo := fakeItemLabelRepository{
		listFn: func(context.Context, string) ([]storage.Label, error) {
			seen["GET"] = true
			return []storage.Label{}, nil
		},
		attachFn: func(context.Context, storage.AttachLabelParams) error {
			seen["PUT"] = true
			return nil
		},
		detachFn: func(context.Context, string, string, int64) error {
			seen["DELETE"] = true
			return nil
		},
	}
	h, err := NewRouter(itemLabelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, itemLabelCollectionPath},
		{http.MethodPut, itemLabelEdgePath},
		{http.MethodDelete, itemLabelEdgePath},
	} {
		rec := do(t, h, tc.method, tc.path)
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status = %d -- route not mounted", tc.method, tc.path, rec.Code)
		}
	}
	for _, m := range []string{"GET", "PUT", "DELETE"} {
		if !seen[m] {
			t.Errorf("%s never reached its repository method", m)
		}
	}
}

func TestAttachPassesBothPathIDsToTheRepository(t *testing.T) {
	var got storage.AttachLabelParams
	repo := fakeItemLabelRepository{
		attachFn: func(_ context.Context, p storage.AttachLabelParams) error {
			got = p
			return nil
		},
	}
	h, err := NewRouter(itemLabelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := do(t, h, http.MethodPut, itemLabelEdgePath)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if got.ItemID != "itm-1" {
		t.Errorf("ItemID = %q, want \"itm-1\"", got.ItemID)
	}
	if got.LabelID != "lbl-1" {
		t.Errorf("LabelID = %q, want \"lbl-1\" -- the second path id never reached storage", got.LabelID)
	}
	if got.ID == "" {
		t.Error("the edge's own id was not minted before the repository was called")
	}
	if got.Now <= 0 {
		t.Error("Now was not stamped")
	}
}

func TestDetachPassesBothPathIDsToTheRepository(t *testing.T) {
	var gotItem, gotLabel string
	repo := fakeItemLabelRepository{
		detachFn: func(_ context.Context, itemID, labelID string, _ int64) error {
			gotItem, gotLabel = itemID, labelID
			return nil
		},
	}
	h, err := NewRouter(itemLabelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := do(t, h, http.MethodDelete, itemLabelEdgePath)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if gotItem != "itm-1" || gotLabel != "lbl-1" {
		t.Errorf("Detach(%q, %q), want (\"itm-1\", \"lbl-1\")", gotItem, gotLabel)
	}
}

func TestItemLabelHandlersTranslateNotFound(t *testing.T) {
	repo := fakeItemLabelRepository{
		listFn: func(context.Context, string) ([]storage.Label, error) {
			return nil, storage.ErrNotFound
		},
		attachFn: func(context.Context, storage.AttachLabelParams) error {
			return storage.ErrNotFound
		},
		detachFn: func(context.Context, string, string, int64) error {
			return storage.ErrNotFound
		},
	}
	h, err := NewRouter(itemLabelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, itemLabelCollectionPath},
		{http.MethodPut, itemLabelEdgePath},
		{http.MethodDelete, itemLabelEdgePath},
	} {
		rec := do(t, h, tc.method, tc.path)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", tc.method, tc.path, rec.Code)
			continue
		}
		var body struct {
			Detail string `json:"detail"`
			Type   string `json:"type"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s %s: body is not JSON: %v", tc.method, tc.path, err)
			continue
		}
		if body.Detail != "" {
			t.Errorf("%s %s: 404 carries detail %q -- a 404 on these routes must say nothing about which id was wrong", tc.method, tc.path, body.Detail)
		}
	}
}

func TestListReturnsWholeLabelsNotIDs(t *testing.T) {
	repo := fakeItemLabelRepository{
		listFn: func(context.Context, string) ([]storage.Label, error) {
			return []storage.Label{{ID: "lbl-1", Name: "fragile", Color: "#c0392b", Version: 2}}, nil
		},
	}
	h, err := NewRouter(itemLabelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := do(t, h, http.MethodGet, itemLabelCollectionPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var got itemLabelListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Labels) != 1 {
		t.Fatalf("got %d labels, want 1", len(got.Labels))
	}
	if got.Labels[0].Name != "fragile" || got.Labels[0].Color != "#c0392b" {
		t.Errorf("got %+v, want the whole label (name and colour), not just an id", got.Labels[0])
	}
}

func TestItemLabelHandlersReportUnexpectedErrorsAs500(t *testing.T) {
	boom := errors.New("database is on fire")
	repo := fakeItemLabelRepository{
		listFn: func(context.Context, string) ([]storage.Label, error) { return nil, boom },
		attachFn: func(context.Context, storage.AttachLabelParams) error {
			return boom
		},
		detachFn: func(context.Context, string, string, int64) error { return boom },
	}
	h, err := NewRouter(itemLabelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, itemLabelCollectionPath},
		{http.MethodPut, itemLabelEdgePath},
		{http.MethodDelete, itemLabelEdgePath},
	} {
		rec := do(t, h, tc.method, tc.path)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s: status = %d, want 500", tc.method, tc.path, rec.Code)
		}
	}
}
