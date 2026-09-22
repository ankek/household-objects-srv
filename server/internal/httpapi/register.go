package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/groups"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"log/slog"
	"net/http"
)

type Registrar interface {
	Register(ctx context.Context, req groups.RegisterRequest) (groups.Registered, error)
}

var _ Registrar = (*groups.Service)(nil)

var _ Registrar = (*groups.RegistrationGate)(nil)

type registerRequestBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerResponseBody struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

func registerHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body registerRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		ipKey := registerRateLimitKey(r, cfg.TrustProxyHeaders)
		if ok, retryAfter := cfg.RegisterRateLimiter.Allow(ipKey); !ok {
			writeRateLimited(w, r, retryAfter)
			return
		}

		result, err := cfg.Registrar.Register(r.Context(), groups.RegisterRequest{
			Username: body.Username,
			Password: []byte(body.Password),
		})
		registerRateLimitRecord(cfg.RegisterRateLimiter, ipKey, err)
		switch {
		case errors.Is(err, groups.ErrUsernameInvalid), errors.Is(err, groups.ErrPasswordInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, groups.ErrRegistrationClosed):
			problem.Write(w, r, problem.Conflict())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "registration failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		respBody, err := json.Marshal(registerResponseBody{
			GroupID:  result.GroupID,
			UserID:   result.UserID,
			Username: result.Username,
		})
		if err != nil {
			problem.Write(w, r, problem.Internal())
			return
		}

		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(respBody)
	}
}

func registerRateLimitKey(r *http.Request, trustProxyHeaders bool) string {
	return "ip:" + clientIP(r, trustProxyHeaders)
}

func registerRateLimitRecord(limiter *ratelimit.Limiter, key string, err error) {
	if errors.Is(err, groups.ErrUsernameInvalid) || errors.Is(err, groups.ErrPasswordInvalid) {
		return
	}
	limiter.RecordFailure(key)
}
