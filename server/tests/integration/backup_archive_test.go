package integration

import (
	"archive/tar"
	"compress/gzip"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestTenantIsolationBackupIncludesEveryGroupsData(t *testing.T) {
	f := newCrossTenantFixture(t)

	rec := f.request(t, cookieCredential, http.MethodGet, "/api/v1/backup", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s GET /api/v1/backup: status = %d, want 200: %s", f.b.name, rec.Code, rec.Body.String())
	}

	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("response body is not a valid gzip stream: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)

	const dbEntryName = "db/hho.db"
	var dbData []byte
	for {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("read tar entries looking for %q: %v (ran out of entries first)", dbEntryName, err)
		}
		if hdr.Name != dbEntryName {
			continue
		}
		dbData, err = io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %q entry body: %v", dbEntryName, err)
		}
		break
	}
	if dbData == nil {
		t.Fatalf("archive has no %q entry -- cannot inspect the snapshot for cross-group data", dbEntryName)
	}

	dbPath := filepath.Join(t.TempDir(), "extracted.db")
	if err := os.WriteFile(dbPath, dbData, 0o600); err != nil {
		t.Fatalf("write extracted db entry: %v", err)
	}
	restored, err := storage.Open(t.Context(), storage.Config{Path: dbPath, ReadConns: 2})
	if err != nil {
		t.Fatalf("storage.Open(extracted archive db): %v", err)
	}
	defer func() {
		if err := restored.Close(); err != nil {
			t.Errorf("Close(restored): %v", err)
		}
	}()

	for _, tn := range []tenant{f.a, f.b} {
		scope, err := restored.ForGroup(storage.MustGroupID(tn.groupID))
		if err != nil {
			t.Fatalf("ForGroup(%s, %s) on extracted archive db: %v", tn.name, tn.groupID, err)
		}
		got, err := scope.Items().Get(t.Context(), tn.itemID)
		if err != nil {
			t.Fatalf("%s's item %q missing from the backup archive database entry (err = %v) -- "+
				"GET /api/v1/backup no longer returns every group's data, which contradicts FR-132's "+
				"stated design and this suite's own tenant_isolation_coverage.txt line for this route",
				tn.name, tn.itemID, err)
		}
		if got.Name != tn.itemName {
			t.Errorf("%s's item in the backup archive has Name = %q, want %q", tn.name, got.Name, tn.itemName)
		}
	}
}
