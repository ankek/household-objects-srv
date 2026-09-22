package middleware

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"net/http"
)

const DefaultMaxBodyBytes int64 = 1 << 20

func MaxBody(limit int64) func(http.Handler) http.Handler {
	if limit <= 0 {
		limit = DefaultMaxBodyBytes
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				problem.Write(w, r, problem.ContentTooLarge())
				return
			}

			r.Body = http.MaxBytesReader(unwrap(w), r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
