package httpapi

import (
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"net/http"
)

type statusResponse struct {
	Status string `json:"status"`

	Version string `json:"version"`

	SchemaVersion int64 `json:"schema_version"`
}

func statusHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := json.Marshal(statusResponse{
			Status:        "ok",
			Version:       cfg.Version,
			SchemaVersion: cfg.Schema.SchemaVersion(),
		})
		if err != nil {
			problem.Write(w, r, problem.Internal())
			return
		}

		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}
