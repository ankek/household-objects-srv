package middleware

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
)

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := wrap(w)

			defer func() {
				v := recover()
				if v == nil {
					return
				}

				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}

				logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.String("request_id", requestid.FromContext(r.Context())),
					slog.String("panic", panicSummary(v)),
					slog.String("stack", string(debug.Stack())),
				)

				if rec.wrote {
					return
				}
				problem.Write(rec, r, problem.Internal())
			}()

			next.ServeHTTP(rec, r)
		})
	}
}

func panicSummary(v any) string {
	if err, ok := v.(runtime.Error); ok {
		return err.Error()
	}
	return fmt.Sprintf("panic value of type %T (contents withheld: NFR-018)", v)
}
