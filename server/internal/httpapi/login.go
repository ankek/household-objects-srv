package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

type LoginService interface {
	Login(ctx context.Context, req session.LoginRequest) (session.LoggedIn, error)
}

var _ LoginService = (*session.Service)(nil)

type loginRequestBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponseBody struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func loginHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body loginRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			problem.Write(w, r, problem.BadRequest("the request body is not valid JSON"))
			return
		}

		ipKey, acctKey := loginRateLimitKeys(r, cfg.TrustProxyHeaders, body.Username)
		if ok, retryAfter := cfg.LoginRateLimiter.Allow(ipKey); !ok {
			writeRateLimited(w, r, retryAfter)
			return
		}
		if ok, retryAfter := cfg.LoginRateLimiter.Allow(acctKey); !ok {
			writeRateLimited(w, r, retryAfter)
			return
		}

		result, err := cfg.LoginService.Login(r.Context(), session.LoginRequest{
			Username:   body.Username,
			Password:   []byte(body.Password),
			UserAgent:  r.UserAgent(),
			RemoteAddr: remoteAddr(r),
		})
		switch {
		case errors.Is(err, session.ErrInvalidCredentials):
			cfg.LoginRateLimiter.RecordFailure(ipKey)
			cfg.LoginRateLimiter.RecordFailure(acctKey)
			if errors.Is(err, auth.ErrMalformedHash) {
				cfg.Logger.LogAttrs(r.Context(), slog.LevelWarn, "stored password hash is malformed; answered as invalid credentials",
					slog.String("request_id", requestid.FromContext(r.Context())),
				)
			}
			problem.Write(w, r, problem.Unauthorized())
			return
		case err != nil:
			cfg.Logger.LogAttrs(r.Context(), slog.LevelError, "login failed",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("error", err.Error()),
			)
			problem.Write(w, r, problem.Internal())
			return
		}

		cfg.LoginRateLimiter.RecordSuccess(ipKey)
		cfg.LoginRateLimiter.RecordSuccess(acctKey)

		http.SetCookie(w, &http.Cookie{
			Name:     session.CookieName,
			Value:    result.Token,
			Path:     "/",
			Expires:  result.ExpiresAt,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		respBody, err := json.Marshal(loginResponseBody{
			GroupID:  result.GroupID,
			UserID:   result.UserID,
			Username: result.Username,
			Role:     result.Role,
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
		_, _ = w.Write(respBody)
	}
}

func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func loginRateLimitKeys(r *http.Request, trustProxyHeaders bool, username string) (ipKey, acctKey string) {
	ip := clientIP(r, trustProxyHeaders)
	acct := strings.ToLower(strings.TrimSpace(username))
	return "ip:" + ip, "acct:" + acct
}
