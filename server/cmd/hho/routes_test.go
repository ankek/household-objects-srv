package main

import (
	"bufio"
	"bytes"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const coverageFile = "../../internal/httpapi/tenant_isolation_coverage.txt"

func TestEnumerationConfigPopulatesEveryConfigField(t *testing.T) {
	if err := assertConfigFullyPopulated(enumerationConfig()); err != nil {
		t.Fatalf("enumerationConfig is incomplete: %v", err)
	}
}

func TestAssertConfigFullyPopulatedRejectsAZeroConfig(t *testing.T) {
	err := assertConfigFullyPopulated(httpapi.Config{})
	if err == nil {
		t.Fatal("assertConfigFullyPopulated(zero Config) = nil, want an error naming the unset fields")
	}
	for _, field := range []string{"Registrar", "LoginService", "Sessions", "Invites"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not name the unset field %q", err, field)
		}
	}
}

func minimalRouter(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	h, err := httpapi.NewRouter(httpapi.Config{
		Version: "minimal",
		Schema:  stubSchema{},
		Logger:  logger,
		Authenticator: middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
			return middleware.Identity{}, middleware.ErrUnauthenticated
		}),
		Scopes: stubScopes{},
	})
	if err != nil {
		t.Fatalf("NewRouter(minimal): %v", err)
	}
	return h
}

func TestZeroConfigRouterIsAStrictSubsetOfTheEnumeration(t *testing.T) {
	full, err := enumerateRoutes()
	if err != nil {
		t.Fatalf("enumerateRoutes: %v", err)
	}
	minimal, err := enumerateRouterRoutes(minimalRouter(t))
	if err != nil {
		t.Fatalf("enumerateRouterRoutes(minimal): %v", err)
	}

	if len(minimal) >= len(full) {
		t.Fatalf("a zero-optional-Config router serves %d routes and the fully-configured one %d; "+
			"the nil-guard subset this enumeration defends against no longer exists, or the defence stopped working",
			len(minimal), len(full))
	}

	inFull := make(map[string]struct{}, len(full))
	for _, e := range full {
		inFull[e.Line()] = struct{}{}
	}
	for _, e := range minimal {
		if _, ok := inFull[e.Line()]; !ok {
			t.Errorf("minimal router serves %q, which the full enumeration does not list", e.Line())
		}
	}
}

var routeLine = regexp.MustCompile(`^[A-Z]+ (?:/api/v1(?:/[^ *]*)?|/i/\{token\})$`)

func TestEnumerationShapeAndOrder(t *testing.T) {
	entries, err := enumerateRoutes()
	if err != nil {
		t.Fatalf("enumerateRoutes: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("enumerateRoutes returned no routes; an empty list silently satisfies every coverage comparison")
	}

	seen := make(map[string]struct{}, len(entries))
	prev := ""
	for _, e := range entries {
		line := e.Line()
		if !routeLine.MatchString(line) {
			t.Errorf("route line %q does not match %s", line, routeLine)
		}
		if _, dup := seen[line]; dup {
			t.Errorf("route %q is emitted twice; a duplicate makes the gate's set comparison ambiguous", line)
		}
		seen[line] = struct{}{}

		key := e.Path + " " + e.Method
		if prev != "" && key < prev {
			t.Errorf("output is not sorted: %q follows %q", key, prev)
		}
		prev = key
	}
}

func TestEveryClassIsRepresented(t *testing.T) {
	entries, err := enumerateRoutes()
	if err != nil {
		t.Fatalf("enumerateRoutes: %v", err)
	}

	byClass := map[string]int{}
	for _, e := range entries {
		byClass[e.Class]++
		switch e.Class {
		case "public":
			if e.Isolation != "exempt" {
				t.Errorf("%s: class public but isolation %q, want exempt", e.Line(), e.Isolation)
			}
		case "tenant-scoped", "tenant-scoped+owner-only":
			if e.Isolation != "required" {
				t.Errorf("%s: class %q but isolation %q, want required", e.Line(), e.Class, e.Isolation)
			}
		default:
			t.Errorf("%s: unknown class %q", e.Line(), e.Class)
		}
	}
	for _, class := range []string{"public", "tenant-scoped", "tenant-scoped+owner-only"} {
		if byClass[class] == 0 {
			t.Errorf("no route was classified %q; the classifier is not distinguishing route groups", class)
		}
	}
}

func TestPublicRoutesAreExactlyTheUnauthenticatedOnes(t *testing.T) {
	want := map[string]struct{}{
		"GET /api/v1/status":          {},
		"GET /api/v1/openapi.yaml":    {},
		"POST /api/v1/auth/register":  {},
		"POST /api/v1/auth/login":     {},
		"POST /api/v1/invites/redeem": {},
		"GET /i/{token}":              {},
	}

	entries, err := enumerateRoutes()
	if err != nil {
		t.Fatalf("enumerateRoutes: %v", err)
	}
	got := map[string]struct{}{}
	for _, e := range entries {
		if e.Isolation == "exempt" {
			got[e.Line()] = struct{}{}
		}
	}
	for line := range want {
		if _, ok := got[line]; !ok {
			t.Errorf("%q is no longer classified exempt; if it became authenticated that is correct, update this test", line)
		}
	}
	for line := range got {
		if _, ok := want[line]; !ok {
			t.Errorf("%q is classified exempt from tenant isolation but is not one of mountV1's known public routes; "+
				"a new unauthenticated route must be added here deliberately, not discovered later", line)
		}
	}
}

func TestCoverageFileListsEveryRegisteredRoute(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash(coverageFile))
	if err != nil {
		t.Fatalf("read %s: %v", coverageFile, err)
	}

	listed := map[string]struct{}{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Errorf("malformed coverage line %q: want at least \"<METHOD> <path>\"", line)
			continue
		}
		listed[fields[0]+" "+fields[1]] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", coverageFile, err)
	}

	entries, err := enumerateRoutes()
	if err != nil {
		t.Fatalf("enumerateRoutes: %v", err)
	}
	for _, e := range entries {
		if _, ok := listed[e.Line()]; !ok {
			t.Errorf("route %q has no entry in %s; add one (\"exempt:\" if it authenticates nobody, "+
				"otherwise a covered-by:/pending: entry) -- a route with no entry is NFR-010's exact failure mode",
				e.Line(), coverageFile)
		}
	}
}

