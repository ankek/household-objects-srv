package main

import (
	"context"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/devicetoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	_ "modernc.org/sqlite"
	"net/http/httptest"
	"os"
	"path/filepath"
)

type benchServer struct {
	httpServer *httptest.Server
	store      *storage.Storage
	cleanup    func()
}

func newBenchServer(ctx context.Context) (*benchServer, error) {
	tmpRoot, err := os.MkdirTemp("", "hho-bench-api-latency-")
	if err != nil {
		return nil, fmt.Errorf("create temp root: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpRoot) }

	dbPath := filepath.Join(tmpRoot, "hho.db")
	store, err := storage.Open(ctx, storage.Config{Path: dbPath})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("storage.Open: %w", err)
	}

	dataDir := filepath.Join(tmpRoot, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := datadir.Ensure(dataDir); err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("datadir.Ensure: %w", err)
	}

	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("auth.NewHasher: %w", err)
	}

	registrar, err := groups.NewService(store, hasher)
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("groups.NewService: %w", err)
	}
	gate, err := groups.NewRegistrationGate(registrar, true)
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("groups.NewRegistrationGate: %w", err)
	}

	loginSvc, err := session.NewService(store, hasher)
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("session.NewService: %w", err)
	}
	sessAuth, err := session.NewAuthenticator(store)
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("session.NewAuthenticator: %w", err)
	}
	devAuth, err := devicetoken.NewAuthenticator(store)
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("devicetoken.NewAuthenticator: %w", err)
	}
	devSvc, err := devicetoken.NewService(store)
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("devicetoken.NewService: %w", err)
	}
	inviteSvc, err := invite.NewService(store, hasher)
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("invite.NewService: %w", err)
	}

	cfg := httpapi.Config{
		Version:              "bench-api-latency",
		Schema:               store,
		Logger:               slog.New(slog.DiscardHandler),
		Authenticator:        middleware.Chain(sessAuth, devAuth),
		Scopes:               store,
		Registrar:            gate,
		LoginService:         loginSvc,
		SessionAuthenticator: sessAuth,
		DeviceTokenService:   devSvc,
		Sessions:             store,
		DeviceTokens:         store,
		InviteService:        inviteSvc,
		Invites:              store,
		InviteRedeemer:       inviteSvc,
		DataDir:              dataDir,
		MaxAttachmentBytes:   attachments.DefaultMaxUploadBytes,
		Store:                store,
	}

	handler, err := httpapi.NewRouter(cfg)
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, fmt.Errorf("httpapi.NewRouter: %w", err)
	}

	srv := httptest.NewServer(handler)

	return &benchServer{
		httpServer: srv,
		store:      store,
		cleanup: func() {
			srv.Close()
			_ = store.Close()
			cleanup()
		},
	}, nil
}

func (s *benchServer) Close() {
	if s == nil {
		return
	}
	s.cleanup()
}
