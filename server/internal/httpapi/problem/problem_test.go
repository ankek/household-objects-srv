package problem

import (
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteSendsTheRegisteredMediaTypeAndMatchingStatus(t *testing.T) {
	for _, p := range []Problem{NotFound(), MethodNotAllowed(), ContentTooLarge(), Internal(), BadRequest("detail"), Conflict(), TooManyRequests()} {
		rec := httptest.NewRecorder()
		Write(rec, httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil), p)

		if got := rec.Header().Get("Content-Type"); got != MediaType {
			t.Errorf("%s: Content-Type = %q, want %q", p.Type, got, MediaType)
		}
		if rec.Code != p.Status {
			t.Errorf("%s: response line says %d but the body says %d; a client that trusts one and logs the other reports two different failures for one response", p.Type, rec.Code, p.Status)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store; an error cached by an intermediary outlives the condition that produced it", p.Type, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", p.Type, got)
		}

		var decoded Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Errorf("%s: body is not JSON (%v): %q", p.Type, err, rec.Body.String())
			continue
		}
		if decoded != p {
			t.Errorf("%s: round-trip changed the problem: %+v -> %+v", p.Type, p, decoded)
		}
	}
}

func TestTypesAreStableAndDistinct(t *testing.T) {
	want := map[string]string{
		"not-found":          NotFound().Type,
		"method-not-allowed": MethodNotAllowed().Type,
		"content-too-large":  ContentTooLarge().Type,
		"internal":           Internal().Type,
		"bad-request":        BadRequest("detail").Type,
		"conflict":           Conflict().Type,
		"too-many-requests":  TooManyRequests().Type,
		"forbidden":          Forbidden().Type,
	}
	seen := make(map[string]string, len(want))
	for kind, typ := range want {
		if typ != typePrefix+kind {
			t.Errorf("type for %q = %q, want %q", kind, typ, typePrefix+kind)
		}
		if prev, dup := seen[typ]; dup {
			t.Errorf("%q and %q share the type %q; a client cannot tell them apart", prev, kind, typ)
		}
		seen[typ] = kind
	}
}

func TestNotFoundCarriesNothingRequestSpecific(t *testing.T) {
	if d := NotFound().Detail; d != "" {
		t.Errorf("NotFound carries a detail (%q); a foreign-tenant 404 and a genuinely-absent 404 must be the same response", d)
	}
	if NotFound() != NotFound() { //nolint:staticcheck // SA4000: identical operands are the point
		t.Error("NotFound is not deterministic")
	}
}

func TestInternalCarriesNoDetail(t *testing.T) {
	if d := Internal().Detail; d != "" {
		t.Errorf("Internal carries a detail (%q)", d)
	}
}

func TestConflictCarriesNoDetail(t *testing.T) {
	if d := Conflict().Detail; d != "" {
		t.Errorf("Conflict carries a detail (%q)", d)
	}
}

func TestTooManyRequestsCarriesNoDetail(t *testing.T) {
	if d := TooManyRequests().Detail; d != "" {
		t.Errorf("TooManyRequests carries a detail (%q)", d)
	}
}

func TestForbiddenCarriesNoDetail(t *testing.T) {
	if d := Forbidden().Detail; d != "" {
		t.Errorf("Forbidden carries a detail (%q)", d)
	}
}

func TestBadRequestCarriesTheGivenDetail(t *testing.T) {
	const detail = "username must be 1-64 bytes"
	if got := BadRequest(detail).Detail; got != detail {
		t.Errorf("BadRequest(%q).Detail = %q, want %q", detail, got, detail)
	}
}

func TestWriteRefusesAStatuslessProblem(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil), Problem{Title: "hand-built"})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; a Problem with no status must not be delivered as 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hand-built") {
		t.Errorf("the degraded response kept the hand-built fields: %q", rec.Body.String())
	}
}

func TestWithDetailDoesNotMutateTheOriginal(t *testing.T) {
	base := New(http.StatusBadRequest, "malformed-request", "Bad Request")
	withDetail := base.WithDetail("body is not valid JSON")

	if base.Detail != "" {
		t.Errorf("WithDetail mutated the receiver: %+v", base)
	}
	if withDetail.Detail == "" {
		t.Error("WithDetail did not set the detail")
	}
	if withDetail.Type != base.Type || withDetail.Status != base.Status {
		t.Errorf("WithDetail changed more than the detail: %+v -> %+v", base, withDetail)
	}
}