func TestRunRoutesPrintsOneLinePerRouteAndNothingElse(t *testing.T) {
	entries, err := enumerateRoutes()
	if err != nil {
		t.Fatalf("enumerateRoutes: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := runRoutes(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("runRoutes exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("runRoutes wrote to stderr: %q", stderr.String())
	}

	got := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(got) != len(entries) {
		t.Fatalf("stdout has %d lines, want %d (one per route)", len(got), len(entries))
	}
	for i, e := range entries {
		if got[i] != e.Line() {
			t.Errorf("line %d = %q, want %q", i, got[i], e.Line())
		}
	}
}

func TestRunRoutesClassifyAddsTwoColumns(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runRoutes([]string{"--classify"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runRoutes --classify exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if n := strings.Count(line, "\t"); n != 2 {
			t.Errorf("classified line %q has %d tabs, want 2", line, n)
		}
	}
}

func TestRunRoutesRejectsUnknownArguments(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"extra"}} {
		var stdout, stderr bytes.Buffer
		if code := runRoutes(args, &stdout, &stderr); code == 0 {
			t.Errorf("runRoutes(%q) exit code = 0, want non-zero", args)
		}
		if stdout.Len() != 0 {
			t.Errorf("runRoutes(%q) wrote %q to stdout on failure, want nothing", args, stdout.String())
		}
	}
}

func TestTrimClosureSuffix(t *testing.T) {
	cases := map[string]string{
		"pkg/mw.TenantScope.func1": "pkg/mw.TenantScope",
		"pkg/mw.TenantScope.1":     "pkg/mw.TenantScope",
		"pkg/mw.Log.func1.2":       "pkg/mw.Log",
		"pkg/mw.RequestID":         "pkg/mw.RequestID",
		"github.com/x/v5.Chain":    "github.com/x/v5.Chain",
		"pkg/mw.(*T).Method-fm":    "pkg/mw.(*T).Method-fm",
	}
	for in, want := range cases {
		if got := trimClosureSuffix(in); got != want {
			t.Errorf("trimClosureSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConstructorIdentity(t *testing.T) {
	cases := map[string]string{
		"github.com/ankek/Household-Objects-Dev/server/internal/httpapi.newRouter.Log":                "Log",
		"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware.Log":               "Log",
		"github.com/ankek/Household-Objects-Dev/server/cmd/hho.knownMiddleware.Log":                   "Log",
		"github.com/ankek/Household-Objects-Dev/server/internal/httpapi.authenticate.TenantScope":     "TenantScope",
		"github.com/ankek/Household-Objects-Dev/server/internal/httpapi.mountV1.func2.1.RequireOwner": "RequireOwner",
		"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware.RequestID":         "RequestID",
		"RequestID": "RequestID",
	}
	for in, want := range cases {
		if got := constructorIdentity(in); got != want {
			t.Errorf("constructorIdentity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKnownMiddlewareKeysAreMutuallyUnique(t *testing.T) {
	known, err := knownMiddleware(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("knownMiddleware: %v", err)
	}
	if len(known) != 7 {
		t.Errorf("knownMiddleware returned %d entries, want 7 (RequestID, Log, Recover, MaxBody, ClientVersion, TenantScope, RequireOwner)", len(known))
	}
}

func TestBuildKnownMiddlewareRejectsCollidingKeys(t *testing.T) {
	_, err := buildKnownMiddleware([]middlewareConstructor{
		{label: "middleware.Alpha", key: "SameKey", role: roleNeutral},
		{label: "middleware.Beta", key: "SameKey", role: roleTenantScope},
	})
	if err == nil {
		t.Fatal("buildKnownMiddleware(two entries sharing a key) = nil error, want an error naming both")
	}
	for _, want := range []string{"middleware.Alpha", "middleware.Beta", "SameKey"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestBuildKnownMiddlewareAcceptsDistinctKeys(t *testing.T) {
	got, err := buildKnownMiddleware([]middlewareConstructor{
		{label: "middleware.Alpha", key: "AlphaKey", role: roleNeutral},
		{label: "middleware.Beta", key: "BetaKey", role: roleTenantScope},
	})
	if err != nil {
		t.Fatalf("buildKnownMiddleware(two distinct-key entries): %v", err)
	}
	if got["AlphaKey"] != roleNeutral || got["BetaKey"] != roleTenantScope {
		t.Errorf("buildKnownMiddleware(distinct keys) = %v, want AlphaKey:roleNeutral, BetaKey:roleTenantScope", got)
	}
}
