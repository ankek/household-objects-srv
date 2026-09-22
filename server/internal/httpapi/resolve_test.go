package httpapi

import (
	"context"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const resolveTestGroup = "grp-resolve"

var resolveTestAuth = middleware.AuthenticatorFunc(func(r *http.Request) (middleware.Identity, error) {
	if r.Header.Get("X-Test-Auth") != "yes" {
		return middleware.Identity{}, fmt.Errorf("resolve_test: no test credential presented: %w", middleware.ErrUnauthenticated)
	}
	return middleware.Identity{Group: resolveTestGroup, UserID: "usr-resolve", Role: "member"}, nil
})

var resolveTestAuthErroring = middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
	return middleware.Identity{}, fmt.Errorf("resolve_test: simulated database fault")
})

type explodingScopeSource struct{ t *testing.T }

func (e explodingScopeSource) ForGroup(storage.GroupID) (storage.Scope, error) {
	e.t.Helper()
	e.t.Fatal("Config.Scopes.ForGroup must not be called for this request (A123: no database read before/without a scoped need)")
	return nil, nil
}

type resolveItemsRepo struct {
	storage.ItemRepository
	byCode map[string]storage.Item
}

func (r resolveItemsRepo) GetByShortCode(_ context.Context, code string) (storage.Item, error) {
	item, ok := r.byCode[code]
	if !ok {
		return storage.Item{}, storage.ErrNotFound
	}
	return item, nil
}

type resolveScope struct {
	fakeScope
	items storage.ItemRepository
}

func (s resolveScope) Items() storage.ItemRepository { return s.items }

type resolveScopes struct{ scope storage.Scope }

func (s resolveScopes) ForGroup(storage.GroupID) (storage.Scope, error) { return s.scope, nil }

func resolveConfig(scopes middleware.ScopeSource) Config {
	cfg := testConfig()
	cfg.Authenticator = resolveTestAuth
	cfg.Scopes = scopes
	return cfg
}

func newResolveRouter(t *testing.T, cfg Config) http.Handler {
	t.Helper()
	h, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return h
}

const resolveTestUUIDUpper = "018F1E2A-89AB-7CDE-8123-000000000001"

func TestItemResolveUUIDUnauthenticatedRedirectsToLoginWithNoDatabaseRead(t *testing.T) {
	h := newResolveRouter(t, resolveConfig(explodingScopeSource{t: t}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/"+resolveTestUUIDUpper, nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	want := "/login?next=" + url.QueryEscape("/i/"+resolveTestUUIDUpper)
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestItemResolveUUIDAuthenticatedRedirectsToItemsPage(t *testing.T) {
	h := newResolveRouter(t, resolveConfig(explodingScopeSource{t: t}))

	req := httptest.NewRequest(http.MethodGet, "/i/"+resolveTestUUIDUpper, nil)
	req.Header.Set("X-Test-Auth", "yes")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	canonical := uuid.MustParse(resolveTestUUIDUpper).String()
	if canonical == resolveTestUUIDUpper {
		t.Fatal("resolveTestUUIDUpper is already canonical; this test cannot tell canonicalisation apart from an echo")
	}
	want := "/items/" + canonical
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestItemResolveShortCodeAuthenticatedInGroupRedirectsToItemsPage(t *testing.T) {
	const code = "abc123"
	item := storage.Item{ID: "018f1e2a-89ab-7cde-8123-000000000099", ShortCode: code}
	scope := resolveScope{items: resolveItemsRepo{byCode: map[string]storage.Item{code: item}}}
	h := newResolveRouter(t, resolveConfig(resolveScopes{scope: scope}))

	req := httptest.NewRequest(http.MethodGet, "/i/"+code, nil)
	req.Header.Set("X-Test-Auth", "yes")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got, want := rec.Header().Get("Location"), "/items/"+item.ID; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestItemResolveShortCodeCrossGroupOrUnknownAnswers404(t *testing.T) {
	scope := resolveScope{items: resolveItemsRepo{byCode: map[string]storage.Item{
		"caller-owns-this-one": {ID: "018f1e2a-89ab-7cde-8123-000000000042"},
	}}}
	h := newResolveRouter(t, resolveConfig(resolveScopes{scope: scope}))

	for _, token := range []string{"belongs-to-another-group", "totally-unknown-code"} {
		t.Run(token, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/i/"+token, nil)
			req.Header.Set("X-Test-Auth", "yes")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
			}
			got := decodeProblem(t, rec)
			want := problem.NotFound()
			if got.Type != want.Type || got.Status != want.Status {
				t.Errorf("problem = %+v, want type/status matching %+v", got, want)
			}
		})
	}
}

func TestItemResolveShortCodeUnauthenticatedRedirectsToLoginWithNoDatabaseRead(t *testing.T) {
	h := newResolveRouter(t, resolveConfig(explodingScopeSource{t: t}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/abc123", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	want := "/login?next=" + url.QueryEscape("/i/abc123")
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestItemResolveAuthenticationFaultAnswers500(t *testing.T) {
	cfg := resolveConfig(explodingScopeSource{t: t})
	cfg.Authenticator = resolveTestAuthErroring
	h := newResolveRouter(t, cfg)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/"+resolveTestUUIDUpper, nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestItemResolvePublicBaseURLResolverPrefixesRedirectTargets(t *testing.T) {
	const base = "https://example.com/hho"
	cfg := resolveConfig(explodingScopeSource{t: t})
	cfg.PublicBaseURLResolver = func(*http.Request) string { return base }
	h := newResolveRouter(t, cfg)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/"+resolveTestUUIDUpper, nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	want := base + "/login?next=" + url.QueryEscape(base+"/i/"+resolveTestUUIDUpper)
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}
