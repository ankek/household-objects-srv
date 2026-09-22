package middleware

import (
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/problem"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveRecovering(h http.Handler, r *http.Request) (rec *httptest.ResponseRecorder, escaped any) {
	rec = httptest.NewRecorder()
	defer func() { escaped = recover() }()
	h.ServeHTTP(rec, r)
	return rec, nil
}

func TestRecoverAnswersAPanicWithTheUniformProblemShape(t *testing.T) {
	logger, _ := captureLogs()

	h := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec, escaped := serveRecovering(h, httptest.NewRequest(http.MethodGet, "/", nil))

	if escaped != nil {
		t.Fatalf("the panic escaped the middleware: %v", escaped)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := decodeProblem(t, rec); got.Type != problem.Internal().Type {
		t.Errorf("problem type = %q, want %q", got.Type, problem.Internal().Type)
	}
}

func TestRecoverTellsTheClientNothingAboutTheFailure(t *testing.T) {
	const secret = "db password hunter2 in the error string"

	logger, _ := captureLogs()
	h := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(errors.New(secret))
	}))
	rec, _ := serveRecovering(h, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	for _, forbidden := range []string{secret, "hunter2", "goroutine", "panic", ".go:"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the 500 body contains %q: %s", forbidden, body)
		}
	}
	if got := decodeProblem(t, rec); got.Detail != "" {
		t.Errorf("the 500 carries a detail (%q); problem.Internal has no way to attach one for exactly this reason", got.Detail)
	}
}

func TestRecoverKeepsThePanicValueOutOfTheLog(t *testing.T) {
	const secret = "SECRETPANIC-c2Vzc2lvbi10b2tlbg"

	logger, logs := captureLogs()
	var id string
	h := RequestID(Recover(logger)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id = requestid.FromContext(r.Context())
		panic(fmt.Errorf("saving session %s failed", secret))
	})))
	if _, escaped := serveRecovering(h, httptest.NewRequest(http.MethodGet, "/", nil)); escaped != nil {
		t.Fatalf("the panic escaped: %v", escaped)
	}

	got := logs.String()
	if !strings.Contains(got, "panic recovered") {
		t.Fatalf("the panic was not logged at all, so this test proves nothing: %q", got)
	}
	if strings.Contains(got, secret) || strings.Contains(got, "saving session") {
		t.Errorf("the panic value reached the log (NFR-018): %q", got)
	}
	if !strings.Contains(got, "*errors.errorString") {
		t.Errorf("the log does not name the panic value's type, which is what is left to debug from: %q", got)
	}
	if !strings.Contains(got, "TestRecoverKeepsThePanicValueOutOfTheLog") {
		t.Errorf("the log carries no stack naming the panicking code: %q", got)
	}
	if !strings.Contains(got, id) {
		t.Errorf("the panic log line does not carry the request id %q, so it cannot be tied to the request line: %q", id, got)
	}
}

func TestRecoverKeepsRuntimeErrorText(t *testing.T) {
	logger, logs := captureLogs()

	h := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		var xs []int
		_ = xs[3] //nolint:govet // deliberate out-of-range index; the runtime panic is the subject
	}))
	if _, escaped := serveRecovering(h, httptest.NewRequest(http.MethodGet, "/", nil)); escaped != nil {
		t.Fatalf("the panic escaped: %v", escaped)
	}

	if got := logs.String(); !strings.Contains(got, "index out of range") {
		t.Errorf("the runtime error text was suppressed; it names no program data and is the whole diagnosis: %q", got)
	}
}

func TestRecoverLeavesAStartedResponseAlone(t *testing.T) {
	const partial = `{"items":[`

	logger, logs := captureLogs()
	h := Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(partial))
		panic("boom halfway through the list")
	}))
	rec, _ := serveRecovering(h, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; the 200 was already sent and cannot be taken back", rec.Code)
	}
	if rec.Body.String() != partial {
		t.Errorf("body = %q, want the truncated %q with nothing appended", rec.Body.String(), partial)
	}
	if !strings.Contains(logs.String(), "panic recovered") {
		t.Error("the panic was not logged; when the client cannot be told, the log is the only record")
	}
}

func TestRecoverLetsErrAbortHandlerThrough(t *testing.T) {
	for name, panicValue := range map[string]any{
		"bare":    http.ErrAbortHandler,
		"wrapped": fmt.Errorf("copying attachment: %w", http.ErrAbortHandler),
	} {
		logger, logs := captureLogs()
		h := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(panicValue)
		}))
		rec, escaped := serveRecovering(h, httptest.NewRequest(http.MethodGet, "/", nil))

		if escaped == nil {
			t.Errorf("%s: ErrAbortHandler was swallowed; net/http never sees the abort", name)
			continue
		}
		err, ok := escaped.(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("%s: re-panicked with %v, want the original value", name, escaped)
		}
		if logs.Len() != 0 {
			t.Errorf("%s: an aborted handler was logged as a fault: %q", name, logs.String())
		}
		if rec.Body.Len() != 0 {
			t.Errorf("%s: a body was written for an aborted handler: %q", name, rec.Body.String())
		}
	}
}

func TestRecoveredResponseCarriesTheRequestID(t *testing.T) {
	logger, logs := captureLogs()

	h := RequestID(Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec, _ := serveRecovering(h, httptest.NewRequest(http.MethodGet, "/", nil))

	id := decodeProblem(t, rec).RequestID
	if id == "" {
		t.Fatal("the 500 body carries no request_id, so a user reporting the error has nothing to quote")
	}
	if got := rec.Header().Get(requestid.HeaderName); got != id {
		t.Errorf("body request_id = %q but the header said %q", id, got)
	}
	if !strings.Contains(logs.String(), id) {
		t.Errorf("the id %q the client was given is on no log line: %q", id, logs.String())
	}
}
