package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientVersion(t *testing.T) {
	tests := []struct {
		name        string
		minimum     string
		header      *string
		wantReached bool
		wantStatus  int
	}{
		{
			name:        "absent header is allowed",
			minimum:     "2.0.0",
			header:      nil,
			wantReached: true,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "equal to minimum is allowed",
			minimum:     "2.0.0",
			header:      ptr("2.0.0"),
			wantReached: true,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "above minimum is allowed",
			minimum:     "2.0.0",
			header:      ptr("2.0.1"),
			wantReached: true,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "below minimum is refused",
			minimum:     "2.0.0",
			header:      ptr("1.9.9"),
			wantReached: false,
			wantStatus:  http.StatusUpgradeRequired,
		},
		{
			name:        `"1.10.0" >= "1.9.0" (lexical-comparison trap)`,
			minimum:     "1.9.0",
			header:      ptr("1.10.0"),
			wantReached: true,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "malformed: not numeric",
			minimum:     "2.0.0",
			header:      ptr("abc"),
			wantReached: false,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "malformed: too few components",
			minimum:     "2.0.0",
			header:      ptr("1.2"),
			wantReached: false,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "malformed: too many components",
			minimum:     "2.0.0",
			header:      ptr("1.2.3.4"),
			wantReached: false,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "malformed: header present but empty",
			minimum:     "2.0.0",
			header:      ptr(""),
			wantReached: false,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "malformed: negative component",
			minimum:     "2.0.0",
			header:      ptr("1.-2.3"),
			wantReached: false,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "no minimum configured: any parseable version is allowed",
			minimum:     "",
			header:      ptr("0.0.0"),
			wantReached: true,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "no minimum configured: an implausibly large but well-formed version is allowed",
			minimum:     "",
			header:      ptr("999999.999999.999999"),
			wantReached: true,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "no minimum configured: a malformed claim still 400s",
			minimum:     "",
			header:      ptr("abc"),
			wantReached: false,
			wantStatus:  http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			h := ClientVersion(tc.minimum)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != nil {
				req.Header.Set(ClientVersionHeader, *tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if reached != tc.wantReached {
				t.Errorf("handler reached = %v, want %v", reached, tc.wantReached)
			}
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d: %q", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestClientVersionRefusalNamesTheMinimum(t *testing.T) {
	h := ClientVersion("3.2.1")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("the handler ran; the whole point of the 426 is that it does not")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(ClientVersionHeader, "1.0.0")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want 426: %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"minimum_version":"3.2.1"`) {
		t.Errorf("body does not carry the configured minimum: %q", body)
	}
	if !strings.Contains(body, "upgrade-required") {
		t.Errorf("body's problem type does not name upgrade-required: %q", body)
	}
}

func TestClientVersionConstructorPanicsOnAMalformedMinimum(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ClientVersion(\"not-a-version\") did not panic")
		}
	}()
	ClientVersion("not-a-version")
}

func TestParseClientVersion(t *testing.T) {
	valid := map[string][3]int{
		"0.0.0":    {0, 0, 0},
		"1.4.2":    {1, 4, 2},
		"10.20.30": {10, 20, 30},
		"01.02.03": {1, 2, 3},
	}
	for raw, want := range valid {
		major, minor, patch, err := parseClientVersion(raw)
		if err != nil {
			t.Errorf("parseClientVersion(%q) = error %v, want (%d,%d,%d)", raw, err, want[0], want[1], want[2])
			continue
		}
		if major != want[0] || minor != want[1] || patch != want[2] {
			t.Errorf("parseClientVersion(%q) = (%d,%d,%d), want (%d,%d,%d)", raw, major, minor, patch, want[0], want[1], want[2])
		}
	}

	invalid := []string{"", "abc", "1.2", "1.2.3.4", "1.-2.3", "-1.2.3", "1.2.-3", "1.2.3-rc1", "1.2.3+build5", "1..3", "1.2."}
	for _, raw := range invalid {
		if _, _, _, err := parseClientVersion(raw); err == nil {
			t.Errorf("parseClientVersion(%q) accepted a malformed version", raw)
		}
	}
}

func TestVersionLessIsNumericNotLexical(t *testing.T) {
	if versionLess(1, 10, 0, 1, 9, 0) {
		t.Error("versionLess(1.10.0, 1.9.0) = true; 1.10.0 is the LATER release")
	}
	if !versionLess(1, 9, 0, 1, 10, 0) {
		t.Error("versionLess(1.9.0, 1.10.0) = false; 1.9.0 is the EARLIER release")
	}
	if versionLess(1, 2, 3, 1, 2, 3) {
		t.Error("versionLess(1.2.3, 1.2.3) = true; equal versions are not less than each other")
	}
}

func ptr(s string) *string { return &s }
