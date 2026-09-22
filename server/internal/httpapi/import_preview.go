package httpapi

import (
	"context"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"os"
	"time"
)

const importPreviewPathParam = "importID"

type importPreviewRowResponse struct {
	Line    int                             `json:"line"`
	Action  string                          `json:"action"`
	ItemID  string                          `json:"item_id,omitempty"`
	Name    string                          `json:"name,omitempty"`
	Changes []string                        `json:"changes,omitempty"`
	Errors  []importPreviewRowErrorResponse `json:"errors,omitempty"`
}

type importPreviewRowErrorResponse struct {
	Line    int    `json:"line"`
	Column  string `json:"column,omitempty"`
	Message string `json:"message"`
}

type importPreviewSummaryResponse struct {
	Create    int `json:"create"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Error     int `json:"error"`
}

type importPreviewResponse struct {
	ImportID string                       `json:"import_id"`
	Rows     []importPreviewRowResponse   `json:"rows"`
	Summary  importPreviewSummaryResponse `json:"summary"`
}

func resolveStagedImportSession(ctx context.Context, sessions storage.ImportSessionRepository, importID string, now int64) (session storage.ImportSession, ok bool, conflict bool, err error) {
	session, err = sessions.Get(ctx, importID)
	if err != nil {
		return storage.ImportSession{}, false, false, err
	}

	switch {
	case session.Status == importexport.StatusCommitted:
		return session, false, true, nil
	case session.Status == importexport.StatusStaged && session.ExpiresAt > now:
		return session, true, false, nil
	default:
		_ = sessions.Delete(ctx, importID)
		return storage.ImportSession{}, false, false, nil
	}
}

func importPreviewHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		importID := chi.URLParam(r, importPreviewPathParam)
		groupID := scope.GroupID().String()

		sessions, err := importSessionRepositoryFor(cfg, scope)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import preview: resolve session repository failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		session, ok, conflict, err := resolveStagedImportSession(r.Context(), sessions, importID, time.Now().UnixMilli())
		switch {
		case errors.Is(err, storage.ErrNotFound):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import preview: read session failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		case conflict:
			problem.Write(w, r, problem.Conflict())
			return
		case !ok:
			_ = importexport.RemoveStaged(cfg.DataDir, groupID, importID)
			problem.Write(w, r, problem.NotFound())
			return
		}
		_ = session

		raw, err := os.ReadFile(importexport.StagingPath(cfg.DataDir, groupID, importID))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_ = sessions.Delete(r.Context(), importID)
				problem.Write(w, r, problem.NotFound())
				return
			}
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import preview: read staged file failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		classified, summary, err := classifyImportRows(r.Context(), scope, raw)
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "import preview: classify staged file failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		resp := importPreviewResponse{ImportID: importID, Rows: make([]importPreviewRowResponse, 0, len(classified)), Summary: summary}
		for _, row := range classified {
			resp.Rows = append(resp.Rows, importPreviewRowResponse{
				Line: row.Line, Action: row.Action, ItemID: row.ItemID, Name: row.Name, Changes: row.Changes, Errors: row.Errors,
			})
		}

		writeJSON(w, r, http.StatusOK, resp)
	}
}
