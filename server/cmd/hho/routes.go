package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/devicetoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

const routesUsage = `usage: hho routes [--classify]

Prints every route registered by the API router, one per line, sorted.

  --classify   append tab-separated class and isolation columns
`

var errRoutesEnumerationOnly = errors.New("hho routes: this router exists only to be enumerated and serves no request")

type (
	stubSchema            struct{}
	stubScopes            struct{}
	stubRegistrar         struct{}
	stubLoginService      struct{}
	stubDeviceTokenIssuer struct{}
	stubSessionAdmin      struct{}
	stubDeviceTokenAdmin  struct{}
	stubInviteIssuer      struct{}
	stubInviteAdmin       struct{}
	stubInviteRedeemer    struct{}
)

func (stubSchema) SchemaVersion() int64 { return 0 }

func (stubScopes) ForGroup(storage.GroupID) (storage.Scope, error) {
	return nil, errRoutesEnumerationOnly
}

func (stubRegistrar) Register(context.Context, groups.RegisterRequest) (groups.Registered, error) {
	return groups.Registered{}, errRoutesEnumerationOnly
}

func (stubLoginService) Login(context.Context, session.LoginRequest) (session.LoggedIn, error) {
	return session.LoggedIn{}, errRoutesEnumerationOnly
}

func (stubDeviceTokenIssuer) Issue(context.Context, devicetoken.IssueRequest) (devicetoken.Issued, error) {
	return devicetoken.Issued{}, errRoutesEnumerationOnly
}

func (stubSessionAdmin) ListSessions(context.Context, string, string) ([]storage.SessionInfo, error) {
	return nil, errRoutesEnumerationOnly
}

func (stubSessionAdmin) RevokeSession(context.Context, string, string, string, int64) error {
	return errRoutesEnumerationOnly
}

func (stubSessionAdmin) RevokeCallingSession(context.Context, string, string, string, int64) error {
	return errRoutesEnumerationOnly
}

func (stubDeviceTokenAdmin) ListDeviceTokens(context.Context, string, string) ([]storage.DeviceTokenInfo, error) {
	return nil, errRoutesEnumerationOnly
}

func (stubDeviceTokenAdmin) RevokeDeviceToken(context.Context, string, string, string, int64) error {
	return errRoutesEnumerationOnly
}

func (stubInviteIssuer) Issue(context.Context, invite.IssueRequest) (invite.Issued, error) {
	return invite.Issued{}, errRoutesEnumerationOnly
}

func (stubInviteAdmin) ListInvites(context.Context, string) ([]storage.InviteInfo, error) {
	return nil, errRoutesEnumerationOnly
}

func (stubInviteAdmin) RevokeInvite(context.Context, string, string, int64) error {
	return errRoutesEnumerationOnly
}

func (stubInviteRedeemer) Redeem(context.Context, invite.RedeemRequest) (invite.Redeemed, error) {
	return invite.Redeemed{}, errRoutesEnumerationOnly
}

func enumerationConfig() httpapi.Config {
	logger := slog.New(slog.DiscardHandler)

	declines := middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
		return middleware.Identity{}, middleware.ErrUnauthenticated
	})

	return httpapi.Config{
		Version:                 "routes-enumeration",
		Schema:                  stubSchema{},
		Logger:                  logger,
		Authenticator:           declines,
		Scopes:                  stubScopes{},
		MaxBodyBytes:            1,
		Registrar:               stubRegistrar{},
		LoginService:            stubLoginService{},
		SessionAuthenticator:    declines,
		DeviceTokenService:      stubDeviceTokenIssuer{},
		LoginRateLimiter:        ratelimit.New(ratelimit.Config{}),
		RegisterRateLimiter:     ratelimit.New(ratelimit.Config{}),
		TrustProxyHeaders:       true,
		Sessions:                stubSessionAdmin{},
		DeviceTokens:            stubDeviceTokenAdmin{},
		InviteService:           stubInviteIssuer{},
		Invites:                 stubInviteAdmin{},
		InviteRedeemer:          stubInviteRedeemer{},
		InviteRedeemRateLimiter: ratelimit.New(ratelimit.Config{}),

		DataDir:            "/data",
		MaxAttachmentBytes: 25 << 20,

		MaxImportBytes: 10 << 20,

		MinClientVersion: "0.0.0",

		PublicBaseURLResolver: func(*http.Request) string { return "" },

		Store: &storage.Storage{},
	}
}

func assertConfigFullyPopulated(cfg httpapi.Config) error {
	v := reflect.ValueOf(cfg)
	t := v.Type()

	var zero []string
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if v.Field(i).IsZero() {
			zero = append(zero, f.Name)
		}
	}
	if len(zero) == 0 {
		return nil
	}
	return fmt.Errorf(
		"httpapi.Config field(s) %s are unset in enumerationConfig; a nil optional dependency leaves its routes UNMOUNTED (A32), "+
			"so this enumeration would silently be a subset -- populate them with a stub in enumerationConfig",
		strings.Join(zero, ", "))
}

type middlewareRole int

const (
	roleNeutral middlewareRole = iota
	roleTenantScope
	roleRequireOwner
)

