package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/api"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const syncConflictsPath = "/api/v1/sync/conflicts"

func TestSyncConflictsIsReadableByAMember(t *testing.T) {
	cfg := testConfig()
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	stubConflictSource(t, &fakeConflictRepo{})
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, asTestUser(httptest.NewRequest(http.MethodGet, syncConflictsPath, nil), "member"))

	if rec.Code == http.StatusForbidden {
		t.Fatalf("GET %s as a member: status = 403, want 200 -- D4.6/A182 make the conflict log "+
			"member-readable; a 403 means the route was mounted inside the RequireOwner sub-group",
			syncConflictsPath)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as a member: status = %d, want %d; body = %s",
			syncConflictsPath, rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestSyncConflictsRequiresCredentials(t *testing.T) {
	cfg := testConfig()
	cfg.Authenticator = identityByHeaderAuth(sessionsTestIdentities)
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, syncConflictsPath, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET %s: status = %d, want 401", syncConflictsPath, rec.Code)
	}
}

type fakeConflictRepo struct {
	storage.PushRepository
	entries []storage.ConflictLogEntry
	err     error

	gotLimit int
	gotAfter *storage.ConflictCursor
}

func (f *fakeConflictRepo) ListConflicts(_ context.Context, after *storage.ConflictCursor, limit int) (storage.ConflictPage, error) {
	f.gotLimit, f.gotAfter = limit, after
	if f.err != nil {
		return storage.ConflictPage{}, f.err
	}
	start := 0
	if after != nil {
		for i, e := range f.entries {
			if e.ID == after.ID {
				start = i + 1
			}
		}
	}
	rest := f.entries[start:]
	page := storage.ConflictPage{Entries: []storage.ConflictLogEntry{}}
	for i, e := range rest {
		if i == limit {
			last := page.Entries[limit-1]
			page.Next = &storage.ConflictCursor{DetectedAt: last.DetectedAt, ID: last.ID}
			break
		}
		page.Entries = append(page.Entries, e)
	}
	return page, nil
}

func stubConflictSource(t *testing.T, f *fakeConflictRepo) {
	t.Helper()
	orig := syncConflictRepositoryFor
	syncConflictRepositoryFor = func(Config, storage.Scope) (storage.PushRepository, error) { return f, nil }
	t.Cleanup(func() { syncConflictRepositoryFor = orig })
}

func syncConflictsRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := testConfig()
	cfg.Authenticator = authAsGroup("grp-test-a")
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return h
}

func strp(s string) *string { return &s }

func TestSyncConflictsLimitValidation(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantCode  int
		wantLimit int
	}{
		{"default", "", 200, 50},
		{"one", "?limit=1", 200, 1},
		{"max", "?limit=200", 200, 200},
		{"zero", "?limit=0", 400, 0},
		{"over max", "?limit=201", 400, 0},
		{"negative", "?limit=-1", 400, 0},
		{"non integer", "?limit=abc", 400, 0},
		{"fraction", "?limit=1.5", 400, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeConflictRepo{}
			stubConflictSource(t, f)
			rec := do(t, syncConflictsRouter(t), http.MethodGet, syncConflictsPath+tc.query)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantCode == 200 && f.gotLimit != tc.wantLimit {
				t.Errorf("limit passed to storage = %d, want %d", f.gotLimit, tc.wantLimit)
			}
			if tc.wantCode == 400 {
				if p := decodeProblem(t, rec); p.Detail == "" {
					t.Errorf("400 must carry a detail: %+v", p)
				}
				if f.gotLimit != 0 {
					t.Errorf("storage was called on an invalid limit")
				}
			}
		})
	}
}

