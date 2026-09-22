package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"io"
	"io/fs"
	_ "modernc.org/sqlite"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBackupRestoreCycleRoundTripsThroughRealBinaries(t *testing.T) {
	binPath := buildHHOBinary(t)

	dataDir := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "hho-backup-under-test.tar.gz")

	seedProc := startHHOServe(t, binPath, dataDir, true)
	seedBaseURL := "http://" + seedProc.addr

	alice := seedHousehold(t, seedBaseURL, "alice-t117", "alice-t117-password-1")
	bob := seedHousehold(t, seedBaseURL, "bob-t117", "bob-t117-password-1")
	if alice.groupID == "" || bob.groupID == "" {
		t.Fatalf("seeded households have an empty group id: alice=%q bob=%q", alice.groupID, bob.groupID)
	}
	if alice.groupID == bob.groupID {
		t.Fatalf("alice and bob landed in the SAME group (%q); this test proves nothing about a "+
			"multi-tenant restore without two independent households", alice.groupID)
	}

	seedProc.stop(t)

	dbPath := filepath.Join(dataDir, "db", "hho.db")
	attachmentsDir := filepath.Join(dataDir, "attachments")
	countsBefore := tableRowCounts(t, dbPath)
	attachmentsBefore := attachmentDigests(t, attachmentsDir)

	if countsBefore["groups"] < 2 {
		t.Fatalf("seed produced groups = %d, want >= 2: %v", countsBefore["groups"], countsBefore)
	}
	if countsBefore["items"] < 2 {
		t.Fatalf("seed produced items = %d, want >= 2 (one per household): %v", countsBefore["items"], countsBefore)
	}
	if len(attachmentsBefore) < 2 {
		t.Fatalf("seed produced %d attachment file(s) on disk under %s, want >= 2 (one per "+
			"household, plus its generated thumbnail): %v", len(attachmentsBefore), attachmentsDir,
			attachmentRelPaths(attachmentsBefore))
	}

	runHHOOneShot(t, binPath, "hho backup", []string{
		"backup", "--data-dir", dataDir, "--output", archivePath,
	})
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("stat archive %s produced by hho backup: %v", archivePath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("hho backup produced an empty archive at %s", archivePath)
	}

	wipeDataDir(t, dataDir)
	assertGenuinelyWiped(t, dbPath, attachmentsDir)

	runHHOOneShot(t, binPath, "hho restore", []string{
		"restore", "--data-dir", dataDir, "--input", archivePath,
	})

	countsAfter := tableRowCounts(t, dbPath)
	compareRowCounts(t, countsBefore, countsAfter)

	attachmentsAfter := attachmentDigests(t, attachmentsDir)
	compareAttachmentDigests(t, attachmentsBefore, attachmentsAfter)

	restoredProc := startHHOServe(t, binPath, dataDir, false)
	defer restoredProc.stop(t)
	restoredBaseURL := "http://" + restoredProc.addr

	verifyHouseholdOverAPI(t, restoredBaseURL, alice)
	verifyHouseholdOverAPI(t, restoredBaseURL, bob)
}

type hhoServeProcess struct {
	cmd            *exec.Cmd
	addr           string
	stdout, stderr bytes.Buffer
	stopped        bool
}

func startHHOServe(t *testing.T, binPath, dataDir string, registrationOpen bool) *hhoServeProcess {
	t.Helper()
	addr := freeLoopbackAddr(t)

	cmd := exec.Command(binPath, "serve", "--addr", addr)
	registration := "false"
	if registrationOpen {
		registration = "true"
	}
	cmd.Env = append(os.Environ(),
		"HHO_DATA_DIR="+dataDir,
		"HHO_REGISTRATION_OPEN="+registration,
		"HHO_BACKUP_INTERVAL_SECONDS=0",
	)

	p := &hhoServeProcess{addr: addr}
	cmd.Stdout = &p.stdout
	cmd.Stderr = &p.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start hho serve --addr %s against %s: %v", addr, dataDir, err)
	}
	p.cmd = cmd

	t.Cleanup(func() {
		if p.stopped {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
	})

	waitForHTTPOK(t, "http://"+addr+"/api/v1/status", 10*time.Second)
	return p
}

