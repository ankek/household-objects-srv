package httpapi

import (
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/importexport"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"time"
)

type importUploadResponse struct {
	ImportID string `json:"import_id"`
}

func maxImportBytes(cfg Config) int64 {
	if cfg.MaxImportBytes <= 0 {
		return importexport.DefaultMaxUploadBytes
	}
	return cfg.MaxImportBytes
}

func importSessionRepositoryFor(cfg Config, scope storage.Scope) (storage.ImportSessionRepository, error) {
	if cfg.Store == nil {
		return nil, errors.New("httpapi: import upload: no storage configured")
	}
	return cfg.Store.ForGroupImportSessions(scope.GroupID())
}

func importNativeUploadHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		id, err := uuid.NewV7()
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "mint import id failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}
		importID := id.String()
		groupID := scope.GroupID().String()

		stageErr := importexport.Stage(cfg.DataDir, groupID, importID, r.Body, maxImportBytes(cfg))
		var maxBytesErr *http.MaxBytesError
		switch {
		case errors.As(stageErr, &maxBytesErr), errors.Is(stageErr, importexport.ErrUploadTooLarge):
			problem.Write(w, r, problem.ContentTooLarge())
			return
		case errors.Is(stageErr, importexport.ErrInvalidCSV):
			problem.Write(w, r, problem.BadRequest(stageErr.Error()))
			return
		case stageErr != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "stage native import upload failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", stageErr.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		sessions, err := importSessionRepositoryFor(cfg, scope)
		if err != nil {
			_ = importexport.RemoveStaged(cfg.DataDir, groupID, importID)
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "resolve import session repository failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		created, err := sessions.Create(r.Context(), storage.CreateImportSessionParams{
			ID:     importID,
			Source: importexport.SourceNative,
			Now:    time.Now().UnixMilli(),
		})
		if err != nil {
			_ = importexport.RemoveStaged(cfg.DataDir, groupID, importID)
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "create import session failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		writeJSON(w, r, http.StatusCreated, importUploadResponse{ImportID: created.ID})
	}
}
