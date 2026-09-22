package middleware

import (
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"net/http"
	"strconv"
	"strings"
)

const ClientVersionHeader = "X-HHO-Client-Version"

func ClientVersion(minimum string) func(http.Handler) http.Handler {
	var (
		hasMinimum                   bool
		minMajor, minMinor, minPatch int
	)
	if minimum != "" {
		major, minor, patch, err := parseClientVersion(minimum)
		if err != nil {
			panic(fmt.Sprintf("middleware: ClientVersion: minimum %q is not MAJOR.MINOR.PATCH: %v", minimum, err))
		}
		hasMinimum = true
		minMajor, minMinor, minPatch = major, minor, patch
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			values := r.Header.Values(ClientVersionHeader)
			if len(values) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			raw := values[0]

			major, minor, patch, err := parseClientVersion(raw)
			if err != nil {
				problem.Write(w, r, problem.BadRequest(fmt.Sprintf(
					"%s must be MAJOR.MINOR.PATCH (e.g. \"1.4.2\"): %v", ClientVersionHeader, err)))
				return
			}

			if hasMinimum && versionLess(major, minor, patch, minMajor, minMinor, minPatch) {
				problem.WriteUpgradeRequired(w, r, problem.UpgradeRequired(minimum))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func versionLess(aMajor, aMinor, aPatch, bMajor, bMinor, bPatch int) bool {
	if aMajor != bMajor {
		return aMajor < bMajor
	}
	if aMinor != bMinor {
		return aMinor < bMinor
	}
	return aPatch < bPatch
}

func parseClientVersion(raw string) (major, minor, patch int, err error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("%q has %d dot-separated component(s), want 3", raw, len(parts))
	}
	nums := make([]int, 3)
	for i, part := range parts {
		if !isDigits(part) {
			return 0, 0, 0, fmt.Errorf("component %q of %q is not a non-negative integer", part, raw)
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("component %q of %q: %w", part, raw, err)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