func knownMiddleware(logger *slog.Logger) (map[string]middlewareRole, error) {
	declines := middleware.AuthenticatorFunc(func(*http.Request) (middleware.Identity, error) {
		return middleware.Identity{}, middleware.ErrUnauthenticated
	})

	return buildKnownMiddleware([]middlewareConstructor{
		{label: "middleware.RequestID", key: middlewareName(middleware.RequestID), role: roleNeutral},
		{label: "middleware.Log", key: middlewareName(middleware.Log(logger)), role: roleNeutral},
		{label: "middleware.Recover", key: middlewareName(middleware.Recover(logger)), role: roleNeutral},
		{label: "middleware.MaxBody", key: middlewareName(middleware.MaxBody(1)), role: roleNeutral},
		{label: "middleware.ClientVersion", key: middlewareName(middleware.ClientVersion("")), role: roleNeutral},
		{label: "middleware.TenantScope", key: middlewareName(middleware.TenantScope(declines, stubScopes{}, logger)), role: roleTenantScope},
		{label: "middleware.RequireOwner", key: middlewareName(middleware.RequireOwner(logger)), role: roleRequireOwner},
	})
}

type middlewareConstructor struct {
	label string
	key   string
	role  middlewareRole
}

func buildKnownMiddleware(entries []middlewareConstructor) (map[string]middlewareRole, error) {
	known := make(map[string]middlewareRole, len(entries))
	labelOf := make(map[string]string, len(entries))
	for _, e := range entries {
		if prev, collide := labelOf[e.key]; collide {
			return nil, fmt.Errorf(
				"middleware identity %q is derived from both %s and %s; knownMiddleware's keys are no longer unique per constructor "+
					"(see middlewareName's doc for why the derived key is shorter than the full symbol name, and A89 for why that is checked here)",
				e.key, prev, e.label)
		}
		known[e.key] = e.role
		labelOf[e.key] = e.label
	}
	return known, nil
}

func middlewareName(mw func(http.Handler) http.Handler) string {
	p := reflect.ValueOf(mw).Pointer()
	fn := runtime.FuncForPC(p)
	if fn == nil {
		return fmt.Sprintf("unnamed-middleware@%#x", p)
	}
	return constructorIdentity(trimClosureSuffix(fn.Name()))
}

func constructorIdentity(name string) string {
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		return name[dot+1:]
	}
	return name
}

func trimClosureSuffix(name string) string {
	for {
		dot := strings.LastIndexByte(name, '.')
		if dot < 0 {
			return name
		}
		seg := strings.TrimPrefix(name[dot+1:], "func")
		if seg == "" || strings.TrimLeft(seg, "0123456789") != "" {
			return name
		}
		name = name[:dot]
	}
}

type routeEntry struct {
	Method    string
	Path      string
	Class     string
	Isolation string
}

func (e routeEntry) Line() string { return e.Method + " " + e.Path }

func (e routeEntry) ClassifiedLine() string {
	return e.Line() + "\t" + e.Class + "\t" + e.Isolation
}

func enumerateRoutes() ([]routeEntry, error) {
	cfg := enumerationConfig()
	if err := assertConfigFullyPopulated(cfg); err != nil {
		return nil, err
	}

	h, err := httpapi.NewRouter(cfg)
	if err != nil {
		return nil, fmt.Errorf("build router: %w", err)
	}
	return enumerateRouterRoutes(h)
}

func enumerateRouterRoutes(h http.Handler) ([]routeEntry, error) {
	routes, ok := h.(chi.Routes)
	if !ok {
		return nil, fmt.Errorf("router is %T, which does not implement chi.Routes; it cannot be enumerated", h)
	}

	known, err := knownMiddleware(slog.New(slog.DiscardHandler))
	if err != nil {
		return nil, err
	}

	var out []routeEntry
	walkErr := chi.Walk(routes, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		scoped, owner := false, false
		for _, mw := range mws {
			name := middlewareName(mw)
			role, ok := known[name]
			if !ok {
				return fmt.Errorf("route %s %s carries unrecognised middleware %q: add its constructor to knownMiddleware so this route can be classified", method, route, name)
			}
			switch role {
			case roleTenantScope:
				scoped = true
			case roleRequireOwner:
				owner = true
			case roleNeutral:
			}
		}

		entry := routeEntry{Method: method, Path: route, Class: "public", Isolation: "exempt"}
		switch {
		case scoped && owner:
			entry.Class, entry.Isolation = "tenant-scoped+owner-only", "required"
		case scoped:
			entry.Class, entry.Isolation = "tenant-scoped", "required"
		case owner:
			return fmt.Errorf("route %s %s is wrapped in RequireOwner but not in TenantScope; it can only answer 500", method, route)
		}
		out = append(out, entry)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if len(out) == 0 {
		return nil, errors.New("the router registered no routes at all; the enumeration cannot be trusted")
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out, nil
}

func runRoutes(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(stderr, routesUsage) }
	classify := fs.Bool("classify", false, "append tab-separated class and isolation columns")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "hho routes: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	entries, err := enumerateRoutes()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho routes: %v\n", err)
		return 1
	}

	for _, e := range entries {
		line := e.Line()
		if *classify {
			line = e.ClassifiedLine()
		}
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return 1
		}
	}
	return 0
}
