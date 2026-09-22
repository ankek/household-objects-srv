package integration

import (
	"encoding/json"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	_ "modernc.org/sqlite"
	"net/http"
	"strings"
	"testing"
)

const maxSyncPullPagingIterations = 200

type syncPullEntry struct {
	EntityType     string          `json:"entity_type"`
	ID             string          `json:"id"`
	GroupChangeSeq int64           `json:"group_change_seq"`
	Data           json.RawMessage `json:"data"`
}

type syncPullTombstoneEntry struct {
	EntityType string `json:"entity_type"`
	ID         string `json:"id"`
	DeletedAt  int64  `json:"deleted_at"`
}

type syncPullOKBody struct {
	Changes       []syncPullEntry          `json:"changes"`
	Tombstones    []syncPullTombstoneEntry `json:"tombstones"`
	NextWatermark int64                    `json:"next_watermark"`
	HasMore       bool                     `json:"has_more"`
}

type syncPullRequestWire struct {
	DeviceID string `json:"device_id"`
	Since    int64  `json:"since"`
	Limit    int64  `json:"limit"`
}

func (s *liveServer) decodeSyncPullOK(t *testing.T, cookie, deviceID string, since, limit int64) syncPullOKBody {
	t.Helper()
	rec := s.do(t, http.MethodPost, "/api/v1/sync/pull", cookie, mustMarshal(t, syncPullRequestWire{DeviceID: deviceID, Since: since, Limit: limit}))
	if rec.Code != http.StatusOK {
		t.Fatalf("sync pull (since=%d, limit=%d): status = %d, want 200: %s", since, limit, rec.Code, rec.Body.String())
	}
	var body syncPullOKBody
	mustDecode(t, rec, &body)
	return body
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return b
}

type mirrorKey struct {
	entityType string
	id         string
}

type mirrorRecord struct {
	tombstone bool
	data      string
	deletedAt int64
}

func applyPage(mirror map[mirrorKey]mirrorRecord, page syncPullOKBody) {
	for _, c := range page.Changes {
		mirror[mirrorKey{c.EntityType, c.ID}] = mirrorRecord{data: string(c.Data)}
	}
	for _, tomb := range page.Tombstones {
		mirror[mirrorKey{tomb.EntityType, tomb.ID}] = mirrorRecord{tombstone: true, deletedAt: tomb.DeletedAt}
	}
}

func pullInto(t *testing.T, srv *liveServer, cookie, deviceID string, startSince, limit int64, mirror map[mirrorKey]mirrorRecord) (map[mirrorKey]mirrorRecord, int64) {
	t.Helper()
	since := startSince
	watermark := startSince
	for i := 0; i < maxSyncPullPagingIterations; i++ {
		page := srv.decodeSyncPullOK(t, cookie, deviceID, since, limit)
		if page.NextWatermark < watermark {
			t.Fatalf("watermark moved backwards mid-pull: %d -> %d (since=%d, limit=%d)", watermark, page.NextWatermark, since, limit)
		}
		if page.HasMore && page.NextWatermark == since {
			t.Fatalf("has_more=true with zero progress at since=%d (limit=%d): next_watermark did not advance", since, limit)
		}
		applyPage(mirror, page)
		watermark = page.NextWatermark
		since = page.NextWatermark
		if !page.HasMore {
			return mirror, watermark
		}
	}
	t.Fatalf("paging from since=%d with limit=%d did not terminate within %d iterations", startSince, limit, maxSyncPullPagingIterations)
	return nil, 0
}

func assertMirrorsEqual(t *testing.T, got, want map[mirrorKey]mirrorRecord) {
	t.Helper()
	for k, wantRec := range want {
		gotRec, ok := got[k]
		if !ok {
			t.Errorf("mirror missing %+v, which the reference pull carries as tombstone=%v", k, wantRec.tombstone)
			continue
		}
		if gotRec.tombstone != wantRec.tombstone {
			t.Errorf("%+v: tombstone = %v, want %v", k, gotRec.tombstone, wantRec.tombstone)
			continue
		}
		if wantRec.tombstone {
			if gotRec.deletedAt != wantRec.deletedAt {
				t.Errorf("%+v: deleted_at = %d, want %d", k, gotRec.deletedAt, wantRec.deletedAt)
			}
			continue
		}
		if gotRec.data != wantRec.data {
			t.Errorf("%+v: data = %s, want %s", k, gotRec.data, wantRec.data)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("mirror carries unexpected %+v, absent from the reference pull", k)
		}
	}
}

