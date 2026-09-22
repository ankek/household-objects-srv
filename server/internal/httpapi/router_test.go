package httpapi

import (
	"bytes"
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fixedSchema int64

func (f fixedSchema) SchemaVersion() int64 { return int64(f) }

const (
	testVersion = "1.2.3-test"
	testSchema  = fixedSchema(7)
)

const testGroup = "grp-test"

var rejectingAuth = middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
	return middleware.Identity{}, middleware.ErrUnauthenticated
})

var acceptingAuth = middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
	return middleware.Identity{Group: testGroup, UserID: "usr-test", Role: "owner"}, nil
})

type fakeScope struct{ group storage.GroupID }

func (f fakeScope) GroupID() storage.GroupID      { return f.group }
func (f fakeScope) Items() storage.ItemRepository { return nil }

func (f fakeScope) Warranty() storage.WarrantyRepository { return nil }

func (f fakeScope) Sale() storage.SaleRepository { return nil }

func (f fakeScope) Purchase() storage.PurchaseRepository          { return nil }
func (f fakeScope) Visibility() storage.GroupVisibilityRepository { return nil }

func (f fakeScope) Identifications() storage.IdentificationRepository { return nil }

func (f fakeScope) CustomFieldDefs() storage.CustomFieldDefRepository { return nil }

func (f fakeScope) ItemCustomFields() storage.ItemCustomFieldRepository { return nil }

func (f fakeScope) StockAdjustments() storage.StockAdjustmentRepository { return nil }
func (f fakeScope) Locations() storage.LocationRepository               { return nil }

func (f fakeScope) Labels() storage.LabelRepository           { return nil }
func (f fakeScope) ItemLabels() storage.ItemLabelRepository   { return nil }
func (f fakeScope) Attachments() storage.AttachmentRepository { return nil }

func (f fakeScope) Members() storage.MemberRepository { return nil }

type fakeScopes struct{}

func (fakeScopes) ForGroup(g storage.GroupID) (storage.Scope, error) { return fakeScope{group: g}, nil }

func testConfig() Config {
	return Config{
		Version:       testVersion,
		Schema:        testSchema,
		Logger:        slog.New(slog.DiscardHandler),
		Authenticator: rejectingAuth,
		Scopes:        fakeScopes{},
	}
}

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	h, err := NewRouter(testConfig())
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return h
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problem.Problem {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != problem.MediaType {
		t.Fatalf("Content-Type = %q, want %q; an error body in a different media type is a second error shape, which is what the uniform-problem rule exists to prevent", got, problem.MediaType)
	}
	var p problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	return p
}

func TestNewRouterRejectsMissingDependencies(t *testing.T) {
	noSchema := testConfig()
	noSchema.Schema = nil
	if _, err := NewRouter(noSchema); err == nil {
		t.Error("NewRouter accepted a nil SchemaVersioner; the health endpoint would panic per request")
	}
	noVersion := testConfig()
	noVersion.Version = ""
	if _, err := NewRouter(noVersion); err == nil {
		t.Error("NewRouter accepted an empty Version; the health endpoint would report a blank build")
	}
	noLogger := testConfig()
	noLogger.Logger = nil
	if _, err := NewRouter(noLogger); err == nil {
		t.Error("NewRouter accepted a nil Logger; the logging middleware runs on every request, so the process would start and then panic on all of them")
	}
	noAuth := testConfig()
	noAuth.Authenticator = nil
	if _, err := NewRouter(noAuth); err == nil {
		t.Error("NewRouter accepted a nil Authenticator; the authenticated route group would serve every tenant's data to anyone who can reach the port, and nothing at runtime would report it")
	}
	noScopes := testConfig()
	noScopes.Scopes = nil
	if _, err := NewRouter(noScopes); err == nil {
		t.Error("NewRouter accepted a nil ScopeSource; no request could be bound to a group (NFR-011)")
	}
	if _, err := newRouter(testConfig(), nil); err == nil {
		t.Error("newRouter accepted an empty version table; a router serving no API at all would still pass a smoke test that only checks the process is listening")
	}
}

