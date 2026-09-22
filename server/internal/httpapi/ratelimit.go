package httpapi

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"net/http"
	"strconv"
	"time"
)

func writeRateLimited(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	seconds := int64(retryAfter+time.Second-1) / int64(time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	problem.Write(w, r, problem.TooManyRequests())
}