func TestSyncConflictsBadCursorIsGeneric400(t *testing.T) {
	for _, after := range []string{"!!!not-base64!!!", "aGVsbG8", "Og", "MTo"} {
		t.Run(after, func(t *testing.T) {
			f := &fakeConflictRepo{}
			stubConflictSource(t, f)
			rec := do(t, syncConflictsRouter(t), http.MethodGet, syncConflictsPath+"?after="+after)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
			p := decodeProblem(t, rec)
			if p.Detail != "after is not a valid cursor" {
				t.Errorf("detail = %q, want the fixed generic sentence", p.Detail)
			}
			for _, leak := range []string{"storage", "base64", "illegal", "strconv", "parsing"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Errorf("400 body leaks internal text %q: %s", leak, rec.Body.String())
				}
			}
		})
	}
}

func TestSyncConflictsStorageErrorIs500WithoutInternalText(t *testing.T) {
	f := &fakeConflictRepo{err: fmt.Errorf("storage: list conflicts: SQLITE_BUSY secret-table")}
	stubConflictSource(t, f)
	rec := do(t, syncConflictsRouter(t), http.MethodGet, syncConflictsPath)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "SQLITE") || strings.Contains(rec.Body.String(), "secret-table") {
		t.Errorf("500 body leaks internal text: %s", rec.Body.String())
	}
}

func syncConflictsFixture(n int) []storage.ConflictLogEntry {
	out := make([]storage.ConflictLogEntry, n)
	for i := range out {
		out[i] = storage.ConflictLogEntry{
			ID: "c" + strconv.Itoa(1000-i), EntityType: "item", EntityID: "it-1", FieldName: "name",
			DetectedAt: int64(5000 - i),
		}
	}
	return out
}

func TestSyncConflictsPaginationRoundTrip(t *testing.T) {
	f := &fakeConflictRepo{entries: syncConflictsFixture(5)}
	stubConflictSource(t, f)
	h := syncConflictsRouter(t)

	var seen []string
	path := syncConflictsPath + "?limit=2"
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination did not terminate")
		}
		rec := do(t, h, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Conflicts  []struct{ ID string } `json:"conflicts"`
			NextCursor *string               `json:"next_cursor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, c := range body.Conflicts {
			seen = append(seen, c.ID)
		}
		if body.NextCursor == nil {
			break
		}
		if _, err := storage.DecodeConflictCursor(*body.NextCursor); err != nil {
			t.Fatalf("next_cursor is not decodable: %v", err)
		}
		path = syncConflictsPath + "?limit=2&after=" + *body.NextCursor
	}
	want := []string{"c1000", "c999", "c998", "c997", "c996"}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("walked %v, want %v", seen, want)
	}
}

func TestSyncConflictsEmitsExplicitNulls(t *testing.T) {
	f := &fakeConflictRepo{entries: []storage.ConflictLogEntry{
		{ID: "c1", EntityType: "item", EntityID: "it-1", FieldName: "_entity", DetectedAt: 7},
		{ID: "c2", MutationID: strp("m-2"), EntityType: "item", EntityID: "it-1", FieldName: "name",
			ServerValue: strp(`"a"`), LosingClientValue: strp(`"b"`), DetectedAt: 6},
	}}
	stubConflictSource(t, f)
	rec := do(t, syncConflictsRouter(t), http.MethodGet, syncConflictsPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var raw struct {
		Conflicts  []map[string]json.RawMessage `json:"conflicts"`
		NextCursor json.RawMessage              `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw.NextCursor) != "null" {
		t.Errorf("next_cursor = %q, want the explicit null", raw.NextCursor)
	}
	if len(raw.Conflicts) != 2 {
		t.Fatalf("got %d entries, want 2", len(raw.Conflicts))
	}
	for _, key := range []string{"mutation_id", "server_value", "losing_client_value"} {
		if got, ok := raw.Conflicts[0][key]; !ok || string(got) != "null" {
			t.Errorf("entry 0 key %q = %q (present=%v), want explicit null", key, got, ok)
		}
	}
	if got := string(raw.Conflicts[1]["server_value"]); got != `"\"a\""` {
		t.Errorf("entry 1 server_value = %s, want the JSON text as a string", got)
	}

	rec = do(t, syncConflictsRouter(t), http.MethodGet, syncConflictsPath+"?limit=1&after="+
		storage.EncodeConflictCursor(storage.ConflictCursor{DetectedAt: 1, ID: "zzz"}))
	if !strings.Contains(rec.Body.String(), `"conflicts":[`) {
		t.Errorf("body = %s", rec.Body.String())
	}
	f.entries = nil
	rec = do(t, syncConflictsRouter(t), http.MethodGet, syncConflictsPath)
	if got := strings.TrimSpace(rec.Body.String()); got != `{"conflicts":[],"next_cursor":null}` {
		t.Errorf("empty log body = %s, want {\"conflicts\":[],\"next_cursor\":null}", got)
	}
}