func TestStatusEndpoint(t *testing.T) {
	rec := do(t, newTestRouter(t), http.MethodGet, "/api/v1/status")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; the FR-140 container healthcheck polls this path and a non-200 keeps the container unhealthy: body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store; a cached health answer makes a dead container look alive", got)
	}

	var body struct {
		Status        string `json:"status"`
		Version       string `json:"version"`
		SchemaVersion int64  `json:"schema_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("status field = %q, want %q", body.Status, "ok")
	}
	if body.Version != testVersion {
		t.Errorf("version field = %q, want %q -- the endpoint reports its own build, not a constant", body.Version, testVersion)
	}
	if body.SchemaVersion != int64(testSchema) {
		t.Errorf("schema_version field = %d, want %d -- the endpoint reports the version the database actually reached", body.SchemaVersion, int64(testSchema))
	}
}

func TestStatusPublishesNothingBeyondFR135(t *testing.T) {
	rec := do(t, newTestRouter(t), http.MethodGet, "/api/v1/status")

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not a JSON object (%v): %q", err, rec.Body.String())
	}
	allowed := map[string]bool{"status": true, "version": true, "schema_version": true}
	for k := range raw {
		if !allowed[k] {
			t.Errorf("unauthenticated /status publishes unexpected field %q; FR-135 asks for liveness, version and schema version, and every other field is disclosed to anyone who can reach the port (NFR-018)", k)
		}
	}
}

func TestUnknownPathsReturnTheUniformProblemShape(t *testing.T) {
	h := newTestRouter(t)

	for _, path := range []string{
		"/api/v1",
		"/api/v1/nope",
		"/api/v1/status/",
		"/api/v1/widgets/01234567-89ab-7def-8000-000000000000",
		"/api/v2/status",
	} {
		rec := do(t, h, http.MethodGet, path)

		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
			continue
		}
		p := decodeProblem(t, rec)
		if p.Status != http.StatusNotFound {
			t.Errorf("GET %s: body status = %d, want 404 -- the body must agree with the response line", path, p.Status)
		}
		if p.Type == "" || p.Title == "" {
			t.Errorf("GET %s: problem has empty type/title (%+v); clients branch on type", path, p)
		}
		if strings.Contains(rec.Body.String(), "page not found") {
			t.Errorf("GET %s: chi's default text/plain 404 leaked through: %q", path, rec.Body.String())
		}
	}
}

func TestNotFoundBodiesAreIndistinguishable(t *testing.T) {
	h := newTestRouter(t)

	firstRec := do(t, h, http.MethodGet, "/api/v1/widgets/00000000-0000-7000-8000-000000000001")
	secondRec := do(t, h, http.MethodGet, "/api/v1/definitely/not/a/route")
	first, second := decodeProblem(t, firstRec), decodeProblem(t, secondRec)

	if first.RequestID == "" || second.RequestID == "" || first.RequestID == second.RequestID {
		t.Errorf("request ids %q and %q: every problem body must carry its own id (FR-136)", first.RequestID, second.RequestID)
	}
	first.RequestID, second.RequestID = "", ""
	if first != second {
		t.Errorf("two 404 bodies differ beyond the request id:\n  %+v\n  %+v\nA 404 that varies with the request tells a prober which path it hit, which is the distinction FR-008 and P-3 pay a status code to hide", first, second)
	}
	if body := firstRec.Body.String(); strings.Contains(body, "widgets") || strings.Contains(body, "00000000") {
		t.Errorf("the 404 body echoes the request: %s", body)
	}
}

func TestMethodNotAllowedReturnsTheProblemShapeWithAllow(t *testing.T) {
	rec := do(t, newTestRouter(t), http.MethodPost, "/api/v1/status")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body %q", rec.Code, rec.Body.String())
	}
	p := decodeProblem(t, rec)
	if p.Status != http.StatusMethodNotAllowed {
		t.Errorf("body status = %d, want 405", p.Status)
	}
	if p.Type == problem.NotFound().Type {
		t.Error("405 reuses the 404 problem type; a client cannot tell a wrong method from a wrong path")
	}

	allow := rec.Header().Get("Allow")
	if !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow = %q, want it to contain GET; RFC 9110 requires a 405 to name the methods the path does accept, and chi drops that header as soon as a custom 405 handler is installed", allow)
	}
	if strings.Contains(allow, http.MethodPost) {
		t.Errorf("Allow = %q names POST, the method that was just refused", allow)
	}
}

func TestVersionSeamLeavesEarlierVersionsUntouched(t *testing.T) {
	cfg := testConfig()

	v1Only, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	before := do(t, v1Only, http.MethodGet, "/api/v1/status")

	withV2, err := newRouter(cfg, []apiVersion{
		{base: "/api/v1", mount: mountV1},
		{base: "/api/v2", mount: func(r chi.Router, _ Config) {
			r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("v2")) })
		}},
	})
	if err != nil {
		t.Fatalf("newRouter with two versions: %v", err)
	}

	after := do(t, withV2, http.MethodGet, "/api/v1/status")
	if after.Code != before.Code || after.Body.String() != before.Body.String() {
		t.Errorf("mounting /api/v2 changed /api/v1/status: %d %q -> %d %q", before.Code, before.Body.String(), after.Code, after.Body.String())
	}

	if rec := do(t, withV2, http.MethodGet, "/api/v2/ping"); rec.Code != http.StatusOK || rec.Body.String() != "v2" {
		t.Errorf("GET /api/v2/ping = %d %q, want 200 %q", rec.Code, rec.Body.String(), "v2")
	}

	if rec := do(t, withV2, http.MethodGet, "/api/v2/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/v2/nope = %d, want 404", rec.Code)
	} else {
		decodeProblem(t, rec)
	}
}

func TestDuplicateVersionBaseIsRejected(t *testing.T) {
	_, err := newRouter(testConfig(), []apiVersion{
		{base: "/api/v1", mount: mountV1},
		{base: "/api/v1", mount: mountV1},
	})
	if err == nil {
		t.Error("newRouter accepted the same base path twice")
	}
}

func TestHandlersRemainPlainHTTPHandlers(t *testing.T) {
	var _ http.Handler = newTestRouter(t)                //nolint:staticcheck // QF1011: explicit type IS the assertion
	var _ http.HandlerFunc = statusHandler(testConfig()) //nolint:staticcheck // QF1011: explicit type IS the assertion
	var _ http.HandlerFunc = notFound
}

func TestEveryResponseCarriesARequestID(t *testing.T) {
	h := newTestRouter(t)

	for _, tc := range []struct {
		name          string
		method, path  string
		expectProblem bool
	}{
		{name: "200", method: http.MethodGet, path: "/api/v1/status"},
		{name: "404", method: http.MethodGet, path: "/api/v1/nope", expectProblem: true},
		{name: "405", method: http.MethodDelete, path: "/api/v1/status", expectProblem: true},
	} {
		rec := do(t, h, tc.method, tc.path)

		id := rec.Header().Get(requestid.HeaderName)
		if id == "" {
			t.Errorf("%s: no %s response header", tc.name, requestid.HeaderName)
			continue
		}
		if !tc.expectProblem {
			continue
		}
		if got := decodeProblem(t, rec).RequestID; got != id {
			t.Errorf("%s: body request_id = %q but header said %q; a user quoting one of them would be quoting an id that is in no log line", tc.name, got, id)
		}
	}
}

func TestRequestLogNamesTheRouteAndNothingFromTheRequest(t *testing.T) {
	const (
		bearer = "secret-device-token-QK7ZP2"
		cookie = "secret-session-value-M4XW9D"
		query  = "secret-invite-token-B8TR3V"
	)

	var logs bytes.Buffer
	cfg := testConfig()
	cfg.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status?token="+query, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Cookie", "hho_session="+cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	got := logs.String()

	for _, want := range []string{`"method":"GET"`, `"route":"/api/v1/status"`, `"status":200`} {
		if !strings.Contains(got, want) {
			t.Fatalf("log line is missing %s, so the absence assertions below would prove nothing: %s", want, got)
		}
	}
	if id := rec.Header().Get(requestid.HeaderName); !strings.Contains(got, id) {
		t.Errorf("log line does not carry the request id %q that the client was given: %s", id, got)
	}

	for name, secret := range map[string]string{
		"Authorization header": bearer,
		"Cookie header":        cookie,
		"query-string token":   query,
	} {
		if strings.Contains(got, secret) {
			t.Errorf("the %s reached the log (NFR-018): %s", name, got)
		}
	}
	if strings.Contains(got, "?") || strings.Contains(got, "token=") {
		t.Errorf("the log line carries a raw URL: %s", got)
	}
}

func probeRouter(t *testing.T, cfg Config, probe middleware.ScopedHandler) http.Handler {
	t.Helper()
	h, err := newRouter(cfg, []apiVersion{
		{base: "/api/v1", mount: func(r chi.Router, cfg Config) {
			mountV1(r, cfg)
			r.Group(func(r chi.Router) {
				authenticate(r, cfg)
				r.Get("/probe", middleware.Scoped(probe))
			})
		}},
	})
	if err != nil {
		t.Fatalf("newRouter: %v", err)
	}
	return h
}

func TestAuthenticatedRoutesAreUnreachableWithoutCredentials(t *testing.T) {
	reached := false
	h := probeRouter(t, testConfig(), func(w http.ResponseWriter, _ *http.Request, _ storage.Scope) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	})

	rec := do(t, h, http.MethodGet, "/api/v1/probe")

	if reached {
		t.Fatal("a route in the authenticated group ran for a request with no credentials")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %q", rec.Code, rec.Body.String())
	}
	if p := decodeProblem(t, rec); p.RequestID != rec.Header().Get(requestid.HeaderName) {
		t.Errorf("the 401's request_id %q does not match the response header %q", p.RequestID, rec.Header().Get(requestid.HeaderName))
	}
}

func TestAuthenticatedRoutesReceiveAScopeBoundToTheCallersGroup(t *testing.T) {
	cfg := testConfig()
	cfg.Authenticator = acceptingAuth

	var seen string
	h := probeRouter(t, cfg, func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		seen = scope.GroupID().String()
		if id, ok := middleware.IdentityFromContext(r.Context()); !ok || id.Group != testGroup {
			t.Errorf("IdentityFromContext = %+v, %v; want the identity authentication resolved", id, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := do(t, h, http.MethodGet, "/api/v1/probe")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %q", rec.Code, rec.Body.String())
	}
	if seen != testGroup {
		t.Errorf("the handler was scoped to %q, want %q", seen, testGroup)
	}
}

func TestStatusStaysReachableWithoutCredentials(t *testing.T) {
	h := probeRouter(t, testConfig(), func(http.ResponseWriter, *http.Request, storage.Scope) {
		t.Error("the probe handler ran for a request to /status")
	})

	rec := do(t, h, http.MethodGet, "/api/v1/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/status = %d, want 200 with no credentials: %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("the health endpoint issued an authentication challenge (%q); it must not be behind one", got)
	}

	if rec := do(t, h, http.MethodGet, "/api/v1/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/v1/nope = %d, want 404", rec.Code)
	}
}

func TestUnknownPathUnderTheAuthenticatedGroupIsNotAnOracle(t *testing.T) {
	h := probeRouter(t, testConfig(), func(http.ResponseWriter, *http.Request, storage.Scope) {})

	for _, path := range []string{"/api/v1/probe", "/api/v1/absent"} {
		rec := do(t, h, http.MethodGet, path)
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s answered 403; this API answers 404 or 401 and never 403 (FR-008, P-3)", path)
		}
		if p := decodeProblem(t, rec); p.Detail != "" {
			t.Errorf("%s carried detail %q", path, p.Detail)
		}
	}
}

func TestClientVersionRefusalPrecedesAuthentication(t *testing.T) {
	cfg := testConfig()
	cfg.MinClientVersion = "9.9.9"

	reached := false
	h := probeRouter(t, cfg, func(w http.ResponseWriter, _ *http.Request, _ storage.Scope) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set(middleware.ClientVersionHeader, "1.0.0")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if reached {
		t.Fatal("the probe handler ran for a refused client version")
	}
	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want 426 (not 401 -- the refusal must precede authentication): %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(requestid.HeaderName); got == "" {
		t.Error("the 426 response carries no request id header; a refusal that precedes RequestID is not routed through this codebase's chain")
	} else if p := decodeProblem(t, rec); p.RequestID != got {
		t.Errorf("the 426's body request_id %q does not match the response header %q", p.RequestID, got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	req2.Header.Set(middleware.ClientVersionHeader, "1.0.0")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUpgradeRequired {
		t.Errorf("GET /api/v1/status with an outdated claim = %d, want 426: %q", rec2.Code, rec2.Body.String())
	}
}

func TestClientVersionDefaultsToNoMinimum(t *testing.T) {
	h := probeRouter(t, testConfig(), func(w http.ResponseWriter, _ *http.Request, _ storage.Scope) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	req.Header.Set(middleware.ClientVersionHeader, "0.0.1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/v1/status with a claim and no configured minimum = %d, want 200: %q", rec.Code, rec.Body.String())
	}
}
