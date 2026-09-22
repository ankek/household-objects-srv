package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/backup"
	"github.com/ankek/Household-Objects-Dev/server/internal/config"
	"github.com/ankek/Household-Objects-Dev/server/internal/datadir"
	"github.com/ankek/Household-Objects-Dev/server/internal/devicetoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"github.com/ankek/Household-Objects-Dev/server/internal/serverlock"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/ankek/Household-Objects-Dev/server/internal/webui"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const serveUsage = `usage: hho serve [--addr host:port]

Runs the HHO server: the /api/v1 API and the embedded web UI, on one listener.

  --addr   TCP address to listen on (default from HHO_ADDR, else :7745)

Configuration is read from the environment; see internal/config.
`

const (
	readHeaderTimeout = 10 * time.Second

	readTimeout = 60 * time.Second

	writeTimeout = 60 * time.Second

	idleTimeout = 120 * time.Second

	shutdownTimeout = 5 * time.Second
)

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(stderr, serveUsage) }
	addrFlag := fs.String("addr", "", "TCP address to listen on")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "hho serve: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := serve(ctx, *addrFlag, logger, stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "hho serve: %v\n", err)
		return 1
	}
	return 0
}

func serve(ctx context.Context, addrOverride string, logger *slog.Logger, stdout io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	addr := cfg.Addr
	if addrOverride != "" {
		addr = addrOverride
	}

	if err := datadir.Ensure(cfg.DataDir); err != nil {
		return err
	}

	var lock *serverlock.Lock
	switch lock, err = serverlock.Acquire(cfg.DataDir); {
	case err == nil:
		defer func() {
			if cerr := lock.Close(); cerr != nil {
				logger.Error("release server lock", slog.String("error", cerr.Error()))
			}
		}()
	case serverlock.IsHeld(err):
		return fmt.Errorf(
			"another \"hho serve\" already appears to be running against %s: %w -- refusing to start "+
				"a second server against the same data directory", cfg.DataDir, err)
	case errors.Is(err, serverlock.ErrUnsupported):
		logger.Warn(
			"server lock unavailable; hho restore will not be able to detect this running server on this data directory's filesystem",
			slog.String("data_dir", cfg.DataDir), slog.String("error", err.Error()))
	default:
		return fmt.Errorf("acquire server lock on %s: %w", cfg.DataDir, err)
	}

	store, err := storage.Open(ctx, storage.Config{Path: cfg.DatabasePath()})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			logger.Error("close database", slog.String("error", cerr.Error()))
		}
	}()

	startAttachmentReclaimLoop(ctx, store, cfg.DataDir, cfg.AttachmentReclaimIntervalSeconds, logger)

	startScheduledBackupLoop(ctx, store, cfg.DataDir, cfg.ScheduledBackupIntervalSeconds, cfg.ScheduledBackupRetentionCount, logger)

	router, err := buildRouter(cfg, store, logger)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", router)
	mux.Handle("/i/", router)
	mux.Handle("/", webui.Handler())

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	logger.Info("hho serve",
		slog.String("addr", ln.Addr().String()),
		slog.String("version", version),
		slog.Int64("schema_version", store.SchemaVersion()),
		slog.String("data_dir", cfg.DataDir),
		slog.Bool("registration_open", cfg.RegistrationOpen),
	)
	_, _ = fmt.Fprintf(stdout, "hho %s listening on %s\n", version, listenAddrForLog(addr))

	errc := make(chan error, 1)
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errc <- serveErr
			return
		}
		errc <- nil
	}()

	select {
	case serveErr := <-errc:
		return serveErr
	case <-ctx.Done():
		logger.Info("shutting down", slog.Duration("timeout", shutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return <-errc
}

func startAttachmentReclaimLoop(ctx context.Context, store *storage.Storage, dataDir string, intervalSeconds int64, logger *slog.Logger) {
	if intervalSeconds <= 0 {
		logger.Info("attachment reclamation pass disabled", slog.Int64("interval_seconds", intervalSeconds))
		return
	}

	go func() {
		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		defer ticker.Stop()

		runAttachmentReclaim(ctx, store, dataDir, logger)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runAttachmentReclaim(ctx, store, dataDir, logger)
			}
		}
	}()
}

