package httpapi

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"strings"
)

type SchemaVersioner interface {
	SchemaVersion() int64
}

type Config struct {
	Version string

	Schema SchemaVersioner

	Logger *slog.Logger

	Authenticator middleware.Authenticator

	Scopes middleware.ScopeSource

	MaxBodyBytes int64

	Registrar Registrar

	LoginService LoginService

	SessionAuthenticator middleware.Authenticator

	DeviceTokenService DeviceTokenIssuer

	LoginRateLimiter *ratelimit.Limiter

	RegisterRateLimiter *ratelimit.Limiter

	TrustProxyHeaders bool

	Sessions SessionAdmin

	DeviceTokens DeviceTokenAdmin

	InviteService InviteIssuer

	Invites InviteAdmin

	InviteRedeemer InviteRedeemer

	InviteRedeemRateLimiter *ratelimit.Limiter

	DataDir string

	MaxAttachmentBytes int64

	MaxImportBytes int64

	Store *storage.Storage

	MinClientVersion string

	PublicBaseURLResolver func(*http.Request) string
}

func defaultRateLimiter(cur *ratelimit.Limiter) *ratelimit.Limiter {
	if cur != nil {
		return cur
	}
	return ratelimit.New(ratelimit.Config{})
}

type apiVersion struct {
	base  string
	mount func(r chi.Router, cfg Config)
}

var apiVersions = []apiVersion{
	{base: "/api/v1", mount: mountV1},
}

func NewRouter(cfg Config) (http.Handler, error) {
	return newRouter(cfg, apiVersions)
}

func newRouter(cfg Config, versions []apiVersion) (http.Handler, error) {
	if cfg.Schema == nil {
		return nil, errors.New("httpapi: no schema version source configured")
	}
	if cfg.Version == "" {
		return nil, errors.New("httpapi: no build version configured")
	}
	if cfg.Logger == nil {
		return nil, errors.New("httpapi: no logger configured")
	}
	if cfg.Authenticator == nil {
		return nil, errors.New("httpapi: no authenticator configured; the authenticated route group would answer every caller")
	}
	if cfg.Scopes == nil {
		return nil, errors.New("httpapi: no scope source configured; no request could be bound to a group (NFR-011)")
	}
	cfg.LoginRateLimiter = defaultRateLimiter(cfg.LoginRateLimiter)
	cfg.RegisterRateLimiter = defaultRateLimiter(cfg.RegisterRateLimiter)
	cfg.InviteRedeemRateLimiter = defaultRateLimiter(cfg.InviteRedeemRateLimiter)
	if len(versions) == 0 {
		return nil, errors.New("httpapi: no API versions to mount")
	}

	root := chi.NewRouter()

	root.NotFound(notFound)
	root.MethodNotAllowed(methodNotAllowed(root))

	root.Use(middleware.RequestID)
	root.Use(middleware.Log(cfg.Logger))
	root.Use(middleware.Recover(cfg.Logger))

	seen := make(map[string]struct{}, len(versions))
	for _, v := range versions {
		if _, dup := seen[v.base]; dup {
			return nil, fmt.Errorf("httpapi: API version %q is mounted twice", v.base)
		}
		seen[v.base] = struct{}{}

		mount := v.mount
		root.Route(v.base, func(r chi.Router) { mount(r, cfg) })
	}

	root.Get("/i/{token}", itemResolveHandler(cfg))

	return root, nil
}

