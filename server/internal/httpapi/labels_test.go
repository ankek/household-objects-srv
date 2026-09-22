package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/labels"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeLabelRepository struct {
	getFn    func(ctx context.Context, id string) (storage.Label, error)
	listFn   func(ctx context.Context) ([]storage.Label, error)
	createFn func(ctx context.Context, p storage.CreateLabelParams) (storage.Label, error)
	updateFn func(ctx context.Context, p storage.UpdateLabelParams) (storage.Label, error)
	deleteFn func(ctx context.Context, id string, now int64) error
}

func (f fakeLabelRepository) Get(ctx context.Context, id string) (storage.Label, error) {
	return f.getFn(ctx, id)
}

func (f fakeLabelRepository) List(ctx context.Context) ([]storage.Label, error) {
	return f.listFn(ctx)
}

func (f fakeLabelRepository) Create(ctx context.Context, p storage.CreateLabelParams) (storage.Label, error) {
	return f.createFn(ctx, p)
}

func (f fakeLabelRepository) Update(ctx context.Context, p storage.UpdateLabelParams) (storage.Label, error) {
	return f.updateFn(ctx, p)
}

func (f fakeLabelRepository) Delete(ctx context.Context, id string, now int64) error {
	return f.deleteFn(ctx, id, now)
}

type labelFakeScope struct {
	group storage.GroupID
	repo  storage.LabelRepository
}

