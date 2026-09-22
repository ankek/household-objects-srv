package middleware

import (
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type logRecord map[string]any

func onlyRecord(t *testing.T, logs interface{ String() string }) logRecord {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly one log record, got %d: %q", len(lines), logs.String())
	}
	var rec logRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("log line is not JSON (%v): %q", err, lines[0])
	}
	return rec
}

func TestLogReportsTheOutcomeOfTheRequest(t *testing.T) {
	logger, logs := captureLogs()

	var id string
	h := RequestID(Log(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id = requestid.FromContext(r.Context())
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "brewing")
	})))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPatch, "/anything", nil))

	rec := onlyRecord(t, logs)
	for field, want := range map[string]any{
		"msg":        "http request",
		"level":      "INFO",
		"method":     "PATCH",
		"status":     float64(http.StatusTeapot),
		"bytes":      float64(len("brewing")),
		"request_id": id,
	} {
		if got := rec[field]; got != want {
			t.Errorf("%s = %v, want %v (full record: %v)", field, got, want, rec)
		}
	}
	if _, ok := rec["duration"]; !ok {
		t.Errorf("no duration in the record: %v", rec)
	}
}

func TestLogRecordsTheImplicitTwoHundred(t *testing.T) {
	logger, logs := captureLogs()

	h := Log(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got := onlyRecord(t, logs)["status"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", got)
	}
}

func TestLogRaisesTheLevelForServerFaults(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusOK:                  "INFO",
		http.StatusNotFound:            "INFO",
		http.StatusTooManyRequests:     "INFO",
		http.StatusInternalServerError: "ERROR",
		http.StatusBadGateway:          "ERROR",
	} {
		logger, logs := captureLogs()
		h := Log(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

		if got := onlyRecord(t, logs)["level"]; got != want {
			t.Errorf("status %d logged at %v, want %s", status, got, want)
		}
	}
}

func TestLogLeavesTheRouteEmptyWithoutARouter(t *testing.T) {
	logger, logs := captureLogs()

	h := Log(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/secret-token-value", nil))

	rec := onlyRecord(t, logs)
	if got := rec["route"]; got != "" {
		t.Errorf("route = %v, want empty outside a chi router", got)
	}
	if strings.Contains(logs.String(), "secret-token-value") {
		t.Errorf("the request path was logged: %q", logs.String())
	}
}

func TestLogEmitsExactlyOneRecordPerRequest(t *testing.T) {
	logger, logs := captureLogs()

	h := Log(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "a")
		_, _ = io.WriteString(w, "b")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	rec := onlyRecord(t, logs)
	if got := rec["status"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200: the second WriteHeader was never sent to the client, so it must not be what the log reports", got)
	}
	if got := rec["bytes"]; got != float64(2) {
		t.Errorf("bytes = %v, want 2", got)
	}
}