func mountV1(r chi.Router, cfg Config) {
	r.Use(middleware.ClientVersion(cfg.MinClientVersion))

	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(cfg.MaxBodyBytes))
		r.Get("/status", statusHandler(cfg))

		r.Get("/openapi.yaml", openapiHandler())

		if cfg.Registrar != nil {
			r.Post("/auth/register", registerHandler(cfg))
		}
		if cfg.LoginService != nil {
			r.Post("/auth/login", loginHandler(cfg))
		}
		if cfg.InviteRedeemer != nil {
			r.Post("/invites/redeem", inviteRedeemHandler(cfg))
		}
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(cfg.MaxBodyBytes))
		authenticate(r, cfg)

		if cfg.Sessions != nil {
			r.Get("/auth/sessions", sessionsListHandler(cfg))
			r.Delete("/auth/sessions/{sessionID}", sessionRevokeHandler(cfg))
		}
		if cfg.DeviceTokens != nil {
			r.Get("/auth/device-tokens", deviceTokensListHandler(cfg))
			r.Delete("/auth/device-tokens/{deviceTokenID}", deviceTokenRevokeHandler(cfg))
		}

		if cfg.InviteService != nil && cfg.Invites != nil {
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireOwner(cfg.Logger))
				r.Get("/invites", inviteListHandler(cfg))
				r.Post("/invites", inviteCreateHandler(cfg))
				r.Delete("/invites/{inviteID}", inviteRevokeHandler(cfg))
			})
		}

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireOwner(cfg.Logger))
			r.Put("/groups/detail-visibility", middleware.Scoped(groupVisibilityUpdateHandler(cfg)))
		})
		r.Get("/groups/detail-visibility", middleware.Scoped(groupVisibilityGetHandler(cfg)))

		r.Get("/groups/members", middleware.Scoped(memberListHandler(cfg)))

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireOwner(cfg.Logger))
			r.Post("/custom-field-defs", middleware.Scoped(customFieldDefCreateHandler(cfg)))
			r.Put("/custom-field-defs/{fieldDefID}", middleware.Scoped(customFieldDefUpdateHandler(cfg)))
			r.Delete("/custom-field-defs/{fieldDefID}", middleware.Scoped(customFieldDefDeleteHandler(cfg)))
		})
		r.Get("/custom-field-defs", middleware.Scoped(customFieldDefListHandler(cfg)))

		r.Post("/items", middleware.Scoped(itemCreateHandler(cfg)))
		r.Get("/items", middleware.Scoped(itemListHandler(cfg)))
		r.Get("/items/{itemID}", middleware.Scoped(itemGetHandler(cfg)))
		r.Put("/items/{itemID}", middleware.Scoped(itemUpdateHandler(cfg)))
		r.Delete("/items/{itemID}", middleware.Scoped(itemDeleteHandler(cfg)))

		r.Post("/items/{itemID}/warranty", middleware.Scoped(warrantyCreateHandler(cfg)))
		r.Get("/items/{itemID}/warranty", middleware.Scoped(warrantyGetHandler(cfg)))
		r.Put("/items/{itemID}/warranty", middleware.Scoped(warrantyUpdateHandler(cfg)))
		r.Delete("/items/{itemID}/warranty", middleware.Scoped(warrantyDeleteHandler(cfg)))

		r.Post("/items/{itemID}/sale", middleware.Scoped(saleCreateHandler(cfg)))
		r.Get("/items/{itemID}/sale", middleware.Scoped(saleGetHandler(cfg)))
		r.Put("/items/{itemID}/sale", middleware.Scoped(saleUpdateHandler(cfg)))
		r.Delete("/items/{itemID}/sale", middleware.Scoped(saleDeleteHandler(cfg)))

		r.Post("/items/{itemID}/purchase", middleware.Scoped(purchaseCreateHandler(cfg)))
		r.Get("/items/{itemID}/purchase", middleware.Scoped(purchaseGetHandler(cfg)))
		r.Put("/items/{itemID}/purchase", middleware.Scoped(purchaseUpdateHandler(cfg)))
		r.Delete("/items/{itemID}/purchase", middleware.Scoped(purchaseDeleteHandler(cfg)))

		r.Get("/items/{itemID}/identifications", middleware.Scoped(identificationListHandler(cfg)))
		r.Post("/items/{itemID}/identifications", middleware.Scoped(identificationCreateHandler(cfg)))
		r.Put("/items/{itemID}/identifications/{identificationID}", middleware.Scoped(identificationUpdateHandler(cfg)))
		r.Delete("/items/{itemID}/identifications/{identificationID}", middleware.Scoped(identificationDeleteHandler(cfg)))

		r.Get("/items/{itemID}/custom-fields", middleware.Scoped(itemCustomFieldListHandler(cfg)))
		r.Post("/items/{itemID}/custom-fields", middleware.Scoped(itemCustomFieldCreateHandler(cfg)))
		r.Put("/items/{itemID}/custom-fields/{customFieldID}", middleware.Scoped(itemCustomFieldUpdateHandler(cfg)))
		r.Delete("/items/{itemID}/custom-fields/{customFieldID}", middleware.Scoped(itemCustomFieldDeleteHandler(cfg)))

		r.Get("/items/{itemID}/stock-adjustments", middleware.Scoped(stockAdjustmentListHandler(cfg)))
		r.Post("/items/{itemID}/stock-adjustments", middleware.Scoped(stockAdjustmentCreateHandler(cfg)))

		r.Get("/labels", middleware.Scoped(labelListHandler(cfg)))
		r.Post("/labels", middleware.Scoped(labelCreateHandler(cfg)))
		r.Put("/labels/{labelID}", middleware.Scoped(labelUpdateHandler(cfg)))
		r.Delete("/labels/{labelID}", middleware.Scoped(labelDeleteHandler(cfg)))

		r.Get("/items/{itemID}/labels", middleware.Scoped(itemLabelListHandler(cfg)))
		r.Put("/items/{itemID}/labels/{labelID}", middleware.Scoped(itemLabelAttachHandler(cfg)))
		r.Delete("/items/{itemID}/labels/{labelID}", middleware.Scoped(itemLabelDetachHandler(cfg)))

		r.Get("/items/{itemID}/attachments", middleware.Scoped(attachmentListHandler(cfg)))
		r.Get("/items/{itemID}/attachments/{attachmentID}", middleware.Scoped(attachmentDownloadHandler(cfg)))
		r.Get("/items/{itemID}/attachments/{attachmentID}/thumbnail", middleware.Scoped(attachmentThumbnailDownloadHandler(cfg)))
		r.Delete("/items/{itemID}/attachments/{attachmentID}", middleware.Scoped(attachmentDeleteHandler(cfg)))
		r.Post("/locations", middleware.Scoped(locationCreateHandler(cfg)))
		r.Get("/locations", middleware.Scoped(locationListHandler(cfg)))

		r.Get("/locations/tree", middleware.Scoped(locationTreeHandler(cfg)))
		r.Get("/locations/{locationID}", middleware.Scoped(locationGetHandler(cfg)))
		r.Put("/locations/{locationID}", middleware.Scoped(locationUpdateHandler(cfg)))
		r.Delete("/locations/{locationID}", middleware.Scoped(locationDeleteHandler(cfg)))

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireOwner(cfg.Logger))
			r.Get("/backup", backupHandler(cfg))
		})

		r.Get("/export/items.csv", middleware.Scoped(exportItemsCSVHandler(cfg)))

		r.Get("/import/{importID}/preview", middleware.Scoped(importPreviewHandler(cfg)))

		r.Post("/import/{importID}/commit", middleware.Scoped(importCommitHandler(cfg)))

		r.Get("/export/bom", middleware.Scoped(bomHandler(cfg)))

		r.Post("/labels/qr/batch", middleware.Scoped(labelsQRBatchHandler(cfg)))

		r.Route("/reports", func(r chi.Router) {
			r.Get("/valuation", middleware.Scoped(reportsValuationHandler(cfg)))
			r.Get("/warranty-expiring", middleware.Scoped(reportsWarrantyExpiringHandler(cfg)))
			r.Get("/purchases", middleware.Scoped(reportsPurchasesHandler(cfg)))
			r.Get("/item-count-by-location", middleware.Scoped(reportsItemCountByLocationHandler(cfg)))
		})

		r.Route("/sync", func(r chi.Router) {
			r.Post("/pull", middleware.Scoped(syncPullHandler(cfg)))
			r.Post("/push", syncPushHandler(cfg))
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(uploadBodyLimit(cfg)))
		authenticate(r, cfg)
		r.Post("/items/{itemID}/attachments", middleware.Scoped(attachmentUploadHandler(cfg)))
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(maxImportBytes(cfg)))
		authenticate(r, cfg)
		r.Post("/import/native/upload", middleware.Scoped(importNativeUploadHandler(cfg)))
	})

	if cfg.SessionAuthenticator != nil && cfg.DeviceTokenService != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.MaxBody(cfg.MaxBodyBytes))
			r.Use(middleware.TenantScope(cfg.SessionAuthenticator, cfg.Scopes, cfg.Logger))
			r.Post("/auth/device-tokens", deviceTokenIssueHandler(cfg))
		})
	}

	if cfg.SessionAuthenticator != nil && cfg.Sessions != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.MaxBody(cfg.MaxBodyBytes))
			r.Use(middleware.TenantScope(cfg.SessionAuthenticator, cfg.Scopes, cfg.Logger))
			r.Post("/auth/logout", logoutHandler(cfg))
		})
	}
}

func authenticate(r chi.Router, cfg Config) {
	r.Use(middleware.TenantScope(cfg.Authenticator, cfg.Scopes, cfg.Logger))
}

func notFound(w http.ResponseWriter, r *http.Request) {
	problem.Write(w, r, problem.NotFound())
}

var probeMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
}

func methodNotAllowed(routes chi.Routes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if allow := allowedMethods(routes, r.URL.Path); allow != "" {
			w.Header().Set("Allow", allow)
		}
		problem.Write(w, r, problem.MethodNotAllowed())
	}
}

func allowedMethods(routes chi.Routes, path string) string {
	allowed := make([]string, 0, len(probeMethods))
	for _, m := range probeMethods {
		if routes.Match(chi.NewRouteContext(), m, path) {
			allowed = append(allowed, m)
		}
	}
	return strings.Join(allowed, ", ")
}
