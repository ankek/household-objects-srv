package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/bearertoken"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/invite"
	"github.com/ankek/Household-Objects-Dev/server/internal/ratelimit"
	"log/slog"
	"net/http"
)

type InviteRedeemer interface {
	Redeem(ctx context.Context, req invite.RedeemRequest) (invite.Redeemed, error)
}

var _ InviteRedeemer = (*invite.Service)(nil)

type inviteRedeemRequestBody struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type inviteRedeemResponseBody struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

func inviteRedeemHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body inviteRedeemRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		ipKey, tokenKey := inviteRedeemRateLimitKeys(r, cfg.TrustProxyHeaders, body.Token)
		if ok, retryAfter := cfg.InviteRedeemRateLimiter.Allow(ipKey); !ok {
			writeRateLimited(w, r, retryAfter)
			return
		}
		if ok, retryAfter := cfg.InviteRedeemRateLimiter.Allow(tokenKey); !ok {
			writeRateLimited(w, r, retryAfter)
			return
		}

		result, err := cfg.InviteRedeemer.Redeem(r.Context(), invite.RedeemRequest{
			Token:    body.Token,
			Username: body.Username,
			Password: []byte(body.Password),
		})
		inviteRedeemRateLimitRecord(cfg.InviteRedeemRateLimiter, ipKey, tokenKey, err)

		switch {
		case errors.Is(err, invite.ErrTokenRequired), errors.Is(err, invite.ErrUsernameInvalid), errors.Is(err, invite.ErrPasswordInvalid):
			problem.Write(w, r, problem.BadRequest(err.Error()))
			return
		case errors.Is(err, invite.ErrUsernameTaken):
			problem.Write(w, r, problem.Conflict())
			return
		case errors.Is(err, invite.ErrNotRedeemable):
			problem.Write(w, r, problem.NotFound())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "invite redemption failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		respBody, err := json.Marshal(inviteRedeemResponseBody{
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

func inviteRedeemRateLimitKeys(r *http.Request, trustProxyHeaders bool, token string) (ipKey, tokenKey string) {
	ip := clientIP(r, trustProxyHeaders)
	return "ip:" + ip, "token:" + bearertoken.Hash(token)
}

func inviteRedeemRateLimitRecord(limiter *ratelimit.Limiter, ipKey, tokenKey string, err error) {
	if errors.Is(err, invite.ErrTokenRequired) || errors.Is(err, invite.ErrUsernameInvalid) || errors.Is(err, invite.ErrPasswordInvalid) {
		return
	}
	if err != nil && !errors.Is(err, invite.ErrNotRedeemable) && !errors.Is(err, invite.ErrUsernameTaken) {
		return
	}
	limiter.RecordFailure(ipKey)
	limiter.RecordFailure(tokenKey)
}