func (p *hhoServeProcess) stop(t *testing.T) {
	t.Helper()
	if p.stopped {
		return
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal hho serve (pid %d) to stop: %v", p.cmd.Process.Pid, err)
	}
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		p.stopped = true
		if err != nil {
			t.Fatalf("hho serve did not exit cleanly after SIGTERM: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
				err, p.stdout.String(), p.stderr.String())
		}
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		p.stopped = true
		t.Fatalf("hho serve did not exit within 10s of SIGTERM\n--- stdout ---\n%s\n--- stderr ---\n%s",
			p.stdout.String(), p.stderr.String())
	}
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve an ephemeral loopback port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release ephemeral port probe %s: %v", addr, err)
	}
	return addr
}

func waitForHTTPOK(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // url is a loopback address this test itself constructed
		if err != nil {
			lastErr = err
		} else {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never answered 200 within %s: %v", url, timeout, lastErr)
}

func runHHOOneShot(t *testing.T, binPath, label string, args []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s (%s): %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
			label, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
}

func wipeDataDir(t *testing.T, dataDir string) {
	t.Helper()
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatalf("wipe data directory %s: %v", dataDir, err)
	}
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatalf("recreate empty data directory root %s: %v", dataDir, err)
	}
}

func assertGenuinelyWiped(t *testing.T, dbPath, attachmentsDir string) {
	t.Helper()
	if _, err := os.Stat(dbPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("wipe left %s in place (stat err = %v); the wipe is not genuine", dbPath, err)
	}
	if entries, err := os.ReadDir(attachmentsDir); err == nil {
		if len(entries) != 0 {
			t.Fatalf("wipe left %d entr(y/ies) under %s; the wipe is not genuine", len(entries), attachmentsDir)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat attachments dir %s after wipe: %v", attachmentsDir, err)
	}
}

func tableRowCounts(t *testing.T, dbPath string) map[string]int64 {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open %s to count rows: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	rows, err := db.Query(`
		SELECT name FROM pragma_table_list
		WHERE schema = 'main'
		  AND type IN ('table', 'virtual')
		  AND name NOT LIKE 'sqlite_%'
		  AND name <> 'goose_db_version'
		ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables in %s: %v", dbPath, err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			t.Fatalf("scan table name from %s: %v", dbPath, err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables in %s: %v", dbPath, err)
	}
	_ = rows.Close()
	if len(tables) == 0 {
		t.Fatalf("enumerated zero tables in %s; the introspection query is broken and this test asserts nothing", dbPath)
	}

	counts := make(map[string]int64, len(tables))
	for _, tbl := range tables {
		var n int64
		if err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %q`, tbl)).Scan(&n); err != nil {
			t.Fatalf("count rows in %s.%s: %v", dbPath, tbl, err)
		}
		counts[tbl] = n
	}
	return counts
}

func compareRowCounts(t *testing.T, before, after map[string]int64) {
	t.Helper()

	tables := make(map[string]struct{}, len(before)+len(after))
	for tbl := range before {
		tables[tbl] = struct{}{}
	}
	for tbl := range after {
		tables[tbl] = struct{}{}
	}
	if len(tables) < 15 {
		t.Fatalf("enumerated only %d distinct table(s) across before/after; want at least 15 -- "+
			"the schema introspection is not seeing the real table set: %v", len(tables), tables)
	}

	sorted := make([]string, 0, len(tables))
	for tbl := range tables {
		sorted = append(sorted, tbl)
	}
	sort.Strings(sorted)

	mismatches := 0
	for _, tbl := range sorted {
		b, bOK := before[tbl]
		a, aOK := after[tbl]
		switch {
		case !bOK:
			t.Errorf("table %q exists in the RESTORED database but not in the pre-backup one", tbl)
			mismatches++
		case !aOK:
			t.Errorf("table %q existed in the pre-backup database but is MISSING from the restored one", tbl)
			mismatches++
		case a != b:
			t.Errorf("table %q: restored row count = %d, want %d (the pre-backup count)", tbl, a, b)
			mismatches++
		}
	}
	if mismatches == 0 {
		t.Logf("row counts match across all %d tables: %v", len(sorted), before)
	}
}

func attachmentDigests(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	digests := make(map[string][32]byte)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", path, readErr)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return fmt.Errorf("relativize %s against %s: %w", path, root, relErr)
		}
		digests[rel] = sha256.Sum256(content)
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return digests
		}
		t.Fatalf("walk attachments tree %s: %v", root, err)
	}
	return digests
}