func (s labelFakeScope) GroupID() storage.GroupID                      { return s.group }
func (s labelFakeScope) Items() storage.ItemRepository                 { return nil }
func (s labelFakeScope) Warranty() storage.WarrantyRepository          { return nil }
func (s labelFakeScope) Sale() storage.SaleRepository                  { return nil }
func (s labelFakeScope) Purchase() storage.PurchaseRepository          { return nil }
func (s labelFakeScope) Visibility() storage.GroupVisibilityRepository { return nil }
func (s labelFakeScope) Identifications() storage.IdentificationRepository {
	return nil
}
func (s labelFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository   { return nil }
func (s labelFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }
func (s labelFakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }

func (s labelFakeScope) Locations() storage.LocationRepository { return nil }

func (s labelFakeScope) Labels() storage.LabelRepository           { return s.repo }
func (s labelFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s labelFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s labelFakeScope) Members() storage.MemberRepository { return nil }

type labelFakeScopes struct {
	repo storage.LabelRepository
}

func (s labelFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return labelFakeScope{group: g, repo: s.repo}, nil
}

func labelTestConfig(repo storage.LabelRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = labelFakeScopes{repo: repo}
	return cfg
}

const (
	labelCollectionPath = "/api/v1/labels"
	labelItemPath       = "/api/v1/labels/lbl-1"
)

func TestLabelRoutesAreMounted(t *testing.T) {
	seen := map[string]bool{}
	repo := fakeLabelRepository{
		listFn: func(context.Context) ([]storage.Label, error) {
			seen["GET-list"] = true
			return []storage.Label{}, nil
		},
		createFn: func(_ context.Context, p storage.CreateLabelParams) (storage.Label, error) {
			seen["POST"] = true
			return storage.Label{ID: "lbl-1", Name: p.Name, Color: p.Color, Version: 1}, nil
		},
		updateFn: func(_ context.Context, p storage.UpdateLabelParams) (storage.Label, error) {
			seen["PUT"] = true
			return storage.Label{ID: p.ID, Name: p.Name, Color: p.Color, Version: 2}, nil
		},
		deleteFn: func(context.Context, string, int64) error {
			seen["DELETE"] = true
			return nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	do(t, h, http.MethodGet, labelCollectionPath)

	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(map[string]any{"name": "Kitchen", "color": "#888888"})
	req := httptest.NewRequest(http.MethodPost, labelCollectionPath, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	buf.Reset()
	_ = json.NewEncoder(&buf).Encode(map[string]any{"name": "Kitchen", "color": "#888888", "version": 1})
	req = httptest.NewRequest(http.MethodPut, labelItemPath, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	do(t, h, http.MethodDelete, labelItemPath)

	for _, want := range []string{"GET-list", "POST", "PUT", "DELETE"} {
		if !seen[want] {
			t.Errorf("route for %s was not reached", want)
		}
	}
}

func TestLabelListHandlerReturnsLabels(t *testing.T) {
	repo := fakeLabelRepository{
		listFn: func(context.Context) ([]storage.Label, error) {
			return []storage.Label{{ID: "lbl-1", Name: "Kitchen", Color: "#888888", Version: 1}}, nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := do(t, h, http.MethodGet, labelCollectionPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got labelListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "Kitchen" || got.Labels[0].Color != "#888888" {
		t.Fatalf("got %+v, want one label {Kitchen #888888}", got.Labels)
	}
}

func TestLabelListHandlerInternalErrorMaps500(t *testing.T) {
	repo := fakeLabelRepository{
		listFn: func(context.Context) ([]storage.Label, error) { return nil, sql.ErrConnDone },
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := do(t, h, http.MethodGet, labelCollectionPath)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestLabelCreateHandlerCreatesLabel(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(_ context.Context, p storage.CreateLabelParams) (storage.Label, error) {
			return storage.Label{ID: "lbl-1", Name: p.Name, Color: p.Color, Version: 1}, nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, labelCollectionPath, map[string]any{"name": "Kitchen", "color": "#FF8800"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	var got labelBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Kitchen" || got.Color != "#FF8800" || got.Version != 1 {
		t.Fatalf("got %+v, want {Kitchen #FF8800 1}", got)
	}
}

func TestLabelCreateHandlerRejectsMalformedJSON(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(context.Context, storage.CreateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite malformed JSON")
			return storage.Label{}, nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, labelCollectionPath, bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestLabelCreateHandlerRejectsABlankName(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(context.Context, storage.CreateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite a blank name")
			return storage.Label{}, nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, labelCollectionPath, map[string]any{"name": "", "color": "#888888"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLabelCreateHandlerRejectsAnInvalidColor(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(context.Context, storage.CreateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite an invalid color")
			return storage.Label{}, nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, labelCollectionPath, map[string]any{"name": "Kitchen", "color": "not-a-color"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLabelCreateHandlerReturnsConflictOnDuplicateName(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(context.Context, storage.CreateLabelParams) (storage.Label, error) {
			return storage.Label{}, storage.ErrLabelNameConflict
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, labelCollectionPath, map[string]any{"name": "Kitchen", "color": "#888888"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLabelUpdateHandlerUpdatesLabel(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(_ context.Context, p storage.UpdateLabelParams) (storage.Label, error) {
			return storage.Label{ID: p.ID, Name: p.Name, Color: p.Color, Version: 2}, nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, labelItemPath, map[string]any{"name": "New name", "color": "#00FF00", "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var got labelBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "New name" || got.Color != "#00FF00" || got.Version != 2 {
		t.Fatalf("got %+v, want {\"New name\" #00FF00 2}", got)
	}
}

func TestLabelUpdateHandlerReturnsNotFound(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(context.Context, storage.UpdateLabelParams) (storage.Label, error) {
			return storage.Label{}, storage.ErrNotFound
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, labelItemPath, map[string]any{"name": "n", "color": "#888888", "version": 1})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLabelUpdateHandlerReturnsConflictOnVersionMismatch(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(context.Context, storage.UpdateLabelParams) (storage.Label, error) {
			return storage.Label{}, storage.ErrVersionMismatch
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, labelItemPath, map[string]any{"name": "n", "color": "#888888", "version": 1})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLabelUpdateHandlerReturnsConflictOnNameCollision(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(context.Context, storage.UpdateLabelParams) (storage.Label, error) {
			return storage.Label{}, storage.ErrLabelNameConflict
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, labelItemPath, map[string]any{"name": "Kitchen", "color": "#888888", "version": 1})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLabelUpdateHandlerRejectsAMissingVersion(t *testing.T) {
	repo := fakeLabelRepository{
		updateFn: func(context.Context, storage.UpdateLabelParams) (storage.Label, error) {
			t.Error("the repository was called despite a missing version")
			return storage.Label{}, nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := doJSON(t, h, http.MethodPut, labelItemPath, map[string]any{"name": "n", "color": "#888888"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLabelDeleteHandlerDeletesLabel(t *testing.T) {
	var gotID string
	repo := fakeLabelRepository{
		deleteFn: func(_ context.Context, id string, _ int64) error {
			gotID = id
			return nil
		},
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := do(t, h, http.MethodDelete, labelItemPath)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", rec.Code, rec.Body.String())
	}
	if gotID != "lbl-1" {
		t.Errorf("Delete called with id = %q, want lbl-1", gotID)
	}
}

func TestLabelDeleteHandlerReturnsNotFound(t *testing.T) {
	repo := fakeLabelRepository{
		deleteFn: func(context.Context, string, int64) error { return storage.ErrNotFound },
	}
	h, err := NewRouter(labelTestConfig(repo))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := do(t, h, http.MethodDelete, labelItemPath)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLabelCreateIsWritableByAMember(t *testing.T) {
	repo := fakeLabelRepository{
		createFn: func(_ context.Context, p storage.CreateLabelParams) (storage.Label, error) {
			return storage.Label{ID: "lbl-1", Name: p.Name, Color: p.Color, Version: 1}, nil
		},
	}
	cfg := labelTestConfig(repo)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{"name": "Kitchen", "color": "#888888"}); err != nil {
		t.Fatalf("encode request body: %v", err)
	}
	req := asTestUser(httptest.NewRequest(http.MethodPost, labelCollectionPath, &buf), "member")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("POST %s as a member: status = 403, want 201 -- labels are member-writable group data "+
			"(FR-007), not an owner-gated group setting the way /custom-field-defs' DEFINITIONS are: a "+
			"403 here means this route was mounted inside middleware.RequireOwner by mistake", labelCollectionPath)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s as a member: status = %d, want 201; body = %s", labelCollectionPath, rec.Code, rec.Body.String())
	}
}

func TestLabelListIsReadableByAMember(t *testing.T) {
	repo := fakeLabelRepository{
		listFn: func(context.Context) ([]storage.Label, error) {
			return []storage.Label{{ID: "lbl-1", Name: "Kitchen", Color: "#888888", Version: 1}}, nil
		},
	}
	cfg := labelTestConfig(repo)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	req := asTestUser(httptest.NewRequest(http.MethodGet, labelCollectionPath, nil), "member")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("GET %s as a member: status = 403, want 200 -- a member must be able to read the "+
			"group's labels", labelCollectionPath)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as a member: status = %d, want 200; body = %s", labelCollectionPath, rec.Code, rec.Body.String())
	}
}

var _ = []error{
	labels.ErrNameRequired,
	labels.ErrColorInvalid,
	labels.ErrVersionRequired,
}