func runAttachmentReclaim(ctx context.Context, store *storage.Storage, dataDir string, logger *slog.Logger) {
	report, err := attachments.Reclaim(ctx, store, dataDir)
	if err != nil {
		logger.Warn("attachment reclamation pass failed", slog.String("error", err.Error()))
		return
	}

	logger.Info("attachment reclamation pass complete",
		slog.Int("files_removed", report.FilesRemoved),
		slog.Int("rows_tombstoned", report.RowsTombstoned),
		slog.Int("thumbnails_cleared", report.ThumbnailsCleared),
		slog.Int("errors", len(report.Errors)),
	)
	for _, reclaimErr := range report.Errors {
		logger.Warn("attachment reclamation pass: non-fatal error", slog.String("error", reclaimErr.Error()))
	}
}

func startScheduledBackupLoop(ctx context.Context, store *storage.Storage, dataDir string, intervalSeconds, retentionCount int64, logger *slog.Logger) {
	if intervalSeconds <= 0 {
		logger.Warn("scheduled backup pass disabled -- FR-134's backup safety net is OFF for this instance",
			slog.Int64("interval_seconds", intervalSeconds))
		return
	}

	go func() {
		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		defer ticker.Stop()

		runScheduledBackup(ctx, store, dataDir, retentionCount, logger)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runScheduledBackup(ctx, store, dataDir, retentionCount, logger)
			}
		}
	}()
}

func runScheduledBackup(ctx context.Context, store *storage.Storage, dataDir string, retentionCount int64, logger *slog.Logger) {
	result, err := backup.RunScheduled(ctx, store, dataDir, int(retentionCount))
	if err != nil {
		logger.Error("scheduled backup pass failed", slog.String("error", err.Error()))
		return
	}

	logger.Info("scheduled backup pass complete",
		slog.String("path", result.Path),
		slog.Int("attachment_files", result.Stats.AttachmentFiles),
		slog.Int64("attachment_bytes", result.Stats.AttachmentBytes),
		slog.Int("attachment_files_skipped", result.Stats.AttachmentFilesSkipped),
		slog.Int("backups_pruned", result.Pruned),
	)
	for _, pruneErr := range result.PruneErrors {
		logger.Warn("scheduled backup pass: retention prune non-fatal error", slog.String("error", pruneErr.Error()))
	}
}

func buildRouter(cfg config.Config, store *storage.Storage, logger *slog.Logger) (http.Handler, error) {
	hasher, err := auth.NewHasher(auth.Config{})
	if err != nil {
		return nil, fmt.Errorf("build password hasher: %w", err)
	}

	registrar, err := groups.NewService(store, hasher)
	if err != nil {
		return nil, fmt.Errorf("build registration service: %w", err)
	}
	gate, err := groups.NewRegistrationGate(registrar, cfg.RegistrationOpen)
	if err != nil {
		return nil, fmt.Errorf("build registration gate: %w", err)
	}

	loginService, err := session.NewService(store, hasher)
	if err != nil {
		return nil, fmt.Errorf("build login service: %w", err)
	}
	sessionAuth, err := session.NewAuthenticator(store)
	if err != nil {
		return nil, fmt.Errorf("build session authenticator: %w", err)
	}
	deviceAuth, err := devicetoken.NewAuthenticator(store)
	if err != nil {
		return nil, fmt.Errorf("build device-token authenticator: %w", err)
	}
	deviceTokens, err := devicetoken.NewService(store)
	if err != nil {
		return nil, fmt.Errorf("build device-token service: %w", err)
	}
	invites, err := invite.NewService(store, hasher)
	if err != nil {
		return nil, fmt.Errorf("build invite service: %w", err)
	}

	return httpapi.NewRouter(httpapi.Config{
		Version: version,
		Schema:  store,
		Logger:  logger,

		Authenticator: middleware.Chain(sessionAuth, deviceAuth),

		SessionAuthenticator: sessionAuth,

		Scopes:       store,
		MaxBodyBytes: 0,

		DataDir:            cfg.DataDir,
		MaxAttachmentBytes: cfg.MaxAttachmentBytes,

		Registrar:          gate,
		LoginService:       loginService,
		DeviceTokenService: deviceTokens,

		Sessions:     store,
		DeviceTokens: store,
		Invites:      store,

		InviteService:  invites,
		InviteRedeemer: invites,

		LoginRateLimiter:        ratelimit.New(ratelimit.Config{}),
		RegisterRateLimiter:     ratelimit.New(ratelimit.Config{}),
		InviteRedeemRateLimiter: ratelimit.New(ratelimit.Config{}),

		TrustProxyHeaders: cfg.TrustProxyHeaders,

		MinClientVersion: cfg.MinClientVersion,

		Store: store,

		PublicBaseURLResolver: cfg.ResolvePublicBaseURL,
	})
}

func listenAddrForLog(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}
