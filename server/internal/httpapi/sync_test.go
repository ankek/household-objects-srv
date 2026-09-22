package httpapi

import (
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncPushRouteAnswersNotImplemented(t *testing.T) {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/sync/push")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501: %q", rec.Code, rec.Body.String())
	}
	got := decodeProblem(t, rec)
	want := problem.NotImplemented()
	if got.Type != want.Type || got.Title != want.Title || got.Status != want.Status {
		t.Errorf("body = %+v, want type/title/status matching %+v", got, want)
	}
}

func TestSyncPullRouteIsRealNotAStub(t *testing.T) {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/pull", strings.NewReader(""))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (empty body fails JSON decode): %q", rec.Code, rec.Body.String())
	}
	got := decodeProblem(t, rec)
	if got.Detail == "" {
		t.Errorf("body = %+v, want a non-empty Detail (BadRequest always carries one)", got)
	}
}

func TestSyncRoutesRequireCredentials(t *testing.T) {
	h := newTestRouter(t)

	for _, path := range []string{"/api/v1/sync/pull", "/api/v1/sync/push"} {
		t.Run(path, func(t *testing.T) {
			rec := do(t, h, http.MethodPost, path)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %q", rec.Code, rec.Body.String())
			}
		})
	}
}

func authAsGroup(group string) middleware.Authenticator {
	return middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
		return middleware.Identity{Group: group, UserID: "usr-test", Role: "owner"}, nil
	})
}

func TestSyncPushRevealsNothingThatVariesByGroup(t *testing.T) {
	cfgA := testConfig()
	cfgA.Authenticator = authAsGroup("grp-test-a")
	hA, err := NewRouter(cfgA)
	if err != nil {
		t.Fatalf("NewRouter (group A): %v", err)
	}

	cfgB := testConfig()
	cfgB.Authenticator = authAsGroup("grp-test-b")
	hB, err := NewRouter(cfgB)
	if err != nil {
		t.Fatalf("NewRouter (group B): %v", err)
	}

	recA := do(t, hA, http.MethodPost, "/api/v1/sync/push")
	recB := do(t, hB, http.MethodPost, "/api/v1/sync/push")

	if recA.Code != http.StatusNotImplemented || recB.Code != http.StatusNotImplemented {
		t.Fatalf("status = (A: %d, B: %d), want both 501: A=%q B=%q",
			recA.Code, recB.Code, recA.Body.String(), recB.Body.String())
	}

	gotA, gotB := decodeProblem(t, recA), decodeProblem(t, recB)
	gotA.RequestID, gotB.RequestID = "", ""
	if gotA != gotB {
		t.Errorf("household A's body %+v != household B's body %+v (ignoring request_id) -- "+
			"a stub route revealed something that differs by group", gotA, gotB)
	}
}

func syncIsolationTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(t.Context(), storage.Config{Path: filepath.Join(t.TempDir(), "db", "hho.db"), ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func seedSyncIsolationItem(t *testing.T, s *storage.Storage, groupID string) storage.Item {
	t.Helper()
	if err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    groupID,
		UserID:       groupID + "-owner",
		Username:     groupID + "-owner",
		PasswordHash: "not-a-real-hash",
		Now:          1,
	}); err != nil {
		t.Fatalf("RegisterNewGroup(%s): %v", groupID, err)
	}
	scope, err := s.ForGroup(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup(%s): %v", groupID, err)
	}
	item, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID: groupID + "-item", Name: groupID + " Item", ShortCode: groupID + "-SC", Now: 2,
	})
	if err != nil {
		t.Fatalf("Items().Create(%s): %v", groupID, err)
	}
	return item
}

func syncPullOK(t *testing.T, h http.Handler, deviceID string) syncPullResultBody {
	t.Helper()
	reqBody, err := json.Marshal(syncPullRequestBody{DeviceID: deviceID, Since: 0, Limit: 100})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/pull", strings.NewReader(string(reqBody)))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var result syncPullResultBody
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}
	return result
}

func syncPullContainsID(result syncPullResultBody, id string) bool {
	for _, c := range result.Changes {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestSyncPullDoesNotLeakAcrossGroups(t *testing.T) {
	s := syncIsolationTestStorage(t)
	const groupA, groupB = "grp-sync-iso-a", "grp-sync-iso-b"
	itemA := seedSyncIsolationItem(t, s, groupA)
	itemB := seedSyncIsolationItem(t, s, groupB)

	cfgA := testConfig()
	cfgA.Store, cfgA.Scopes = s, s
	cfgA.Authenticator = authAsGroup(groupA)
	hA, err := NewRouter(cfgA)
	if err != nil {
		t.Fatalf("NewRouter (group A): %v", err)
	}

	cfgB := testConfig()
	cfgB.Store, cfgB.Scopes = s, s
	cfgB.Authenticator = authAsGroup(groupB)
	hB, err := NewRouter(cfgB)
	if err != nil {
		t.Fatalf("NewRouter (group B): %v", err)
	}

	resultA := syncPullOK(t, hA, "dev-a")
	resultB := syncPullOK(t, hB, "dev-b")

	if !syncPullContainsID(resultA, itemA.ID) {
		t.Errorf("group A's own pull is missing its own item %q: %+v", itemA.ID, resultA)
	}
	if syncPullContainsID(resultA, itemB.ID) {
		t.Errorf("group A's pull leaked group B's item %q: %+v", itemB.ID, resultA)
	}
	if !syncPullContainsID(resultB, itemB.ID) {
		t.Errorf("group B's own pull is missing its own item %q: %+v", itemB.ID, resultB)
	}
	if syncPullContainsID(resultB, itemA.ID) {
		t.Errorf("group B's pull leaked group A's item %q: %+v", itemA.ID, resultB)
	}
}