func attachmentRelPaths(digests map[string][32]byte) []string {
	paths := make([]string, 0, len(digests))
	for p := range digests {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func compareAttachmentDigests(t *testing.T, before, after map[string][32]byte) {
	t.Helper()
	if len(before) == 0 {
		t.Fatalf("attachmentDigests(before) is empty; this comparison would pass vacuously")
	}

	all := make(map[string]struct{}, len(before)+len(after))
	for p := range before {
		all[p] = struct{}{}
	}
	for p := range after {
		all[p] = struct{}{}
	}
	sorted := make([]string, 0, len(all))
	for p := range all {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	mismatches := 0
	for _, p := range sorted {
		b, bOK := before[p]
		a, aOK := after[p]
		switch {
		case !bOK:
			t.Errorf("attachment %q exists after restore but did not exist before backup", p)
			mismatches++
		case !aOK:
			t.Errorf("attachment %q existed before backup but is MISSING after restore", p)
			mismatches++
		case a != b:
			t.Errorf("attachment %q: restored content's sha256 does not match the pre-backup "+
				"file's -- content was altered or truncated by the backup/restore cycle", p)
			mismatches++
		}
	}
	if mismatches == 0 {
		t.Logf("all %d attachment file(s) round-tripped byte-for-byte: %v", len(sorted), attachmentRelPaths(before))
	}
}

type seededHousehold struct {
	username, password  string
	groupID, userID     string
	itemID              string
	warrantyHolder      string
	saleBuyer           string
	purchaseVendor      string
	identificationValue string
	customFieldValue    string
	labelName           string
	locationName        string
	attachmentID        string
	attachmentContent   []byte
}

func seedHousehold(t *testing.T, baseURL, username, password string) seededHousehold {
	t.Helper()
	hh := seededHousehold{username: username, password: password}

	status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/auth/register", "", regBody(username, password))
	if status != http.StatusCreated {
		t.Fatalf("register %s: status = %d: %s", username, status, body)
	}
	var reg registerResponse
	mustDecodeBytes(t, body, &reg)
	hh.groupID, hh.userID = reg.GroupID, reg.UserID

	status, headers, body := netDo(t, baseURL, http.MethodPost, "/api/v1/auth/login", "", regBody(username, password))
	if status != http.StatusOK {
		t.Fatalf("login %s: status = %d: %s", username, status, body)
	}
	cookie := sessionCookieFromHeader(t, headers)

	status, _, body = netDo(t, baseURL, http.MethodPost, "/api/v1/items", cookie, itemCreateBody(username+"'s ladder"))
	if status != http.StatusCreated {
		t.Fatalf("%s create item: status = %d: %s", username, status, body)
	}
	var item itemResponse
	mustDecodeBytes(t, body, &item)
	hh.itemID = item.ID

	hh.warrantyHolder = username + "'s manufacturer"
	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/items/"+hh.itemID+"/warranty", cookie,
		warrantyCreateBody(hh.warrantyHolder)); status != http.StatusCreated {
		t.Fatalf("%s create warranty: status = %d: %s", username, status, body)
	}

	hh.saleBuyer = username + "'s buyer"
	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/items/"+hh.itemID+"/sale", cookie,
		saleCreateBody(hh.saleBuyer)); status != http.StatusCreated {
		t.Fatalf("%s create sale: status = %d: %s", username, status, body)
	}

	hh.purchaseVendor = username + "'s vendor"
	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/items/"+hh.itemID+"/purchase", cookie,
		purchaseCreateBody(hh.purchaseVendor)); status != http.StatusCreated {
		t.Fatalf("%s create purchase: status = %d: %s", username, status, body)
	}

	hh.identificationValue = username + "-serial-0001"
	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/items/"+hh.itemID+"/identifications", cookie,
		identificationCreateBody("serial", hh.identificationValue)); status != http.StatusCreated {
		t.Fatalf("%s create identification: status = %d: %s", username, status, body)
	}

	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/custom-field-defs", cookie,
		customFieldDefCreateBody(username+"'s color field", "text", 1)); status != http.StatusCreated {
		t.Fatalf("%s create custom field def: status = %d: %s", username, status, body)
	}
	hh.customFieldValue = username + "-copper"
	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/items/"+hh.itemID+"/custom-fields", cookie,
		itemCustomFieldCreateBody("color", hh.customFieldValue)); status != http.StatusCreated {
		t.Fatalf("%s create item custom field: status = %d: %s", username, status, body)
	}

	hh.labelName = username + "'s fragile"
	status, _, body = netDo(t, baseURL, http.MethodPost, "/api/v1/labels", cookie, labelCreateBody(hh.labelName, "#ff00aa"))
	if status != http.StatusCreated {
		t.Fatalf("%s create label: status = %d: %s", username, status, body)
	}
	var label labelResponse
	mustDecodeBytes(t, body, &label)
	if status, _, body := netDo(t, baseURL, http.MethodPut, "/api/v1/items/"+hh.itemID+"/labels/"+label.ID, cookie, nil); status != http.StatusNoContent {
		t.Fatalf("%s assign label to item: status = %d: %s", username, status, body)
	}

	hh.locationName = username + "'s garage"
	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/locations", cookie,
		locationCreateBody(hh.locationName, "")); status != http.StatusCreated {
		t.Fatalf("%s create location: status = %d: %s", username, status, body)
	}

	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/items/"+hh.itemID+"/stock-adjustments", cookie,
		stockAdjustmentCreateBody(3, "restock", username+"'s adjustment")); status != http.StatusCreated {
		t.Fatalf("%s create stock adjustment: status = %d: %s", username, status, body)
	}

	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/invites", cookie, nil); status != http.StatusCreated {
		t.Fatalf("%s create invite: status = %d: %s", username, status, body)
	}

	deviceTokenBody := fmt.Sprintf(`{"device_label":%q}`, username+"'s phone")
	if status, _, body := netDo(t, baseURL, http.MethodPost, "/api/v1/auth/device-tokens", cookie,
		[]byte(deviceTokenBody)); status != http.StatusCreated {
		t.Fatalf("%s issue device token: status = %d: %s", username, status, body)
	}

	hh.attachmentContent = testImageJPEGBytes(t)
	multipartBody, contentType := attachmentUploadMultipartBody(t, attachments.CategoryImage, username+"-photo.jpg", hh.attachmentContent)
	status, _, body = netDoMultipart(t, baseURL, http.MethodPost, "/api/v1/items/"+hh.itemID+"/attachments", cookie, multipartBody, contentType)
	if status != http.StatusCreated {
		t.Fatalf("%s upload attachment: status = %d: %s", username, status, body)
	}
	var att attachmentResponse
	mustDecodeBytes(t, body, &att)
	if !att.HasThumbnail {
		t.Fatalf("%s's uploaded attachment has has_thumbnail = false, want true -- testImageJPEGBytes "+
			"must be a genuinely decodable image for the restored-thumbnail comparison to mean anything", username)
	}
	hh.attachmentID = att.ID

	return hh
}