func TestFallbackBodyIsAValidProblem(t *testing.T) {
	var p Problem
	if err := json.Unmarshal([]byte(fallbackBody), &p); err != nil {
		t.Fatalf("fallbackBody is not JSON: %v", err)
	}
	if p != Internal() {
		t.Errorf("fallbackBody = %+v, want it to match Internal() = %+v", p, Internal())
	}
}

func TestWriteStampsTheRequestID(t *testing.T) {
	const id = "TESTREQUESTID2345678ABCDEF"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req = req.WithContext(requestid.NewContext(req.Context(), id))

	rec := httptest.NewRecorder()
	Write(rec, req, NotFound())

	var decoded Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	if decoded.RequestID != id {
		t.Errorf("request_id = %q, want %q", decoded.RequestID, id)
	}
	if !strings.Contains(rec.Body.String(), `"request_id"`) {
		t.Errorf("the wire format has no request_id field: %q", rec.Body.String())
	}
}

func TestWriteOmitsAnUnknownRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil), NotFound())

	if strings.Contains(rec.Body.String(), "request_id") {
		t.Errorf("a response with no request id still carries the field: %q", rec.Body.String())
	}
}

func TestWriteStampsTheRequestIDOnADegradedProblem(t *testing.T) {
	const id = "TESTREQUESTID2345678ABCDEF"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req = req.WithContext(requestid.NewContext(req.Context(), id))

	rec := httptest.NewRecorder()
	Write(rec, req, Problem{Title: "built by hand, no status"})

	var decoded Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	if decoded.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", decoded.Status)
	}
	if decoded.RequestID != id {
		t.Errorf("request_id = %q, want %q", decoded.RequestID, id)
	}
}

func TestWriteToleratesANilRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, nil, Internal())

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestUpgradeRequiredCarriesTheMinimum(t *testing.T) {
	p := UpgradeRequired("1.4.2")

	if p.Status != http.StatusUpgradeRequired {
		t.Errorf("Status = %d, want 426", p.Status)
	}
	if !strings.HasSuffix(p.Type, "upgrade-required") {
		t.Errorf("Type = %q, want a suffix of \"upgrade-required\"", p.Type)
	}
	if p.MinimumVersion != "1.4.2" {
		t.Errorf("MinimumVersion = %q, want %q", p.MinimumVersion, "1.4.2")
	}
}

func TestWriteUpgradeRequiredSendsTheRegisteredMediaTypeAndMatchingStatus(t *testing.T) {
	p := UpgradeRequired("2.0.0")

	rec := httptest.NewRecorder()
	WriteUpgradeRequired(rec, httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil), p)

	if got := rec.Header().Get("Content-Type"); got != MediaType {
		t.Errorf("Content-Type = %q, want %q", got, MediaType)
	}
	if rec.Code != http.StatusUpgradeRequired {
		t.Errorf("status = %d, want 426", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	var decoded UpgradeRequiredProblem
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	if decoded.MinimumVersion != "2.0.0" {
		t.Errorf("round-trip lost minimum_version: got %+v", decoded)
	}
	if !strings.Contains(rec.Body.String(), `"minimum_version":"2.0.0"`) {
		t.Errorf("wire body has no minimum_version field: %q", rec.Body.String())
	}
}

func TestWriteUpgradeRequiredStampsTheRequestID(t *testing.T) {
	const id = "TESTREQUESTID2345678ABCDEF"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req = req.WithContext(requestid.NewContext(req.Context(), id))

	rec := httptest.NewRecorder()
	WriteUpgradeRequired(rec, req, UpgradeRequired("1.0.0"))

	var decoded UpgradeRequiredProblem
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("body is not JSON (%v): %q", err, rec.Body.String())
	}
	if decoded.RequestID != id {
		t.Errorf("request_id = %q, want %q", decoded.RequestID, id)
	}
}

func TestWriteUpgradeRequiredDegradesAWrongStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteUpgradeRequired(rec, httptest.NewRequest(http.MethodGet, "/", nil), UpgradeRequiredProblem{
		Problem:        Problem{Title: "built by hand, wrong status"},
		MinimumVersion: "1.0.0",
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (degraded to Internal)", rec.Code)
	}
}
