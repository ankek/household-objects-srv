package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncPushRouteIsRealNotAStub(t *testing.T) {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/push", strings.NewReader(""))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (empty body fails JSON decode): %q", rec.Code, rec.Body.String())
	}
	got := decodeProblem(t, rec)
	if got.Detail == "" {
		t.Errorf("body = %+v, want a non-empty Detail (BadRequest always carries one)", got)
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

	if recA.Code != http.StatusBadRequest || recB.Code != http.StatusBadRequest {
		t.Fatalf("status = (A: %d, B: %d), want both 400: A=%q B=%q",
			recA.Code, recB.Code, recA.Body.String(), recB.Body.String())
	}

	gotA, gotB := decodeProblem(t, recA), decodeProblem(t, recB)
	gotA.RequestID, gotB.RequestID = "", ""
	if gotA != gotB {
		t.Errorf("household A's body %+v != household B's body %+v (ignoring request_id) -- "+
			"a request that never reached per-group data revealed something that differs by group", gotA, gotB)
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

func syncPushTestRouter(t *testing.T, groupID string) http.Handler {
	t.Helper()
	s := syncIsolationTestStorage(t)
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

	cfg := testConfig()
	cfg.Store, cfg.Scopes = s, s
	cfg.Authenticator = authAsGroup(groupID)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return h
}

func doSyncPush(t *testing.T, h http.Handler, req syncPushRequestBody) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/sync/push", strings.NewReader(string(raw)))
	h.ServeHTTP(rec, httpReq)
	return rec
}

func pushField(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal field: %v", err)
	}
	return raw
}

func TestSyncPushRequiresDeviceID(t *testing.T) {
	h := syncPushTestRouter(t, "grp-push-devid")

	rec := doSyncPush(t, h, syncPushRequestBody{DeviceID: "", Mutations: nil})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %q", rec.Code, rec.Body.String())
	}
	got := decodeProblem(t, rec)
	if got.Detail != "device_id is required" {
		t.Errorf("detail = %q, want %q", got.Detail, "device_id is required")
	}
}

func TestSyncPushRequiresMutationFields(t *testing.T) {
	tests := []struct {
		name       string
		mutation   syncPushMutationBody
		wantDetail string
	}{
		{
			name:       "missing mutation_id",
			mutation:   syncPushMutationBody{EntityType: "item", EntityID: "e1", BaseVersion: 0, Fields: map[string]json.RawMessage{"name": pushField(t, "Foo")}},
			wantDetail: `mutations[0] (mutation_id=""): mutation_id is required`,
		},
		{
			name:       "missing entity_type",
			mutation:   syncPushMutationBody{MutationID: "mut-1", EntityID: "e1", BaseVersion: 0, Fields: map[string]json.RawMessage{}},
			wantDetail: `mutations[0] (mutation_id="mut-1"): entity_type is required`,
		},
		{
			name:       "missing entity_id",
			mutation:   syncPushMutationBody{MutationID: "mut-1", EntityType: "item", BaseVersion: 0, Fields: map[string]json.RawMessage{}},
			wantDetail: `mutations[0] (mutation_id="mut-1"): entity_id is required`,
		},
		{
			name:       "negative base_version",
			mutation:   syncPushMutationBody{MutationID: "mut-1", EntityType: "item", EntityID: "e1", BaseVersion: -1, Fields: map[string]json.RawMessage{}},
			wantDetail: `mutations[0] (mutation_id="mut-1"): base_version must not be negative`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := syncPushTestRouter(t, "grp-push-reqfields-"+strings.ReplaceAll(tt.name, " ", "-"))

			rec := doSyncPush(t, h, syncPushRequestBody{DeviceID: "dev-1", Mutations: []syncPushMutationBody{tt.mutation}})

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %q", rec.Code, rec.Body.String())
			}
			got := decodeProblem(t, rec)
			if got.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", got.Detail, tt.wantDetail)
			}
		})
	}
}

func TestSyncPushRejectsInvalidUpperLayerFieldValue(t *testing.T) {
	tests := []struct {
		name       string
		mutation   syncPushMutationBody
		wantSubstr string
	}{
		{
			name: "unknown identification kind",
			mutation: syncPushMutationBody{
				MutationID: "mut-kind", EntityType: "item_identification", EntityID: "id-1", BaseVersion: 0,
				Fields: map[string]json.RawMessage{
					"item_id": pushField(t, "itm-1"),
					"kind":    pushField(t, "not-a-real-kind"),
					"value":   pushField(t, "12345"),
				},
			},
			wantSubstr: "kind must be one of serial, model, asset_tag, barcode, other",
		},
		{
			name: "malformed label colour",
			mutation: syncPushMutationBody{
				MutationID: "mut-color", EntityType: "label", EntityID: "lbl-1", BaseVersion: 0,
				Fields: map[string]json.RawMessage{
					"name":  pushField(t, "Kitchen"),
					"color": pushField(t, "not-a-color"),
				},
			},
			wantSubstr: "color must be a 6-digit hex triplet",
		},
		{
			name: "malformed warranty date",
			mutation: syncPushMutationBody{
				MutationID: "mut-date", EntityType: "warranty_block", EntityID: "w-1", BaseVersion: 0,
				Fields: map[string]json.RawMessage{
					"item_id":     pushField(t, "itm-1"),
					"starts_on":   pushField(t, "2026-8-5"),
					"is_lifetime": pushField(t, false),
				},
			},
			wantSubstr: "warranty dates must be ISO-8601 calendar days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := syncPushTestRouter(t, "grp-push-fieldval-"+strings.ReplaceAll(tt.name, " ", "-"))

			rec := doSyncPush(t, h, syncPushRequestBody{DeviceID: "dev-1", Mutations: []syncPushMutationBody{tt.mutation}})

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %q", rec.Code, rec.Body.String())
			}
			got := decodeProblem(t, rec)
			if !strings.Contains(got.Detail, tt.wantSubstr) {
				t.Errorf("detail = %q, want it to contain %q", got.Detail, tt.wantSubstr)
			}
			if !strings.Contains(got.Detail, tt.mutation.MutationID) {
				t.Errorf("detail = %q, want it to name mutation_id %q", got.Detail, tt.mutation.MutationID)
			}
			if !strings.Contains(got.Detail, "mutations[0]") {
				t.Errorf("detail = %q, want it to name the mutation's batch index", got.Detail)
			}
		})
	}
}