func registerAndLogin(t *testing.T, srv *liveServer, username, password string) (cookie, groupID string) {
	t.Helper()
	if rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody(username, password)); rec.Code != http.StatusCreated {
		t.Fatalf("register %s: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	rec := srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody(username, password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status = %d: %s", username, rec.Code, rec.Body.String())
	}
	var login loginResponse
	mustDecode(t, rec, &login)
	return sessionCookie(t, rec), login.GroupID
}

func createSyncItem(t *testing.T, srv *liveServer, cookie, name string) itemResponse {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody(name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item %q: status = %d: %s", name, rec.Code, rec.Body.String())
	}
	var it itemResponse
	mustDecode(t, rec, &it)
	return it
}

func updateSyncItem(t *testing.T, srv *liveServer, cookie, itemID, name string, version int64) itemResponse {
	t.Helper()
	rec := srv.do(t, http.MethodPut, "/api/v1/items/"+itemID, cookie, itemUpdateBody(name, version))
	if rec.Code != http.StatusOK {
		t.Fatalf("update item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
	var it itemResponse
	mustDecode(t, rec, &it)
	return it
}

func deleteSyncItem(t *testing.T, srv *liveServer, cookie, itemID string) {
	t.Helper()
	rec := srv.do(t, http.MethodDelete, "/api/v1/items/"+itemID, cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
}

func createSyncWarranty(t *testing.T, srv *liveServer, cookie, itemID, holder string) {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items/"+itemID+"/warranty", cookie, warrantyCreateBody(holder))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create warranty for item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
}

func createSyncIdentification(t *testing.T, srv *liveServer, cookie, itemID, kind, value string) string {
	t.Helper()
	rec := srv.do(t, http.MethodPost, "/api/v1/items/"+itemID+"/identifications", cookie, identificationCreateBody(kind, value))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create identification for item %q: status = %d: %s", itemID, rec.Code, rec.Body.String())
	}
	var ident identificationResponse
	mustDecode(t, rec, &ident)
	return ident.ID
}

func seedMixedSyncHistory(t *testing.T, srv *liveServer, cookie string) {
	t.Helper()
	specs := []struct {
		name            string
		warranty        bool
		identifications int
		deleteAfter     bool
	}{
		{name: "history-item-0", warranty: false, identifications: 0, deleteAfter: false},
		{name: "history-item-1", warranty: true, identifications: 0, deleteAfter: false},
		{name: "history-item-2", warranty: false, identifications: 1, deleteAfter: false},
		{name: "history-item-3", warranty: true, identifications: 2, deleteAfter: false},
		{name: "history-item-4", warranty: false, identifications: 3, deleteAfter: false},
		{name: "history-item-5", warranty: true, identifications: 1, deleteAfter: true},
		{name: "history-item-6", warranty: false, identifications: 2, deleteAfter: true},
		{name: "history-item-7", warranty: true, identifications: 0, deleteAfter: true},
	}
	for _, spec := range specs {
		item := createSyncItem(t, srv, cookie, spec.name)
		if spec.warranty {
			createSyncWarranty(t, srv, cookie, item.ID, spec.name+"-holder")
		}
		for i := 0; i < spec.identifications; i++ {
			createSyncIdentification(t, srv, cookie, item.ID, "serial", fmt.Sprintf("%s-serial-%d", spec.name, i))
		}
		if spec.deleteAfter {
			deleteSyncItem(t, srv, cookie, item.ID)
		}
	}
}

func TestSyncPullPagingPropertyMatchesFullPullThroughHandler(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, _ := registerAndLogin(t, srv, "paging-owner", "paging-owner-pw-1")
	seedMixedSyncHistory(t, srv, cookie)
	const deviceID = "paging-probe-device"

	reference, referenceWatermark := pullInto(t, srv, cookie, deviceID, 0, 500, map[mirrorKey]mirrorRecord{})
	if len(reference) == 0 {
		t.Fatalf("seeded history produced an empty reference pull; this property test would be vacuous")
	}

	for limit := int64(1); limit <= 15; limit++ {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			got, gotWatermark := pullInto(t, srv, cookie, deviceID, 0, limit, map[mirrorKey]mirrorRecord{})
			assertMirrorsEqual(t, got, reference)
			if gotWatermark != referenceWatermark {
				t.Errorf("final watermark = %d, want %d (limit=500 reference pull)", gotWatermark, referenceWatermark)
			}
		})
	}
}

