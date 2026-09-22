package httpapi

import (
	"context"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeGroupVisibilityRepository struct {
	getFn    func(ctx context.Context) (storage.GroupVisibility, error)
	updateFn func(ctx context.Context, p storage.UpdateGroupVisibilityParams) (storage.GroupVisibility, error)
}

func (f fakeGroupVisibilityRepository) Get(ctx context.Context) (storage.GroupVisibility, error) {
	return f.getFn(ctx)
}

func (f fakeGroupVisibilityRepository) Update(ctx context.Context, p storage.UpdateGroupVisibilityParams) (storage.GroupVisibility, error) {
	return f.updateFn(ctx, p)
}

type groupVisibilityFakeScope struct {
	group storage.GroupID
	repo  storage.GroupVisibilityRepository
}

func (s groupVisibilityFakeScope) GroupID() storage.GroupID                          { return s.group }
func (s groupVisibilityFakeScope) Items() storage.ItemRepository                     { return nil }
func (s groupVisibilityFakeScope) Warranty() storage.WarrantyRepository              { return nil }
func (s groupVisibilityFakeScope) Sale() storage.SaleRepository                      { return nil }
func (s groupVisibilityFakeScope) Purchase() storage.PurchaseRepository              { return nil }
func (s groupVisibilityFakeScope) Visibility() storage.GroupVisibilityRepository     { return s.repo }
func (s groupVisibilityFakeScope) Identifications() storage.IdentificationRepository { return nil }
func (s groupVisibilityFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }
func (s groupVisibilityFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository {
	return nil
}
func (s groupVisibilityFakeScope) StockAdjustments() storage.StockAdjustmentRepository {
	return nil
}
func (s groupVisibilityFakeScope) Locations() storage.LocationRepository { return nil }

func (s groupVisibilityFakeScope) Labels() storage.LabelRepository           { return nil }
func (s groupVisibilityFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s groupVisibilityFakeScope) Attachments() storage.AttachmentRepository { return nil }

func (s groupVisibilityFakeScope) Members() storage.MemberRepository { return nil }

type groupVisibilityFakeScopes struct {
	repo storage.GroupVisibilityRepository
}

func (s groupVisibilityFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return groupVisibilityFakeScope{group: g, repo: s.repo}, nil
}

func groupVisibilityTestConfig(repo storage.GroupVisibilityRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = groupVisibilityFakeScopes{repo: repo}
	return cfg
}

const groupVisibilityPath = "/api/v1/groups/detail-visibility"

func TestGroupVisibilityGetIsReadableByAMember(t *testing.T) {
	repo := fakeGroupVisibilityRepository{
		getFn: func(context.Context) (storage.GroupVisibility, error) {
			return storage.GroupVisibility{WarrantyVisible: true, SaleVisible: false, PurchaseVisible: true, Version: 1}, nil
		},
	}
	cfg := groupVisibilityTestConfig(repo)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, groupVisibilityPath, nil), "member")
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("GET %s as a member: status = 403, want 200 -- A101.1 makes the READ side "+
			"member-readable (FR-011 visibility gates every item screen's widgets for every "+
			"member, not just the owner); a 403 here means the route was mounted inside the "+
			"RequireOwner sub-group beside PUT", groupVisibilityPath)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as a member: status = %d, want %d; body = %s",
			groupVisibilityPath, rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestGroupVisibilityPutRejectsAMember(t *testing.T) {
	repo := fakeGroupVisibilityRepository{
		updateFn: func(context.Context, storage.UpdateGroupVisibilityParams) (storage.GroupVisibility, error) {
			t.Fatal("Update must not be reached: a member's PUT is rejected by RequireOwner before any handler runs")
			return storage.GroupVisibility{}, nil
		},
	}
	cfg := groupVisibilityTestConfig(repo)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodPut, groupVisibilityPath, nil), "member")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("PUT %s as a member: status = %d, want 403", groupVisibilityPath, rec.Code)
	}
}
