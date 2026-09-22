package httpapi

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/middleware"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"log/slog"
	"net/http"
)

type groupMembersResponse struct {
	Members []groupMemberListItem `json:"members"`
}

type groupMemberListItem struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	JoinedAt int64  `json:"joined_at"`
}

func toGroupMemberListItem(m storage.Member) groupMemberListItem {
	return groupMemberListItem{
		ID:       m.ID,
		Username: m.Username,
		Role:     m.Role,
		JoinedAt: m.JoinedAtUnixMilli,
	}
}

func memberListHandler(cfg Config) middleware.ScopedHandler {
	return func(w http.ResponseWriter, r *http.Request, scope storage.Scope) {
		members, err := scope.Members().List(r.Context())
		if err != nil {
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "list group members failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		items := make([]groupMemberListItem, 0, len(members))
		for _, m := range members {
			items = append(items, toGroupMemberListItem(m))
		}
		writeJSON(w, r, http.StatusOK, groupMembersResponse{Members: items})
	}
}
