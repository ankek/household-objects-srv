package httpapi

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/backup"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"log/slog"
	"net/http"
	"time"
)

const backupContentType = "application/gzip"

func filenameForBackup(now time.Time) string {
	return "hho-backup-" + now.UTC().Format("20060102T150405Z") + ".tar.gz"
}

func backupHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.Store == nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "backup requested with no storage configured",
				slog.String("request_id", requestid.FromContext(r.Context())),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		filename := filenameForBackup(time.Now())
		h := w.Header()
		h.Set("Content-Type", backupContentType)
		h.Set("Content-Disposition", attachmentContentDisposition("attachment", filename))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "private, no-store")

		if _, err := backup.WriteArchive(r.Context(), cfg.Store, cfg.DataDir, w); err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "backup archive stream failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			return
		}
	}
}