func verifyHouseholdOverAPI(t *testing.T, baseURL string, hh seededHousehold) {
	t.Helper()

	status, headers, body := netDo(t, baseURL, http.MethodPost, "/api/v1/auth/login", "", regBody(hh.username, hh.password))
	if status != http.StatusOK {
		t.Fatalf("post-restore login as %s: status = %d: %s", hh.username, status, body)
	}
	cookie := sessionCookieFromHeader(t, headers)

	status, _, body = netDo(t, baseURL, http.MethodGet, "/api/v1/items/"+hh.itemID, cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore GET item for %s: status = %d: %s", hh.username, status, body)
	}
	var item itemResponse
	mustDecodeBytes(t, body, &item)
	if item.ID != hh.itemID {
		t.Errorf("%s: restored item id = %q, want %q", hh.username, item.ID, hh.itemID)
	}

	status, _, body = netDo(t, baseURL, http.MethodGet, "/api/v1/items/"+hh.itemID+"/warranty", cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore GET warranty for %s: status = %d: %s", hh.username, status, body)
	}
	var warranty warrantyResponse
	mustDecodeBytes(t, body, &warranty)
	if warranty.Holder != hh.warrantyHolder {
		t.Errorf("%s: restored warranty holder = %q, want %q", hh.username, warranty.Holder, hh.warrantyHolder)
	}

	status, _, body = netDo(t, baseURL, http.MethodGet, "/api/v1/items/"+hh.itemID+"/sale", cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore GET sale for %s: status = %d: %s", hh.username, status, body)
	}
	var sale saleResponse
	mustDecodeBytes(t, body, &sale)
	if sale.BuyerName != hh.saleBuyer {
		t.Errorf("%s: restored sale buyer = %q, want %q", hh.username, sale.BuyerName, hh.saleBuyer)
	}

	status, _, body = netDo(t, baseURL, http.MethodGet, "/api/v1/items/"+hh.itemID+"/purchase", cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore GET purchase for %s: status = %d: %s", hh.username, status, body)
	}
	var purchase purchaseResponse
	mustDecodeBytes(t, body, &purchase)
	if purchase.Vendor != hh.purchaseVendor {
		t.Errorf("%s: restored purchase vendor = %q, want %q", hh.username, purchase.Vendor, hh.purchaseVendor)
	}

	status, _, body = netDo(t, baseURL, http.MethodGet, "/api/v1/items/"+hh.itemID+"/identifications", cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore GET identifications for %s: status = %d: %s", hh.username, status, body)
	}
	var idents identificationListResponse
	mustDecodeBytes(t, body, &idents)
	if !containsIdentificationValue(idents.Identifications, hh.identificationValue) {
		t.Errorf("%s: restored identifications %+v do not contain %q", hh.username, idents.Identifications, hh.identificationValue)
	}

	status, _, body = netDo(t, baseURL, http.MethodGet, "/api/v1/items/"+hh.itemID+"/custom-fields", cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore GET item custom fields for %s: status = %d: %s", hh.username, status, body)
	}
	var fields itemCustomFieldListResponse
	mustDecodeBytes(t, body, &fields)
	if !containsCustomFieldTextValue(fields.CustomFields, hh.customFieldValue) {
		t.Errorf("%s: restored item custom fields %+v do not contain text_value %q", hh.username, fields.CustomFields, hh.customFieldValue)
	}

	status, _, body = netDo(t, baseURL, http.MethodGet, "/api/v1/labels", cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore GET labels for %s: status = %d: %s", hh.username, status, body)
	}
	var labels labelListResponse
	mustDecodeBytes(t, body, &labels)
	if !containsLabelName(labels.Labels, hh.labelName) {
		t.Errorf("%s: restored labels %+v do not contain %q", hh.username, labels.Labels, hh.labelName)
	}

	status, _, body = netDo(t, baseURL, http.MethodGet, "/api/v1/locations", cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore GET locations for %s: status = %d: %s", hh.username, status, body)
	}
	var locations locationListResponse
	mustDecodeBytes(t, body, &locations)
	if !containsLocationName(locations.Locations, hh.locationName) {
		t.Errorf("%s: restored locations %+v do not contain %q", hh.username, locations.Locations, hh.locationName)
	}

	status, _, downloaded := netDo(t, baseURL, http.MethodGet, "/api/v1/items/"+hh.itemID+"/attachments/"+hh.attachmentID, cookie, nil)
	if status != http.StatusOK {
		t.Fatalf("post-restore download attachment for %s: status = %d", hh.username, status)
	}
	if !bytes.Equal(downloaded, hh.attachmentContent) {
		t.Errorf("%s: restored attachment download (%d byte(s)) does not match the originally "+
			"uploaded content (%d byte(s))", hh.username, len(downloaded), len(hh.attachmentContent))
	}
}

