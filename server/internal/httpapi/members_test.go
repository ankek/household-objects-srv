package httpapi

import (
	"context"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeMemberRepository struct {
	listFn func(ctx context.Context) ([]storage.Member, error)
}

func (f fakeMemberRepository) List(ctx context.Context) ([]storage.Member, error) {
	return f.listFn(ctx)
}

type membersFakeScope struct {
	group storage.GroupID
	repo  storage.MemberRepository
}

func (s membersFakeScope) GroupID() storage.GroupID                          { return s.group }
func (s membersFakeScope) Items() storage.ItemRepository                     { return nil }
func (s membersFakeScope) Warranty() storage.WarrantyRepository              { return nil }
func (s membersFakeScope) Sale() storage.SaleRepository                      { return nil }
func (s membersFakeScope) Purchase() storage.PurchaseRepository              { return nil }
func (s membersFakeScope) Visibility() storage.GroupVisibilityRepository     { return nil }
func (s membersFakeScope) Identifications() storage.IdentificationRepository { return nil }
func (s membersFakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }
func (s membersFakeScope) ItemCustomFields() storage.ItemCustomFieldRepository {
	return nil
}
func (s membersFakeScope) StockAdjustments() storage.StockAdjustmentRepository {
	return nil
}
func (s membersFakeScope) Locations() storage.LocationRepository     { return nil }
func (s membersFakeScope) Labels() storage.LabelRepository           { return nil }
func (s membersFakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (s membersFakeScope) Attachments() storage.AttachmentRepository { return nil }
func (s membersFakeScope) Members() storage.MemberRepository         { return s.repo }

type membersFakeScopes struct {
	repo storage.MemberRepository
}

func (s membersFakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) {
	return membersFakeScope{group: g, repo: s.repo}, nil
}

func membersTestConfig(repo storage.MemberRepository) Config {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	cfg.Scopes = membersFakeScopes{repo: repo}
	return cfg
}

const groupMembersPath = "/api/v1/groups/members"

func TestMemberListReturnsTheRosterAsJSON(t *testing.T) {
	repo := fakeMemberRepository{
		listFn: func(context.Context) ([]storage.Member, error) {
			return []storage.Member{
				{ID: "usr-owner", Username: "alice", Role: "owner", JoinedAtUnixMilli: 1_700_000_000_000},
				{ID: "usr-member", Username: "bob", Role: "member", JoinedAtUnixMilli: 1_700_000_100_000},
			}, nil
		},
	}
	cfg := membersTestConfig(repo)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, groupMembersPath, nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200: %s", groupMembersPath, rec.Code, rec.Body.String())
	}
	var got groupMembersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	want := groupMembersResponse{Members: []groupMemberListItem{
		{ID: "usr-owner", Username: "alice", Role: "owner", JoinedAt: 1_700_000_000_000},
		{ID: "usr-member", Username: "bob", Role: "member", JoinedAt: 1_700_000_100_000},
	}}
	if len(got.Members) != len(want.Members) {
		t.Fatalf("GET %s: got %d members, want %d: %+v", groupMembersPath, len(got.Members), len(want.Members), got)
	}
	for i := range want.Members {
		if got.Members[i] != want.Members[i] {
			t.Errorf("GET %s: member[%d] = %+v, want %+v", groupMembersPath, i, got.Members[i], want.Members[i])
		}
	}
}

func TestMemberListReturnsEmptyArrayNeverNull(t *testing.T) {
	repo := fakeMemberRepository{
		listFn: func(context.Context) ([]storage.Member, error) { return nil, nil },
	}
	cfg := membersTestConfig(repo)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, groupMembersPath, nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200: %s", groupMembersPath, rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, `"members":[]`) {
		t.Errorf(`GET %s body = %s, want a "members":[] array, not null`, groupMembersPath, got)
	}
}

func TestMemberListIsReadableByAMember(t *testing.T) {
	repo := fakeMemberRepository{
		listFn: func(context.Context) ([]storage.Member, error) {
			return []storage.Member{
				{ID: ownerUserID, Username: "owner", Role: "owner", JoinedAtUnixMilli: 1},
				{ID: memberUserID, Username: "member", Role: "member", JoinedAtUnixMilli: 2},
			}, nil
		},
	}
	cfg := membersTestConfig(repo)
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := asTestUser(httptest.NewRequest(http.MethodGet, groupMembersPath, nil), "member")
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("GET %s as a member: status = 403, want 200 -- this route is deliberately "+
			"member-readable (members.go's own file doc, \"Who may read\"); a 403 here means it "+
			"was mounted inside a RequireOwner sub-group instead of the general authenticated group",
			groupMembersPath)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as a member: status = %d, want %d; body = %s",
			groupMembersPath, rec.Code, http.StatusOK, rec.Body.String())
	}
}
