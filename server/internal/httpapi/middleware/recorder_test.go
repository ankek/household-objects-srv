package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecorderPreservesOptionalWriterCapabilities(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := wrap(inner)

	var w http.ResponseWriter = rec
	if _, ok := w.(http.Flusher); !ok {
		t.Error("the wrapper is not an http.Flusher")
	}
	if _, ok := w.(http.Hijacker); !ok {
		t.Error("the wrapper is not an http.Hijacker")
	}
	if _, ok := w.(io.ReaderFrom); !ok {
		t.Error("the wrapper is not an io.ReaderFrom; io.Copy would fall back to a buffer loop instead of the underlying writer's fast path")
	}
	if _, ok := w.(interface{ Unwrap() http.ResponseWriter }); !ok {
		t.Error("the wrapper does not implement Unwrap, which is how http.ResponseController finds the real writer")
	}

	rec.Flush()
	if !inner.Flushed {
		t.Error("Flush did not reach the underlying writer")
	}
}

func TestRecorderHijackReportsUnsupportedRatherThanLying(t *testing.T) {
	rec := wrap(httptest.NewRecorder())

	_, _, err := rec.Hijack()
	if !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Hijack on a non-hijackable writer returned %v, want http.ErrNotSupported", err)
	}
}

func TestRecorderReadFromWritesThroughAndCounts(t *testing.T) {
	const payload = "attachment bytes"

	inner := httptest.NewRecorder()
	rec := wrap(inner)

	n, err := io.Copy(rec, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if n != int64(len(payload)) || inner.Body.String() != payload {
		t.Errorf("copied %d bytes and wrote %q, want %d and %q", n, inner.Body.String(), len(payload), payload)
	}
	if rec.status != http.StatusOK || !rec.wrote {
		t.Errorf("status = %d, wrote = %v; a streamed response still sends the implicit 200", rec.status, rec.wrote)
	}
	if rec.bytes != int64(len(payload)) {
		t.Errorf("bytes = %d, want %d", rec.bytes, len(payload))
	}
}

func TestWrapReusesAnInstalledRecorder(t *testing.T) {
	first := wrap(httptest.NewRecorder())
	if second := wrap(first); second != first {
		t.Error("wrap built a second recorder around one that was already installed; the two would disagree about the status and split the byte count between them")
	}
}

func TestUnwrapReachesTheOriginalWriter(t *testing.T) {
	inner := httptest.NewRecorder()

	if got := unwrap(wrap(&recorder{ResponseWriter: inner})); got != http.ResponseWriter(inner) {
		t.Errorf("unwrap returned %T, want the original *httptest.ResponseRecorder", got)
	}
	if got := unwrap(inner); got != http.ResponseWriter(inner) {
		t.Errorf("unwrap of an unwrapped writer returned %T, want it unchanged", got)
	}
}