func TestSyncPushStructuralErrorAnswers400(t *testing.T) {
	h := syncPushTestRouter(t, "grp-push-structural")

	rec := doSyncPush(t, h, syncPushRequestBody{
		DeviceID: "dev-1",
		Mutations: []syncPushMutationBody{
			{MutationID: "mut-attach", EntityType: "attachment", EntityID: "att-1", BaseVersion: 0, Fields: map[string]json.RawMessage{}},
		},
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %q", rec.Code, rec.Body.String())
	}
	got := decodeProblem(t, rec)
	if !strings.Contains(got.Detail, "mut-attach") || !strings.Contains(got.Detail, "mutations[0]") {
		t.Errorf("detail = %q, want it to name mutations[0] and mutation_id mut-attach", got.Detail)
	}
}

type fakeFailingPushCommit struct {
	err error
}

func (f fakeFailingPushCommit) ApplyMutation(context.Context, storage.PushMutation) (storage.PushOutcome, error) {
	return storage.PushOutcome{}, f.err
}

func (f fakeFailingPushCommit) Watermark(context.Context) (int64, error) { return 0, nil }

func TestSyncPushNonStructuralApplyMutationErrorAnswers500(t *testing.T) {
	const injectedText = "disk I/O error reading the mutation ledger"

	orig := syncPushCommitRepositoryFor
	syncPushCommitRepositoryFor = func(cfg Config, scope storage.Scope) (storage.PushCommitRepository, error) {
		return fakeFailingPushCommit{err: errors.New("storage: push: check mutation ledger for \"mut-1\": " + injectedText)}, nil
	}
	t.Cleanup(func() { syncPushCommitRepositoryFor = orig })

	h := syncPushTestRouter(t, "grp-push-nonstructural-500")

	rec := doSyncPush(t, h, syncPushRequestBody{
		DeviceID: "dev-1",
		Mutations: []syncPushMutationBody{
			{MutationID: "mut-1", EntityType: "item", EntityID: "item-1", BaseVersion: 0, Fields: map[string]json.RawMessage{
				"name":        pushField(t, "Drill"),
				"description": pushField(t, ""),
				"location_id": pushField(t, ""),
				"quantity":    pushField(t, 1),
			}},
		},
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "storage:") {
		t.Errorf("body = %q, must not leak the storage-internal \"storage:\" prefix", body)
	}
	if strings.Contains(body, injectedText) {
		t.Errorf("body = %q, must not leak the injected error text %q", body, injectedText)
	}
	if strings.Contains(body, "mut-1") {
		t.Errorf("body = %q, must not name the mutation (that is the 400 path's own contract, not 500's)", body)
	}
}

func TestSyncPushCreatesItemAndReturnsApplied(t *testing.T) {
	h := syncPushTestRouter(t, "grp-push-create")

	const entityID = "itm-push-create-1"
	rec := doSyncPush(t, h, syncPushRequestBody{
		DeviceID: "dev-1",
		Mutations: []syncPushMutationBody{
			{
				MutationID: "mut-create-1", EntityType: "item", EntityID: entityID, BaseVersion: 0,
				Fields: map[string]json.RawMessage{
					"name":        pushField(t, "Pushed Item"),
					"description": pushField(t, ""),
					"location_id": pushField(t, ""),
					"quantity":    pushField(t, 1),
				},
			},
		},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %q", rec.Code, rec.Body.String())
	}

	var got syncPushResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}

	if len(got.Applied) != 1 {
		t.Fatalf("applied = %+v, want exactly one entry", got.Applied)
	}
	applied := got.Applied[0]
	if applied.MutationID != "mut-create-1" || applied.EntityType != "item" || applied.EntityID != entityID {
		t.Errorf("applied[0] = %+v, want mutation_id=mut-create-1 entity_type=item entity_id=%s", applied, entityID)
	}
	if applied.Version != 1 {
		t.Errorf("applied[0].Version = %d, want 1 (a fresh create's first version)", applied.Version)
	}
	if len(got.Skipped) != 0 {
		t.Errorf("skipped = %+v, want none", got.Skipped)
	}
	if len(got.Conflicts) != 0 {
		t.Errorf("conflicts = %+v, want none", got.Conflicts)
	}
	if got.NewWatermark <= 0 {
		t.Errorf("new_watermark = %d, want a positive change_seq watermark after this write", got.NewWatermark)
	}
}