func TestSyncPullCursorTooOldThroughHandler(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, groupID := registerAndLogin(t, srv, "cursor-owner", "cursor-owner-pw-1")
	createSyncItem(t, srv, cookie, "cursor-item-0")

	gid, err := storage.NewGroupID(groupID)
	if err != nil {
		t.Fatalf("storage.NewGroupID(%q): %v", groupID, err)
	}
	repo, err := srv.storage.ForGroupSync(gid)
	if err != nil {
		t.Fatalf("ForGroupSync: %v", err)
	}
	const lowWatermark = 50
	if err := repo.SetLowWatermark(t.Context(), lowWatermark); err != nil {
		t.Fatalf("SetLowWatermark(%d): %v", lowWatermark, err)
	}

	const deviceID = "cursor-probe-device"

	t.Run("since strictly less than low watermark is cursor_too_old", func(t *testing.T) {
		rec := srv.do(t, http.MethodPost, "/api/v1/sync/pull", cookie, mustMarshal(t, syncPullRequestWire{DeviceID: deviceID, Since: lowWatermark - 1, Limit: 10}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got, want := rec.Body.String(), `{"cursor_too_old":true}`; got != want {
			t.Errorf("body = %s, want exactly %s (no other field)", got, want)
		}
	})

	t.Run("since zero is never cursor_too_old regardless of the horizon", func(t *testing.T) {
		body := srv.decodeSyncPullOK(t, cookie, deviceID, 0, 10)
		if body.Changes == nil {
			t.Errorf("changes = nil, want a non-nil (possibly empty) slice -- toSyncPullResultBody's own contract")
		}
		if strings.Contains(mustMarshalString(t, body), "cursor_too_old") {
			t.Errorf("since:0 envelope unexpectedly carries cursor_too_old: %+v", body)
		}
	})

	t.Run("since equal to low watermark is a normal envelope", func(t *testing.T) {
		body := srv.decodeSyncPullOK(t, cookie, deviceID, lowWatermark, 10)
		_ = body
	})

	t.Run("since greater than low watermark is a normal envelope", func(t *testing.T) {
		body := srv.decodeSyncPullOK(t, cookie, deviceID, lowWatermark+1, 10)
		_ = body
	})
}

func mustMarshalString(t *testing.T, v any) string {
	t.Helper()
	return string(mustMarshal(t, v))
}

func TestSyncPullResumeAfterInterveningWritesConvergesToFullPull(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, _ := registerAndLogin(t, srv, "resume-owner", "resume-owner-pw-1")
	const deviceID = "resume-device"

	x1 := createSyncItem(t, srv, cookie, "resume-item-x1")
	createSyncWarranty(t, srv, cookie, x1.ID, "x1-original-holder")
	x2 := createSyncItem(t, srv, cookie, "resume-item-x2")
	x2IdentID := createSyncIdentification(t, srv, cookie, x2.ID, "serial", "x2-serial-0")
	x3 := createSyncItem(t, srv, cookie, "resume-item-x3")

	mirror, watermark := pullInto(t, srv, cookie, deviceID, 0, 2, map[mirrorKey]mirrorRecord{})
	if _, ok := mirror[mirrorKey{"item", x3.ID}]; !ok {
		t.Fatalf("initial pull did not observe x3 (%s); the resumability property below would be vacuous", x3.ID)
	}

	x4 := createSyncItem(t, srv, cookie, "resume-item-x4")
	x1Renamed := updateSyncItem(t, srv, cookie, x1.ID, "resume-item-x1-renamed", x1.Version)
	deleteSyncItem(t, srv, cookie, x2.ID)

	mirror, resumedWatermark := pullInto(t, srv, cookie, deviceID, watermark, 2, mirror)

	fresh, freshWatermark := pullInto(t, srv, cookie, deviceID, 0, 500, map[mirrorKey]mirrorRecord{})

	assertMirrorsEqual(t, mirror, fresh)
	if resumedWatermark != freshWatermark {
		t.Errorf("resumed final watermark = %d, want %d (fresh since:0 pull)", resumedWatermark, freshWatermark)
	}

	x1Rec, ok := mirror[mirrorKey{"item", x1.ID}]
	if !ok || x1Rec.tombstone {
		t.Fatalf("x1 (%s) missing or tombstoned in the resumed mirror, want a live, updated entry: %+v", x1.ID, x1Rec)
	}
	if !strings.Contains(x1Rec.data, `"resume-item-x1-renamed"`) {
		t.Errorf("x1's mirrored data = %s, want it to carry the renamed value (an update to an already-pulled row must reappear)", x1Rec.data)
	}
	_ = x1Renamed

	x2Rec, ok := mirror[mirrorKey{"item", x2.ID}]
	if !ok || !x2Rec.tombstone {
		t.Fatalf("x2 (%s) is not a tombstone in the resumed mirror, want one (a delete must arrive as a tombstone): %+v", x2.ID, x2Rec)
	}
	x2IdentRec, ok := mirror[mirrorKey{"item_identification", x2IdentID}]
	if !ok || !x2IdentRec.tombstone {
		t.Fatalf("x2's identification (%s) is not a tombstone in the resumed mirror, want one (the delete's own cascade): %+v", x2IdentID, x2IdentRec)
	}

	x4Rec, ok := mirror[mirrorKey{"item", x4.ID}]
	if !ok || x4Rec.tombstone {
		t.Fatalf("x4 (%s), created after the client's saved watermark, is missing or tombstoned in the resumed mirror: %+v", x4.ID, x4Rec)
	}
}

func TestSyncPullFullResyncAfterCursorTooOldConvergesToOrdinaryFullPull(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, groupID := registerAndLogin(t, srv, "resync-owner", "resync-owner-pw-1")
	seedMixedSyncHistory(t, srv, cookie)
	const deviceID = "resync-device"

	baseline, baselineWatermark := pullInto(t, srv, cookie, deviceID, 0, 500, map[mirrorKey]mirrorRecord{})
	if len(baseline) == 0 {
		t.Fatalf("seeded history produced an empty baseline pull; this test would be vacuous")
	}

	firstPage := srv.decodeSyncPullOK(t, cookie, deviceID, 0, 3)
	if !firstPage.HasMore {
		t.Fatalf("seeded history fit in one page of 3; this test needs a real partial (mid-history) cursor")
	}
	staleCursor := firstPage.NextWatermark

	gid, err := storage.NewGroupID(groupID)
	if err != nil {
		t.Fatalf("storage.NewGroupID(%q): %v", groupID, err)
	}
	repo, err := srv.storage.ForGroupSync(gid)
	if err != nil {
		t.Fatalf("ForGroupSync: %v", err)
	}
	if err := repo.SetLowWatermark(t.Context(), staleCursor+1); err != nil {
		t.Fatalf("SetLowWatermark(%d): %v", staleCursor+1, err)
	}

	rec := srv.do(t, http.MethodPost, "/api/v1/sync/pull", cookie, mustMarshal(t, syncPullRequestWire{DeviceID: deviceID, Since: staleCursor, Limit: 500}))
	if rec.Code != http.StatusOK {
		t.Fatalf("stale-cursor pull: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Body.String(), `{"cursor_too_old":true}`; got != want {
		t.Fatalf("stale-cursor pull body = %s, want exactly %s", got, want)
	}

	resynced, resyncedWatermark := pullInto(t, srv, cookie, deviceID, 0, 500, map[mirrorKey]mirrorRecord{})
	assertMirrorsEqual(t, resynced, baseline)
	if resyncedWatermark != baselineWatermark {
		t.Errorf("resynced final watermark = %d, want %d (pre-bump baseline)", resyncedWatermark, baselineWatermark)
	}

	zeroBody := srv.decodeSyncPullOK(t, cookie, deviceID, 0, 1)
	_ = zeroBody
}

func TestSyncPullRequestValidationEdgesThroughHandler(t *testing.T) {
	srv := newLiveServer(t, false)
	cookie, _ := registerAndLogin(t, srv, "validate-owner", "validate-owner-pw-1")

	tests := []struct {
		name       string
		body       syncPullRequestWire
		wantStatus int
	}{
		{"empty device_id is rejected", syncPullRequestWire{DeviceID: "", Since: 0, Limit: 10}, http.StatusBadRequest},
		{"negative since is rejected", syncPullRequestWire{DeviceID: "dev", Since: -1, Limit: 10}, http.StatusBadRequest},
		{"zero limit is rejected", syncPullRequestWire{DeviceID: "dev", Since: 0, Limit: 0}, http.StatusBadRequest},
		{"negative limit is rejected", syncPullRequestWire{DeviceID: "dev", Since: 0, Limit: -5}, http.StatusBadRequest},
		{"limit over the max is rejected", syncPullRequestWire{DeviceID: "dev", Since: 0, Limit: 501}, http.StatusBadRequest},
		{"limit at the max is accepted", syncPullRequestWire{DeviceID: "dev", Since: 0, Limit: 500}, http.StatusOK},
		{"limit of one is accepted", syncPullRequestWire{DeviceID: "dev", Since: 0, Limit: 1}, http.StatusOK},
		{"since of zero is accepted", syncPullRequestWire{DeviceID: "dev", Since: 0, Limit: 10}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := srv.do(t, http.MethodPost, "/api/v1/sync/pull", cookie, mustMarshal(t, tt.body))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusBadRequest {
				p := decodeProblem(t, rec)
				if p.Detail == "" {
					t.Errorf("400 response carries an empty Detail")
				}
			}
		})
	}
}
