package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var androidStructuralDetailRE = regexp.MustCompile(`^mutations\[(\d+)] \(mutation_id="((?:[^"\\]|\\.)*)"\): .*`)

const trickyMutationID = `mut-"quoted"\back`

func validPushItem(t *testing.T, id string) syncPushMutationBody {
	t.Helper()
	return syncPushMutationBody{
		MutationID: id, EntityType: "item", EntityID: "itm-" + id,
		Fields: map[string]json.RawMessage{
			"name":        pushField(t, "Drill"),
			"description": pushField(t, ""),
			"location_id": pushField(t, ""),
			"quantity":    pushField(t, 1),
		},
	}
}

func assertStructuralDetail(t *testing.T, h http.Handler, req syncPushRequestBody, wantIndex int, wantID string) {
	t.Helper()
	rec := doSyncPush(t, h, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	detail := decodeProblem(t, rec).Detail
	m := androidStructuralDetailRE.FindStringSubmatch(detail)
	if m == nil {
		t.Fatalf("detail %q does not match Android's regex", detail)
	}
	if idx, _ := strconv.Atoi(m[1]); idx != wantIndex {
		t.Errorf("captured index = %s, want %d (detail %q)", m[1], wantIndex, detail)
	}
	gotID, err := strconv.Unquote(`"` + m[2] + `"`)
	if err != nil {
		t.Fatalf("Unquote(%q): %v", m[2], err)
	}
	if gotID != wantID {
		t.Errorf("unquoted mutation_id = %q, want %q", gotID, wantID)
	}
}

func TestSyncPushStructuralDetailFormat(t *testing.T) {
	tests := []struct {
		name      string
		mutations func(t *testing.T) []syncPushMutationBody
		wantIndex int
		wantID    string
	}{
		{
			name: "handler pre-flight rejection at index 2 (missing entity_type)",
			mutations: func(t *testing.T) []syncPushMutationBody {
				bad := validPushItem(t, trickyMutationID)
				bad.EntityType = ""
				return []syncPushMutationBody{validPushItem(t, "ok-0"), validPushItem(t, "ok-1"), bad}
			},
			wantIndex: 2, wantID: trickyMutationID,
		},
		{
			name: "pre-flight rejection with plain id at index 1 (missing entity_id)",
			mutations: func(t *testing.T) []syncPushMutationBody {
				bad := validPushItem(t, "plain-1")
				bad.EntityID = ""
				return []syncPushMutationBody{validPushItem(t, "ok-0"), bad}
			},
			wantIndex: 1, wantID: "plain-1",
		},
		{
			name: "syncpkg.Push PushMutationError at index 1 (attachment rejected)",
			mutations: func(t *testing.T) []syncPushMutationBody {
				return []syncPushMutationBody{
					validPushItem(t, "ok-0"),
					{MutationID: trickyMutationID, EntityType: "attachment", EntityID: "att-1", Fields: map[string]json.RawMessage{}},
					validPushItem(t, "ok-2"),
				}
			},
			wantIndex: 1, wantID: trickyMutationID,
		},
		{
			name: "syncpkg.Push PushMutationError at index 2 (unknown entity_type)",
			mutations: func(t *testing.T) []syncPushMutationBody {
				return []syncPushMutationBody{
					validPushItem(t, "ok-0"), validPushItem(t, "ok-1"),
					{MutationID: "unk-2", EntityType: "no_such_entity", EntityID: "x-1", Fields: map[string]json.RawMessage{}},
				}
			},
			wantIndex: 2, wantID: "unk-2",
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := syncPushTestRouter(t, "grp-push-detailfmt-"+strconv.Itoa(i))
			assertStructuralDetail(t, h, syncPushRequestBody{DeviceID: "dev-1", Mutations: tt.mutations(t)}, tt.wantIndex, tt.wantID)
		})
	}
}

func TestSyncPushRequestLevel400IsNotClassifiedStructural(t *testing.T) {
	h := syncPushTestRouter(t, "grp-push-detailfmt-reqlevel")

	t.Run("missing device_id", func(t *testing.T) {
		rec := doSyncPush(t, h, syncPushRequestBody{Mutations: []syncPushMutationBody{validPushItem(t, "m")}})
		assertNonStructural400(t, rec)
	})
	t.Run("invalid JSON", func(t *testing.T) {
		rec := httptestPost(h, "{not json")
		assertNonStructural400(t, rec)
	})
}

func assertNonStructural400(t *testing.T, rec interface {
	Result() *http.Response
}) {
	t.Helper()
	res := rec.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
	var p struct {
		Detail string `json:"detail"`
	}
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if strings.HasPrefix(p.Detail, "mutations[") || androidStructuralDetailRE.MatchString(p.Detail) {
		t.Errorf("request-level detail %q must not look structural", p.Detail)
	}
	if p.Detail == "" {
		t.Errorf("detail is empty")
	}
}

func httptestPost(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sync/push", strings.NewReader(body)))
	return rec
}