func TestSyncConflictsResponseMatchesOpenAPISchema(t *testing.T) {
	var doc map[string]any
	if err := yaml.Unmarshal(api.OpenAPIYAML, &doc); err != nil {
		t.Fatal(err)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)

	f := &fakeConflictRepo{entries: []storage.ConflictLogEntry{
		{ID: "c1", EntityType: "item", EntityID: "it-1", FieldName: "_entity", DetectedAt: 7},
		{ID: "c2", MutationID: strp("m-2"), EntityType: "item", EntityID: "it-1", FieldName: "name",
			ServerValue: strp(`"a"`), LosingClientValue: strp(`"b"`), DetectedAt: 6},
	}}
	stubConflictSource(t, f)
	for _, q := range []string{"?limit=1", ""} {
		rec := do(t, syncConflictsRouter(t), http.MethodGet, syncConflictsPath+q)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		dec := json.NewDecoder(strings.NewReader(rec.Body.String()))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		if err := validateAgainstSchema(schemas, map[string]any{"$ref": "#/components/schemas/SyncConflictLogResponse"}, v, "$"); err != nil {
			t.Errorf("query %q: %v\nbody: %s", q, err, rec.Body.String())
		}
	}

	rec := do(t, syncConflictsRouter(t), http.MethodGet, syncConflictsPath+"?limit=0")
	dec := json.NewDecoder(strings.NewReader(rec.Body.String()))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	if err := validateAgainstSchema(schemas, map[string]any{"$ref": "#/components/schemas/Problem"}, v, "$"); err != nil {
		t.Errorf("400 body vs Problem: %v", err)
	}
}

func validateAgainstSchema(schemas map[string]any, schema map[string]any, v any, at string) error {
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		target, ok := schemas[name].(map[string]any)
		if !ok {
			return fmt.Errorf("%s: unresolved $ref %s", at, ref)
		}
		return validateAgainstSchema(schemas, target, v, at)
	}
	var types []string
	switch t := schema["type"].(type) {
	case string:
		types = []string{t}
	case []any:
		for _, x := range t {
			types = append(types, x.(string))
		}
	}
	matches := func(typ string) bool {
		switch typ {
		case "null":
			return v == nil
		case "string":
			_, ok := v.(string)
			return ok
		case "integer":
			n, ok := v.(json.Number)
			if !ok {
				return false
			}
			_, err := n.Int64()
			return err == nil
		case "boolean":
			_, ok := v.(bool)
			return ok
		case "object":
			_, ok := v.(map[string]any)
			return ok
		case "array":
			_, ok := v.([]any)
			return ok
		}
		return true
	}
	if len(types) > 0 {
		ok := false
		for _, typ := range types {
			ok = ok || matches(typ)
		}
		if !ok {
			return fmt.Errorf("%s: value %v does not match type %v", at, v, types)
		}
	}
	switch x := v.(type) {
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		if req, ok := schema["required"].([]any); ok {
			for _, k := range req {
				if _, present := x[k.(string)]; !present {
					return fmt.Errorf("%s: required key %q is absent", at, k)
				}
			}
		}
		for k, val := range x {
			ps, ok := props[k].(map[string]any)
			if !ok {
				return fmt.Errorf("%s: key %q is not declared by the schema", at, k)
			}
			if err := validateAgainstSchema(schemas, ps, val, at+"."+k); err != nil {
				return err
			}
		}
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, el := range x {
				if err := validateAgainstSchema(schemas, items, el, fmt.Sprintf("%s[%d]", at, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