func containsIdentificationValue(items []identificationResponse, value string) bool {
	for _, i := range items {
		if i.Value == value {
			return true
		}
	}
	return false
}

func containsCustomFieldTextValue(items []itemCustomFieldResponse, value string) bool {
	for _, i := range items {
		if i.TextValue != nil && *i.TextValue == value {
			return true
		}
	}
	return false
}

func containsLabelName(items []labelResponse, name string) bool {
	for _, i := range items {
		if i.Name == name {
			return true
		}
	}
	return false
}

func containsLocationName(items []locationResponse, name string) bool {
	for _, i := range items {
		if i.Name == name {
			return true
		}
	}
	return false
}

func netDo(t *testing.T, baseURL, method, path, cookie string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, baseURL+path, r)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body for %s %s: %v", method, path, err)
	}
	return resp.StatusCode, resp.Header, respBody
}

func netDoMultipart(t *testing.T, baseURL, method, path, cookie string, body *bytes.Buffer, contentType string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		t.Fatalf("build multipart request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", contentType)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body for %s %s: %v", method, path, err)
	}
	return resp.StatusCode, resp.Header, respBody
}

func sessionCookieFromHeader(t *testing.T, h http.Header) string {
	t.Helper()
	resp := http.Response{Header: h}
	for _, c := range resp.Cookies() {
		if c.Name == session.CookieName {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie in response headers %v", session.CookieName, h)
	return ""
}

func mustDecodeBytes(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, body)
	}
}
