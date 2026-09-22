package middleware

import (
	"context"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"time"
)

func Log(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := wrap(w)

			defer func() {
				level := slog.LevelInfo
				if rec.status >= http.StatusInternalServerError {
					level = slog.LevelError
				}

				logger.LogAttrs(r.Context(), level, "http request",
					slog.String("request_id", requestid.FromContext(r.Context())),
					slog.String("method", r.Method),
					slog.String("route", routePattern(r.Context())),
					slog.Int("status", rec.status),
					slog.Int64("bytes", rec.bytes),
					slog.Duration("duration", time.Since(start)),
				)
			}()

			next.ServeHTTP(rec, r)
		})
	}
}

func routePattern(ctx context.Context) string {
	rctx := chi.RouteContext(ctx)
	if rctx == nil {
		return ""
	}
	return rctx.RoutePattern()
}
