package httpapi

import (
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
	"strconv"
)

const defaultSyncConflictsLimit = 50

type syncConflictLogEntryBody struct {
	ID                string  `json:"id"`
	MutationID        *string `json:"mutation_id"`
	EntityType        string  `json:"entity_type"`
	EntityID          string  `json:"entity_id"`
	FieldName         string  `json:"field_name"`
	ServerValue       *string `json:"server_value"`
	LosingClientValue *string `json:"losing_client_value"`
	DetectedAt        int64   `json:"detected_at"`
}

type syncConflictLogResponseBody struct {
	Conflicts  []syncConflictLogEntryBody `json:"conflicts"`
	NextCursor *string                    `json:"next_cursor"`
}

var syncConflictRepositoryFor = func(cfg Config, scope storage.Scope) (storage.PushRepository, error) {
	if cfg.Store == nil {
		return nil, errors.New("httpapi: sync: no storage configured")
	}
	return cfg.Store.ForGroupPush(scope.GroupID())
}

func syncConflictsHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		q := r.URL.Query()

		limit := defaultSyncConflictsLimit
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > storage.MaxConflictPageLimit {
				problem.Write(w, r, problem.BadRequest("limit must be an integer between 1 and "+
					strconv.Itoa(storage.MaxConflictPageLimit)))
				return
			}
			limit = n
		}

		var after *storage.ConflictCursor
		if v := q.Get("after"); v != "" {
			c, err := storage.DecodeConflictCursor(v)
			if err != nil {
				problem.Write(w, r, problem.BadRequest("after is not a valid cursor"))
				return
			}
			after = &c
		}

		repo, err := syncConflictRepositoryFor(cfg, scope)
		if err == nil {
			var page storage.ConflictPage
			page, err = repo.ListConflicts(r.Context(), after, limit)
			if err == nil {
				body := syncConflictLogResponseBody{
					Conflicts: make([]syncConflictLogEntryBody, 0, len(page.Entries)),
				}
				for _, e := range page.Entries {
					body.Conflicts = append(body.Conflicts, syncConflictLogEntryBody(e))
				}
				if page.Next != nil {
					s := storage.EncodeConflictCursor(*page.Next)
					body.NextCursor = &s
				}
				writeJSON(w, r, http.StatusOK, body)
				return
			}
		}
		cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "sync: list conflicts failed",
			slog.String("request_id", requestid.FromContext(r.Context())),
			slog.String("error", err.Error()),
		)
		problem.Write(w, r, problem.Internal())
	}
}
