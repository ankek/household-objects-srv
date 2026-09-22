package middleware

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testGroup  = "grp-a"
	otherGroup = "grp-b"
)

type scopeSourceFunc func(storage.GroupID) (storage.Scope, error)

func (f scopeSourceFunc) ForGroup(g storage.GroupID) (storage.Scope, error) { return f(g) }

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

func (f fakeScope) Labels() storage.LabelRepository         { return nil }
func (f fakeScope) ItemLabels() storage.ItemLabelRepository { return nil }

func (f fakeScope) Attachments() storage.AttachmentRepository { return nil }

func (f fakeScope) Members() storage.MemberRepository { return nil }

var fakeScopes = scopeSourceFunc(func(g storage.GroupID) (storage.Scope, error) {
	return fakeScope{group: g}, nil
})

func authFor(identity Identity) Authenticator {
	return AuthenticatorFunc(func(*http.Request) (Identity, error) { return identity, nil })
}

var rejectAll = AuthenticatorFunc(func(*http.Request) (Identity, error) {
	return Identity{}, fmt.Errorf("no session cookie and no bearer token: %w", ErrUnauthenticated)
})

func scopedChain(t *testing.T, auth Authenticator, scopes ScopeSource) (http.Handler, func() string, *bool) {
	t.Helper()
	logger, logs := captureLogs()
	reached := false
	inner := Scoped(func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		reached = true
		if scope == nil {
			t.Error("a ScopedHandler was entered with a nil scope; the whole point of the parameter is that it cannot be one")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := chain(logger)(TenantScope(auth, scopes, logger)(inner))
	return h, logs.String, &reached
}

func TestUnauthenticatedRequestIsRefusedBeforeTheHandler(t *testing.T) {
	h, _, reached := scopedChain(t, rejectAll, fakeScopes)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

	if *reached {
		t.Fatal("the handler ran for an unauthenticated request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer; RFC 9110 requires a challenge on every 401", got)
	}

	p := decodeProblem(t, rec)
	if p.Status != http.StatusUnauthorized {
		t.Errorf("problem.status = %d, want 401", p.Status)
	}
	if p.RequestID == "" {
		t.Error("the 401 carries no request_id, so a user reporting it cannot be correlated with a log line (FR-136)")
	}
	if p.RequestID != rec.Header().Get(requestid.HeaderName) {
		t.Errorf("problem.request_id = %q but the response header says %q", p.RequestID, rec.Header().Get(requestid.HeaderName))
	}
	if p.Detail != "" {
		t.Errorf("the 401 carries detail %q; every credential failure must produce the same document", p.Detail)
	}
}

func TestEveryCredentialFailureProducesTheSameDocument(t *testing.T) {
	reasons := []string{"no such user", "wrong password", "session expired", "device revoked"}

	var first string
	for _, reason := range reasons {
		auth := AuthenticatorFunc(func(*http.Request) (Identity, error) {
			return Identity{}, fmt.Errorf("%s: %w", reason, ErrUnauthenticated)
		})
		h, _, _ := scopedChain(t, auth, fakeScopes)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

		p := decodeProblem(t, rec)
		p.RequestID = ""
		got := fmt.Sprintf("%d %+v", rec.Code, p)
		if first == "" {
			first = got
			continue
		}
		if got != first {
			t.Errorf("%q produced %s, but another failure produced %s; a client that can tell two credential failures apart has an account oracle", reason, got, first)
		}
	}
}

func TestScopedHandlerReceivesRepositoriesBoundToTheResolvedGroup(t *testing.T) {
	store, err := storage.Open(t.Context(), storage.Config{
		Path:      filepath.Join(t.TempDir(), "db", "hho.db"),
		ReadConns: 2,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	logger, _ := captureLogs()
	var seen storage.GroupID
	h := chain(logger)(TenantScope(authFor(Identity{Group: testGroup, UserID: "usr-1", Role: "owner"}), store, logger)(
		Scoped(func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
			seen = scope.GroupID()
			if _, err := scope.Items().List(r.Context(), storage.Page{Limit: 10}); err != nil {
				t.Errorf("the handler's repository could not query: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %q", rec.Code, rec.Body.String())
	}
	if seen.String() != testGroup {
		t.Errorf("the handler was scoped to %q, want %q", seen.String(), testGroup)
	}
}

func TestTheScopeIsTheGroupTheAuthenticatorNamed(t *testing.T) {
	logger, _ := captureLogs()
	var current Identity
	var seen []string

	h := chain(logger)(TenantScope(
		AuthenticatorFunc(func(*http.Request) (Identity, error) { return current, nil }),
		fakeScopes, logger)(
		Scoped(func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
			seen = append(seen, scope.GroupID().String())
			w.WriteHeader(http.StatusNoContent)
		})))

	for _, g := range []string{testGroup, otherGroup, testGroup} {
		current = Identity{Group: g}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204: %q", rec.Code, rec.Body.String())
		}
	}

	want := []string{testGroup, otherGroup, testGroup}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("handlers were scoped to %v, want %v", seen, want)
	}
}

func TestIdentityTravelsWithTheScope(t *testing.T) {
	want := Identity{Group: testGroup, UserID: "usr-7", Role: "member"}
	logger, _ := captureLogs()

	var got Identity
	var ok bool
	h := chain(logger)(TenantScope(authFor(want), fakeScopes, logger)(
		Scoped(func(w http.ResponseWriter, r *http.Request, _ storage.Scope) {
			got, ok = IdentityFromContext(r.Context())
			w.WriteHeader(http.StatusNoContent)
		})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

	if !ok {
		t.Fatal("IdentityFromContext found nothing inside a ScopedHandler; the identity and the scope are supposed to be one value")
	}
	if got != want {
		t.Errorf("identity = %+v, want %+v", got, want)
	}
	if _, ok := IdentityFromContext(t.Context()); ok {
		t.Error("IdentityFromContext reported an identity for a context that never went through the chain")
	}
}

func TestUnusableGroupNeverReachesTheHandler(t *testing.T) {
	for name, group := range map[string]string{
		"empty (an unfilled field)":          "",
		"whitespace":                         " ",
		"a value with structure in it":       "grp a/../grp-b",
		"something far too long to be an id": strings.Repeat("x", 200),
	} {
		t.Run(name, func(t *testing.T) {
			h, logs, reached := scopedChain(t, authFor(Identity{Group: group, UserID: "usr-1"}), fakeScopes)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

			if *reached {
				t.Fatal("the handler ran without a usable tenant scope")
			}
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500; an identity with no usable group is a server defect, not a rejected credential: %q", rec.Code, rec.Body.String())
			}
			if p := decodeProblem(t, rec); p.Detail != "" {
				t.Errorf("the 500 carries detail %q", p.Detail)
			}
			if !strings.Contains(logs(), "no usable tenant scope") {
				t.Errorf("the failure was not logged, so an operator would see only an unexplained 500: %q", logs())
			}
		})
	}
}

func TestScopeSourceFailureNeverReachesTheHandler(t *testing.T) {
	refuse := scopeSourceFunc(func(storage.GroupID) (storage.Scope, error) {
		return nil, storage.ErrNoGroup
	})
	h, _, reached := scopedChain(t, authFor(Identity{Group: testGroup}), refuse)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

	if *reached {
		t.Fatal("the handler ran after ForGroup refused to build a scope")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %q", rec.Code, rec.Body.String())
	}
}

func TestAuthenticationFailureIsNotReportedAsARejectedCredential(t *testing.T) {
	broken := AuthenticatorFunc(func(*http.Request) (Identity, error) {
		return Identity{}, errors.New("session store unavailable")
	})
	h, _, reached := scopedChain(t, broken, fakeScopes)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

	if *reached {
		t.Fatal("the handler ran although authentication never completed")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %q", rec.Code, rec.Body.String())
	}
}

func TestScopedRouteOutsideTheChainIsDeadNotOpen(t *testing.T) {
	logger, _ := captureLogs()
	reached := false
	h := chain(logger)(Scoped(func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

	if reached {
		t.Fatal("a ScopedHandler ran outside the authenticated group, which is the bypass this design exists to close")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %q", rec.Code, rec.Body.String())
	}
}

func TestTenantScopeRefusesToStartWithoutItsDependencies(t *testing.T) {
	logger, _ := captureLogs()
	for name, build := range map[string]func(){
		"nil Authenticator": func() { TenantScope(nil, fakeScopes, logger) },
		"nil ScopeSource":   func() { TenantScope(rejectAll, nil, logger) },
		"nil Logger":        func() { TenantScope(rejectAll, fakeScopes, nil) },
		"nil ScopedHandler": func() { Scoped(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("%s was accepted", name)
				}
			}()
			build()
		})
	}
}

func TestTenantScopeNeverLogsCredentials(t *testing.T) {
	for name, authErr := range map[string]error{
		"declined credential": fmt.Errorf("no session for token %s: %w", secretBearer, ErrUnauthenticated),
		"broken authenticator": fmt.Errorf("looking up session %s in %s: connection refused",
			secretBearer, secretCookie),
	} {
		t.Run(name, func(t *testing.T) {
			logger, logs := captureLogs()
			reached := false
			h := chain(logger)(TenantScope(
				AuthenticatorFunc(func(*http.Request) (Identity, error) { return Identity{}, authErr }),
				fakeScopes, logger)(
				Scoped(func(w http.ResponseWriter, r *http.Request, _ storage.Scope) {
					reached = true
					w.WriteHeader(http.StatusNoContent)
				})))

			req := httptest.NewRequest(http.MethodPost, "/invites/redeem?token="+secretQuery, strings.NewReader(secretReqBody))
			req.Header.Set("Authorization", "Bearer "+secretBearer)
			req.Header.Set("Cookie", "hho_session="+secretCookie)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if reached {
				t.Fatal("the handler ran")
			}

			got := logs.String()

			if !strings.Contains(got, `"msg":"http request"`) {
				t.Fatalf("nothing was logged at all, so the absence assertions here would prove nothing: %q", got)
			}

			for what, secret := range map[string]string{
				"Authorization header": secretBearer,
				"Cookie value":         secretCookie,
				"query-string token":   secretQuery,
				"request body":         secretReqBody,
			} {
				if strings.Contains(got, secret) {
					t.Errorf("the %s reached the log (NFR-018): %q", what, got)
				}
			}
		})
	}
}

func TestBrokenAuthenticatorIsStillDiagnosable(t *testing.T) {
	logger, logs := captureLogs()
	authErr := fmt.Errorf("looking up session %s: %w", secretBearer, errors.New("connection refused"))
	h := chain(logger)(TenantScope(
		AuthenticatorFunc(func(*http.Request) (Identity, error) { return Identity{}, authErr }),
		fakeScopes, logger)(Scoped(func(http.ResponseWriter, *http.Request, storage.Scope) {})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items", nil))

	got := logs.String()
	for _, want := range []string{
		`"msg":"authentication could not be performed"`,
		`"request_id":"`,
		fmt.Sprintf("error of type %T", authErr),
		"contents withheld",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the log is missing %s, so the 500 is undiagnosable: %q", want, got)
		}
	}
}

var scopeYieldingAllowlist = map[string]string{
	"ScopeSource.ForGroup": "the storage constructor, named as a one-method interface",
}

func TestNoExportedAccessorHandsOutAScope(t *testing.T) {
	var _ ScopedHandler = func(http.ResponseWriter, *http.Request, storage.Scope) {}
	var _ func(ScopedHandler) http.HandlerFunc = Scoped //nolint:staticcheck // QF1011: explicit type IS the test

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool { //nolint:staticcheck // SA1019: see above
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	pkg, ok := pkgs["middleware"]
	if !ok {
		t.Fatalf("package middleware not found in %v", pkgs)
	}

	checked := 0
	report := func(what string, pos token.Pos, results *ast.FieldList) {
		if _, allowed := scopeYieldingAllowlist[what]; allowed {
			return
		}
		for _, result := range astFieldsOf(results) {
			if !mentionsStorageScope(result.Type) {
				continue
			}
			t.Errorf("%s: %s yields a storage.Scope; the scope leaves this package as a ScopedHandler parameter or not at all -- an accessor a handler can call is an accessor a handler can ignore the failure of, and then proceed unscoped (NFR-011, P-3). If this one is genuinely safe, put it in scopeYieldingAllowlist with the argument.",
				fset.Position(pos), what)
		}
	}

	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() || !astReceiverIsExported(d) {
					continue
				}
				checked++
				report("func "+d.Name.Name, d.Pos(), d.Type.Results)

			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if !s.Name.IsExported() {
							continue
						}
						checked++
						switch typ := s.Type.(type) {
						case *ast.InterfaceType:
							for _, method := range astFieldsOf(typ.Methods) {
								fn, isFunc := method.Type.(*ast.FuncType)
								if !isFunc || len(method.Names) == 0 {
									continue
								}
								report(s.Name.Name+"."+method.Names[0].Name, method.Pos(), fn.Results)
							}
						case *ast.StructType:
							for _, field := range astFieldsOf(typ.Fields) {
								for _, fieldName := range field.Names {
									if !fieldName.IsExported() {
										continue
									}
									report("field "+s.Name.Name+"."+fieldName.Name, field.Pos(),
										&ast.FieldList{List: []*ast.Field{field}})
								}
							}
						case *ast.FuncType:
							report("type "+s.Name.Name, s.Pos(), typ.Results)
						}

					case *ast.ValueSpec:
						for _, valueName := range s.Names {
							if !valueName.IsExported() || s.Type == nil {
								continue
							}
							checked++
							report("var "+valueName.Name, s.Pos(),
								&ast.FieldList{List: []*ast.Field{{Type: s.Type}}})
						}
					}
				}
			}
		}
	}

	if checked < 8 {
		t.Fatalf("inspected only %d exported declarations; the scan is not reaching this package's API and would not notice a leaked scope", checked)
	}

	id, _ := IdentityFromContext(t.Context())
	if id != (Identity{}) {
		t.Errorf("IdentityFromContext on a bare context returned %+v, want the zero Identity", id)
	}
	if problem.Unauthorized().Status != http.StatusUnauthorized {
		t.Error("problem.Unauthorized is not a 401")
	}
}

func mentionsStorageScope(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		sel, isSelector := n.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		pkgIdent, isIdent := sel.X.(*ast.Ident)
		if isIdent && pkgIdent.Name == "storage" && sel.Sel.Name == "Scope" {
			found = true
			return false
		}
		return true
	})
	return found
}

func astReceiverIsExported(d *ast.FuncDecl) bool {
	if d.Recv == nil {
		return true
	}
	for _, field := range astFieldsOf(d.Recv) {
		switch typ := field.Type.(type) {
		case *ast.StarExpr:
			if ident, isIdent := typ.X.(*ast.Ident); isIdent {
				return ident.IsExported()
			}
		case *ast.Ident:
			return typ.IsExported()
		}
	}
	return false
}

func astFieldsOf(list *ast.FieldList) []*ast.Field {
	if list == nil {
		return nil
	}
	return list.List
}
