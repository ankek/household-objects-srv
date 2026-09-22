package httpapi

import (
	"bytes"
	"encoding/json"
	"flag"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "regenerate golden fixture testdata instead of comparing against it")

const syncPullGoldenPath = "testdata/sync_pull_golden.json"

func seedSyncPullGoldenFixture(t *testing.T, s *storage.Storage, groupID string) {
	t.Helper()
	if err := s.RegisterNewGroup(t.Context(), storage.FirstUserParams{
		GroupID:      groupID,
		GroupName:    groupID,
		UserID:       groupID + "-owner",
		Username:     groupID + "-owner",
		PasswordHash: "not-a-real-hash",
		Now:          1,
	}); err != nil {
		t.Fatalf("RegisterNewGroup: %v", err)
	}
	scope, err := s.ForGroup(storage.MustGroupID(groupID))
	if err != nil {
		t.Fatalf("ForGroup: %v", err)
	}

	item1, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID: "golden-item-1", Name: "Golden Lamp", Quantity: 1, ShortCode: "GOLD0001", Now: 2,
	})
	if err != nil {
		t.Fatalf("create item1: %v", err)
	}

	if _, err := scope.Warranty().Create(t.Context(), storage.CreateWarrantyParams{
		ID: "golden-warranty-1", ItemID: item1.ID, Holder: "Golden Warranty Co", Provider: "Acme",
		StartsOn: "2024-01-01", ExpiresOn: "2026-01-01", IsLifetime: false, Notes: "Fixture warranty.",
		Now: 3,
	}); err != nil {
		t.Fatalf("create warranty: %v", err)
	}

	label1, err := scope.Labels().Create(t.Context(), storage.CreateLabelParams{
		ID: "golden-label-1", Name: "Fragile", Color: "#ff0000", Now: 4,
	})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}

	if err := scope.ItemLabels().Attach(t.Context(), storage.AttachLabelParams{
		ID: "golden-itemlabel-1", ItemID: item1.ID, LabelID: label1.ID, Now: 5,
	}); err != nil {
		t.Fatalf("attach label: %v", err)
	}

	item2, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID: "golden-item-2", Name: "Golden Toolbox", Quantity: 1, ShortCode: "GOLD0002", Now: 6,
	})
	if err != nil {
		t.Fatalf("create item2: %v", err)
	}

	if _, err := scope.Attachments().Create(t.Context(), storage.CreateAttachmentParams{
		ID: "golden-attachment-1", ItemID: item2.ID, Category: "manual",
		OriginalFilename: "manual.pdf", ContentType: "application/pdf", SizeBytes: 1024,
		StoragePath: groupID + "/golden-attachment-1", SHA256: strings.Repeat("a", 64), Now: 7,
	}); err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	item3, err := scope.Items().Create(t.Context(), storage.CreateItemParams{
		ID: "golden-item-3", Name: "Golden Trash", Quantity: 1, ShortCode: "GOLD0003", Now: 8,
	})
	if err != nil {
		t.Fatalf("create item3: %v", err)
	}
	if err := scope.Items().Delete(t.Context(), item3.ID, 9); err != nil {
		t.Fatalf("delete item3: %v", err)
	}
}

func TestSyncPullGoldenFixture(t *testing.T) {
	s := syncIsolationTestStorage(t)
	const group = "grp-sync-golden"
	seedSyncPullGoldenFixture(t, s, group)

	cfg := testConfig()
	cfg.Store, cfg.Scopes = s, s
	cfg.Authenticator = authAsGroup(group)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	reqBody, err := json.Marshal(syncPullRequestBody{DeviceID: "golden-device", Since: 0, Limit: 100})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/pull", strings.NewReader(string(reqBody)))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var indented bytes.Buffer
	if err := json.Indent(&indented, rec.Body.Bytes(), "", "  "); err != nil {
		t.Fatalf("indent response body: %v", err)
	}
	indented.WriteByte('\n')

	if *updateGolden {
		if err := os.WriteFile(syncPullGoldenPath, indented.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden fixture: %v", err)
		}
		t.Logf("regenerated %s", syncPullGoldenPath)
		return
	}

	want, err := os.ReadFile(syncPullGoldenPath)
	if err != nil {
		t.Fatalf("read golden fixture (run with -update to create it): %v", err)
	}
	if indented.String() != string(want) {
		t.Errorf("response does not match %s (run with -update to refresh it if this change is intentional)\n--- got ---\n%s\n--- want ---\n%s",
			syncPullGoldenPath, indented.String(), string(want))
	}
}

func TestSyncPullGoldenFixtureDirectoryExists(t *testing.T) {
	info, err := os.Stat(filepath.Dir(syncPullGoldenPath))
	if err != nil {
		t.Fatalf("stat %s: %v", filepath.Dir(syncPullGoldenPath), err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", filepath.Dir(syncPullGoldenPath))
	}
}
